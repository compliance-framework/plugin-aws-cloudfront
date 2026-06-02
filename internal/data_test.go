package internal

import (
	"context"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudfront"
	"github.com/aws/aws-sdk-go-v2/service/cloudfront/types"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/hashicorp/go-hclog"
)

type mockCFClient struct {
	listDistributions   func(context.Context, *cloudfront.ListDistributionsInput, ...func(*cloudfront.Options)) (*cloudfront.ListDistributionsOutput, error)
	listTagsForResource func(context.Context, *cloudfront.ListTagsForResourceInput, ...func(*cloudfront.Options)) (*cloudfront.ListTagsForResourceOutput, error)
}

func (m *mockCFClient) ListDistributions(ctx context.Context, params *cloudfront.ListDistributionsInput, optFns ...func(*cloudfront.Options)) (*cloudfront.ListDistributionsOutput, error) {
	return m.listDistributions(ctx, params, optFns...)
}

func (m *mockCFClient) ListTagsForResource(ctx context.Context, params *cloudfront.ListTagsForResourceInput, optFns ...func(*cloudfront.Options)) (*cloudfront.ListTagsForResourceOutput, error) {
	return m.listTagsForResource(ctx, params, optFns...)
}

type mockSTSClient struct {
	getCallerIdentity func(context.Context, *sts.GetCallerIdentityInput, ...func(*sts.Options)) (*sts.GetCallerIdentityOutput, error)
}

func (m *mockSTSClient) GetCallerIdentity(ctx context.Context, params *sts.GetCallerIdentityInput, optFns ...func(*sts.Options)) (*sts.GetCallerIdentityOutput, error) {
	return m.getCallerIdentity(ctx, params, optFns...)
}

func defaultSTSMock() *mockSTSClient {
	return &mockSTSClient{
		getCallerIdentity: func(_ context.Context, _ *sts.GetCallerIdentityInput, _ ...func(*sts.Options)) (*sts.GetCallerIdentityOutput, error) {
			return &sts.GetCallerIdentityOutput{Account: aws.String("123456789012")}, nil
		},
	}
}

func emptyTagsMock() func(context.Context, *cloudfront.ListTagsForResourceInput, ...func(*cloudfront.Options)) (*cloudfront.ListTagsForResourceOutput, error) {
	return func(_ context.Context, _ *cloudfront.ListTagsForResourceInput, _ ...func(*cloudfront.Options)) (*cloudfront.ListTagsForResourceOutput, error) {
		return &cloudfront.ListTagsForResourceOutput{Tags: &types.Tags{}}, nil
	}
}

func newTestFetcher(cf CloudFrontClient, s STSClient) *DataFetcher {
	return &DataFetcher{
		logger:       hclog.NewNullLogger(),
		config:       &PluginConfig{},
		newCFClient:  func(_ context.Context) (CloudFrontClient, error) { return cf, nil },
		newSTSClient: func(_ context.Context) (STSClient, error) { return s, nil },
	}
}

