package internal

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	policyManager "github.com/compliance-framework/agent/policy-manager"
	"github.com/compliance-framework/agent/runner/proto"
	"github.com/hashicorp/go-hclog"
)

type PolicyEvaluator struct {
	ctx            context.Context
	logger         hclog.Logger
	stepActivities []*proto.Activity
}

func NewPolicyEvaluator(ctx context.Context, logger hclog.Logger, stepActivities []*proto.Activity) *PolicyEvaluator {
	return &PolicyEvaluator{ctx: ctx, logger: logger, stepActivities: stepActivities}
}

func (pe *PolicyEvaluator) Eval(ctx context.Context, dist DistributionContext, policyPaths []string, policyData map[string]interface{}, extraLabels map[string]string) ([]*proto.Evidence, error) {
	var accumulatedErrors error
	evidences := make([]*proto.Evidence, 0)

	input, err := dist.ToOPAInput()
	if err != nil {
		return nil, fmt.Errorf("serialising distribution %s: %w", dist.DistributionID, err)
	}

	componentID := "common-components/aws-cloudfront"
	inventoryID := fmt.Sprintf("aws-cloudfront-distribution/%s", dist.DistributionID)

	labels := MergeMaps(extraLabels, distributionBaseLabels(), map[string]string{
		"distribution_id": dist.DistributionID,
		"resource_arn":    dist.ARN,
		"account_id":      dist.AccountID,
		"domain_name":     dist.DomainName,
	})

	actors := []*proto.OriginActor{
		{
			Title: "The Continuous Compliance Framework",
			Type:  "assessment-platform",
			Links: []*proto.Link{
				{
					Href: "https://compliance-framework.github.io/docs/",
					Rel:  StringAddressed("reference"),
					Text: StringAddressed("The Continuous Compliance Framework"),
				},
			},
		},
		{
			Title: "Continuous Compliance Framework - AWS CloudFront Plugin",
			Type:  "tool",
			Links: []*proto.Link{
				{
					Href: "https://github.com/compliance-framework/plugin-aws-cloudfront",
					Rel:  StringAddressed("reference"),
					Text: StringAddressed("The Continuous Compliance Framework AWS CloudFront Plugin"),
				},
			},
		},
	}

	components := []*proto.Component{
		{
			Identifier:  componentID,
			Type:        "service",
			Title:       "AWS CloudFront",
			Description: "Amazon CloudFront is a content delivery network (CDN) service that securely delivers data, videos, applications, and APIs with low latency.",
			Purpose:     "To distribute content globally via HTTPS with configurable TLS policies.",
		},
	}

	inventory := []*proto.InventoryItem{
		{
			Identifier: inventoryID,
			Type:       "distribution",
			Title:      fmt.Sprintf("AWS CloudFront Distribution [%s]", dist.DomainName),
			Props: []*proto.Property{
				{Name: "distribution_id", Value: dist.DistributionID},
				{Name: "arn", Value: dist.ARN},
				{Name: "domain_name", Value: dist.DomainName},
				{Name: "status", Value: dist.Status},
				{Name: "account_id", Value: dist.AccountID},
			},
			ImplementedComponents: []*proto.InventoryItemImplementedComponent{
				{Identifier: componentID},
			},
		},
	}

	subjects := []*proto.Subject{
		{Type: proto.SubjectType_SUBJECT_TYPE_COMPONENT, Identifier: componentID},
		{Type: proto.SubjectType_SUBJECT_TYPE_INVENTORY_ITEM, Identifier: inventoryID},
	}

	for _, policyPath := range policyPaths {
		rootData, err := loadBundleRootData(policyPath, policyData)
		if err != nil {
			return nil, fmt.Errorf("loading bundle data for %s: %w", policyPath, err)
		}
		processor := policyManager.NewPolicyProcessor(pe.logger, labels, subjects, components, inventory, actors, pe.stepActivities, rootData)
		evidence, perr := processor.GenerateResults(ctx, policyPath, input)
		evidences = append(evidences, evidence...)
		if perr != nil {
			accumulatedErrors = errors.Join(accumulatedErrors, perr)
		}
	}

	return evidences, accumulatedErrors
}

func distributionBaseLabels() map[string]string {
	return map[string]string{
		"provider": "aws",
		"type":     "cloudfront-distribution",
	}
}

func loadBundleRootData(policyPath string, base map[string]interface{}) (map[string]interface{}, error) {
	candidates := []string{
		filepath.Join(filepath.Dir(policyPath), "data.json"),
		filepath.Join(policyPath, "data.json"),
	}
	for _, p := range candidates {
		raw, err := os.ReadFile(p)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("reading bundle data %s: %w", p, err)
		}
		var bundleData map[string]interface{}
		if err := json.Unmarshal(raw, &bundleData); err != nil {
			return nil, fmt.Errorf("parsing bundle data %s: %w", p, err)
		}
		merged := make(map[string]interface{}, len(bundleData)+len(base))
		for k, v := range bundleData {
			merged[k] = v
		}
		for k, v := range base {
			merged[k] = v
		}
		return merged, nil
	}
	return base, nil
}
