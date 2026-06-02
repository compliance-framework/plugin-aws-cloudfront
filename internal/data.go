package internal

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/cloudfront"
	"github.com/aws/aws-sdk-go-v2/service/cloudfront/types"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/hashicorp/go-hclog"
)

// CloudFrontClient is the subset of the AWS CloudFront API used by DataFetcher.
type CloudFrontClient interface {
	ListDistributions(ctx context.Context, params *cloudfront.ListDistributionsInput, optFns ...func(*cloudfront.Options)) (*cloudfront.ListDistributionsOutput, error)
	ListTagsForResource(ctx context.Context, params *cloudfront.ListTagsForResourceInput, optFns ...func(*cloudfront.Options)) (*cloudfront.ListTagsForResourceOutput, error)
}

// STSClient is used to determine the AWS account ID.
type STSClient interface {
	GetCallerIdentity(ctx context.Context, params *sts.GetCallerIdentityInput, optFns ...func(*sts.Options)) (*sts.GetCallerIdentityOutput, error)
}

// CacheBehaviour holds the viewer protocol policy for one cache behaviour.
type CacheBehaviour struct {
	PathPattern          string `json:"path_pattern"`
	ViewerProtocolPolicy string `json:"viewer_protocol_policy"`
}

// DistributionContext holds all fields required by CloudFront compliance policies.
type DistributionContext struct {
	AccountID       string            `json:"account_id"`
	DistributionID  string            `json:"distribution_id"`
	ARN             string            `json:"arn"`
	DomainName      string            `json:"domain_name"`
	Status          string            `json:"status"`
	Enabled         bool              `json:"enabled"`
	CertificateArn  string            `json:"certificate_arn,omitempty"`
	CacheBehaviours []CacheBehaviour  `json:"cache_behaviours"`
	Tags            map[string]string `json:"tags"`
}

// ToOPAInput serialises d to a map[string]interface{} suitable for OPA input.
func (d DistributionContext) ToOPAInput() (map[string]interface{}, error) {
	b, err := json.Marshal(d)
	if err != nil {
		return nil, err
	}
	var m map[string]interface{}
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	return m, nil
}

// DataFetcher retrieves CloudFront distribution data.
type DataFetcher struct {
	logger       hclog.Logger
	config       *PluginConfig
	newCFClient  func(ctx context.Context) (CloudFrontClient, error)
	newSTSClient func(ctx context.Context) (STSClient, error)
}

// NewDataFetcher returns a DataFetcher using the standard AWS credential chain.
// CloudFront is a global service; all API calls are routed via us-east-1.
func NewDataFetcher(logger hclog.Logger, cfg *PluginConfig) *DataFetcher {
	return &DataFetcher{
		logger: logger,
		config: cfg,
		newCFClient: func(ctx context.Context) (CloudFrontClient, error) {
			awsCfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion("us-east-1"))
			if err != nil {
				return nil, err
			}
			return cloudfront.NewFromConfig(awsCfg), nil
		},
		newSTSClient: func(ctx context.Context) (STSClient, error) {
			awsCfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion("us-east-1"))
			if err != nil {
				return nil, err
			}
			return sts.NewFromConfig(awsCfg), nil
		},
	}
}

// FetchData retrieves all CloudFront distributions for the caller's account.
func (df *DataFetcher) FetchData(ctx context.Context) ([]DistributionContext, error) {
	stsClient, err := df.newSTSClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("create STS client: %w", err)
	}
	identity, err := stsClient.GetCallerIdentity(ctx, &sts.GetCallerIdentityInput{})
	if err != nil {
		return nil, fmt.Errorf("GetCallerIdentity: %w", err)
	}
	accountID := aws.ToString(identity.Account)

	cfClient, err := df.newCFClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("create CloudFront client: %w", err)
	}

	var distributions []DistributionContext
	var marker *string
	for {
		out, err := cfClient.ListDistributions(ctx, &cloudfront.ListDistributionsInput{
			Marker: marker,
		})
		if err != nil {
			return nil, fmt.Errorf("ListDistributions: %w", err)
		}
		if out.DistributionList == nil {
			break
		}
		for _, d := range out.DistributionList.Items {
			dist, err := df.buildContext(ctx, cfClient, accountID, d)
			if err != nil {
				df.logger.Warn("skipping distribution", "id", aws.ToString(d.Id), "error", err)
				continue
			}
			distributions = append(distributions, dist)
		}
		if !aws.ToBool(out.DistributionList.IsTruncated) {
			break
		}
		marker = out.DistributionList.NextMarker
	}
	return distributions, nil
}

func (df *DataFetcher) buildContext(ctx context.Context, client CloudFrontClient, accountID string, d types.DistributionSummary) (DistributionContext, error) {
	arn := aws.ToString(d.ARN)

	// Default cache behaviour is always present; path-based behaviours are optional.
	behaviours := make([]CacheBehaviour, 0)
	if d.DefaultCacheBehavior != nil {
		behaviours = append(behaviours, CacheBehaviour{
			PathPattern:          "/*",
			ViewerProtocolPolicy: string(d.DefaultCacheBehavior.ViewerProtocolPolicy),
		})
	}
	if d.CacheBehaviors != nil {
		for _, cb := range d.CacheBehaviors.Items {
			behaviours = append(behaviours, CacheBehaviour{
				PathPattern:          aws.ToString(cb.PathPattern),
				ViewerProtocolPolicy: string(cb.ViewerProtocolPolicy),
			})
		}
	}

	certArn := ""
	if d.ViewerCertificate != nil && d.ViewerCertificate.ACMCertificateArn != nil {
		certArn = aws.ToString(d.ViewerCertificate.ACMCertificateArn)
	}

	tags := make(map[string]string)
	tagsOut, err := client.ListTagsForResource(ctx, &cloudfront.ListTagsForResourceInput{
		Resource: aws.String(arn),
	})
	if err == nil && tagsOut.Tags != nil {
		for _, tag := range tagsOut.Tags.Items {
			tags[aws.ToString(tag.Key)] = aws.ToString(tag.Value)
		}
	}

	return DistributionContext{
		AccountID:       accountID,
		DistributionID:  aws.ToString(d.Id),
		ARN:             arn,
		DomainName:      aws.ToString(d.DomainName),
		Status:          aws.ToString(d.Status),
		Enabled:         aws.ToBool(d.Enabled),
		CertificateArn:  certArn,
		CacheBehaviours: behaviours,
		Tags:            tags,
	}, nil
}