func TestFetchData_Empty(t *testing.T) {
	cf := &mockCFClient{
		listDistributions: func(_ context.Context, _ *cloudfront.ListDistributionsInput, _ ...func(*cloudfront.Options)) (*cloudfront.ListDistributionsOutput, error) {
			return &cloudfront.ListDistributionsOutput{
				DistributionList: &types.DistributionList{IsTruncated: aws.Bool(false)},
			}, nil
		},
	}
	f := newTestFetcher(cf, defaultSTSMock())
	dists, err := f.FetchData(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(dists) != 0 {
		t.Errorf("expected 0 distributions, got %d", len(dists))
	}
}

func TestFetchData_SingleDistribution(t *testing.T) {
	cf := &mockCFClient{
		listDistributions: func(_ context.Context, _ *cloudfront.ListDistributionsInput, _ ...func(*cloudfront.Options)) (*cloudfront.ListDistributionsOutput, error) {
			return &cloudfront.ListDistributionsOutput{
				DistributionList: &types.DistributionList{
					IsTruncated: aws.Bool(false),
					Items: []types.DistributionSummary{
						{
							Id:         aws.String("EDFDVBD6EXAMPLE"),
							ARN:        aws.String("arn:aws:cloudfront::123456789012:distribution/EDFDVBD6EXAMPLE"),
							DomainName: aws.String("d111111abcdef8.cloudfront.net"),
							Status:     aws.String("Deployed"),
							Enabled:    aws.Bool(true),
							DefaultCacheBehavior: &types.DefaultCacheBehavior{
								ViewerProtocolPolicy: types.ViewerProtocolPolicyRedirectToHttps,
							},
							ViewerCertificate: &types.ViewerCertificate{
								ACMCertificateArn: aws.String("arn:aws:acm:us-east-1:123456789012:certificate/abc-123"),
							},
						},
					},
				},
			}, nil
		},
		listTagsForResource: func(_ context.Context, _ *cloudfront.ListTagsForResourceInput, _ ...func(*cloudfront.Options)) (*cloudfront.ListTagsForResourceOutput, error) {
			return &cloudfront.ListTagsForResourceOutput{
				Tags: &types.Tags{
					Items: []types.Tag{
						{Key: aws.String("env"), Value: aws.String("prod")},
					},
				},
			}, nil
		},
	}

	f := newTestFetcher(cf, defaultSTSMock())
	dists, err := f.FetchData(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(dists) != 1 {
		t.Fatalf("expected 1 distribution, got %d", len(dists))
	}

	d := dists[0]
	if d.DistributionID != "EDFDVBD6EXAMPLE" {
		t.Errorf("distribution_id: want %q, got %q", "EDFDVBD6EXAMPLE", d.DistributionID)
	}
	if d.AccountID != "123456789012" {
		t.Errorf("account_id: want %q, got %q", "123456789012", d.AccountID)
	}
	if d.Status != "Deployed" {
		t.Errorf("status: want %q, got %q", "Deployed", d.Status)
	}
	if !d.Enabled {
		t.Errorf("enabled: want true, got false")
	}
	if d.CertificateArn != "arn:aws:acm:us-east-1:123456789012:certificate/abc-123" {
		t.Errorf("certificate_arn: want %q, got %q", "arn:aws:acm:us-east-1:123456789012:certificate/abc-123", d.CertificateArn)
	}
	if len(d.CacheBehaviours) != 1 {
		t.Fatalf("expected 1 cache behaviour, got %d", len(d.CacheBehaviours))
	}
	if d.CacheBehaviours[0].PathPattern != "/*" {
		t.Errorf("default behaviour path: want %q, got %q", "/*", d.CacheBehaviours[0].PathPattern)
	}
	if d.CacheBehaviours[0].ViewerProtocolPolicy != "redirect-to-https" {
		t.Errorf("viewer_protocol_policy: want %q, got %q", "redirect-to-https", d.CacheBehaviours[0].ViewerProtocolPolicy)
	}
	if d.Tags["env"] != "prod" {
		t.Errorf("tags: expected env=prod, got %v", d.Tags)
	}
}

func TestFetchData_AllowAllAndPathBehaviours(t *testing.T) {
	cf := &mockCFClient{
		listDistributions: func(_ context.Context, _ *cloudfront.ListDistributionsInput, _ ...func(*cloudfront.Options)) (*cloudfront.ListDistributionsOutput, error) {
			return &cloudfront.ListDistributionsOutput{
				DistributionList: &types.DistributionList{
					IsTruncated: aws.Bool(false),
					Items: []types.DistributionSummary{
						{
							Id:         aws.String("DIST2"),
							ARN:        aws.String("arn:aws:cloudfront::123456789012:distribution/DIST2"),
							DomainName: aws.String("d2.cloudfront.net"),
							Status:     aws.String("Deployed"),
							Enabled:    aws.Bool(true),
							DefaultCacheBehavior: &types.DefaultCacheBehavior{
								ViewerProtocolPolicy: types.ViewerProtocolPolicyAllowAll,
							},
							CacheBehaviors: &types.CacheBehaviors{
								Items: []types.CacheBehavior{
									{
										PathPattern:          aws.String("/api/*"),
										ViewerProtocolPolicy: types.ViewerProtocolPolicyRedirectToHttps,
									},
								},
							},
						},
					},
				},
			}, nil
		},
		listTagsForResource: emptyTagsMock(),
	}

	f := newTestFetcher(cf, defaultSTSMock())
	dists, err := f.FetchData(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	d := dists[0]
	if len(d.CacheBehaviours) != 2 {
		t.Fatalf("expected 2 cache behaviours, got %d", len(d.CacheBehaviours))
	}
	if d.CacheBehaviours[0].ViewerProtocolPolicy != "allow-all" {
		t.Errorf("default behaviour: want allow-all, got %q", d.CacheBehaviours[0].ViewerProtocolPolicy)
	}
	if d.CacheBehaviours[1].PathPattern != "/api/*" {
		t.Errorf("path behaviour: want /api/*, got %q", d.CacheBehaviours[1].PathPattern)
	}
}

func TestFetchData_Pagination(t *testing.T) {
	callCount := 0
	cf := &mockCFClient{
		listDistributions: func(_ context.Context, _ *cloudfront.ListDistributionsInput, _ ...func(*cloudfront.Options)) (*cloudfront.ListDistributionsOutput, error) {
			callCount++
			if callCount == 1 {
				return &cloudfront.ListDistributionsOutput{
					DistributionList: &types.DistributionList{
						IsTruncated: aws.Bool(true),
						NextMarker:  aws.String("marker1"),
						Items:       []types.DistributionSummary{{Id: aws.String("DIST1"), ARN: aws.String("arn:aws:cloudfront::123456789012:distribution/DIST1"), DomainName: aws.String("d1.cloudfront.net"), Status: aws.String("Deployed")}},
					},
				}, nil
			}
			return &cloudfront.ListDistributionsOutput{
				DistributionList: &types.DistributionList{
					IsTruncated: aws.Bool(false),
					Items:       []types.DistributionSummary{{Id: aws.String("DIST2"), ARN: aws.String("arn:aws:cloudfront::123456789012:distribution/DIST2"), DomainName: aws.String("d2.cloudfront.net"), Status: aws.String("Deployed")}},
				},
			}, nil
		},
		listTagsForResource: emptyTagsMock(),
	}

	f := newTestFetcher(cf, defaultSTSMock())
	dists, err := f.FetchData(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(dists) != 2 {
		t.Errorf("expected 2 distributions, got %d", len(dists))
	}
	if callCount != 2 {
		t.Errorf("expected 2 ListDistributions calls, got %d", callCount)
	}
}
