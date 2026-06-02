package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/compliance-framework/agent/runner"
	"github.com/compliance-framework/agent/runner/proto"
	"github.com/compliance-framework/plugin-aws-cloudfront/internal"
	"github.com/hashicorp/go-hclog"
	goplugin "github.com/hashicorp/go-plugin"
)

type CompliancePlugin struct {
	logger     hclog.Logger
	config     *internal.PluginConfig
	policyData map[string]interface{}
}

func (l *CompliancePlugin) Configure(req *proto.ConfigureRequest) (*proto.ConfigureResponse, error) {
	parsedConfig, err := internal.ParseConfig(req.GetConfig())
	if err != nil {
		return nil, err
	}
	l.config = parsedConfig
	if req.GetPolicyData() != nil {
		l.policyData = req.GetPolicyData().AsMap()
	}
	return &proto.ConfigureResponse{}, nil
}

func (l *CompliancePlugin) Init(req *proto.InitRequest, apiHelper runner.ApiHelper) (*proto.InitResponse, error) {
	ctx := context.Background()
	subjectTemplates := []*proto.SubjectTemplate{
		{
			Name:                "cloudfront-distribution",
			Type:                proto.SubjectType_SUBJECT_TYPE_COMPONENT,
			TitleTemplate:       "CloudFront Distribution {{ .distribution_id }}",
			DescriptionTemplate: "AWS CloudFront Distribution {{ .distribution_id }} ({{ .domain_name }}) in account {{ .account_id }}.",
			PurposeTemplate:     "Represents an AWS CloudFront distribution evaluated for TLS compliance posture.",
			IdentityLabelKeys:   []string{"account_id", "distribution_id"},
			LabelSchema: []*proto.SubjectLabelSchema{
				{Key: "account_id", Description: "AWS account ID"},
				{Key: "distribution_id", Description: "CloudFront distribution ID"},
				{Key: "domain_name", Description: "CloudFront distribution domain name"},
			},
		},
	}
	return runner.InitWithSubjectsAndRisksFromPolicies(ctx, l.logger, req, apiHelper, subjectTemplates)
}

func (l *CompliancePlugin) Eval(request *proto.EvalRequest, apiHelper runner.ApiHelper) (*proto.EvalResponse, error) {
	ctx := context.Background()
	activities := make([]*proto.Activity, 0)

	if request == nil {
		return &proto.EvalResponse{Status: proto.ExecutionStatus_FAILURE}, fmt.Errorf("eval request is nil")
	}

	dataFetcher := internal.NewDataFetcher(l.logger, l.config)
	distributions, err := dataFetcher.FetchData(ctx)
	if err != nil {
		return &proto.EvalResponse{Status: proto.ExecutionStatus_FAILURE}, fmt.Errorf("failed to fetch data: %w", err)
	}

	policyEvaluator := internal.NewPolicyEvaluator(ctx, l.logger, activities)

	var allEvidences []*proto.Evidence
	var evalErrors error
	for _, dist := range distributions {
		distEvidences, err := policyEvaluator.Eval(ctx, dist, request.GetPolicyPaths(), l.policyData, l.config.PolicyLabels)
		allEvidences = append(allEvidences, distEvidences...)
		if err != nil {
			evalErrors = errors.Join(evalErrors, fmt.Errorf("evaluating distribution %s: %w", dist.DistributionID, err))
		}
	}

	if err := apiHelper.CreateEvidence(ctx, allEvidences); err != nil {
		l.logger.Error("Error creating evidence", "error", err)
		return &proto.EvalResponse{Status: proto.ExecutionStatus_FAILURE}, err
	}

	if evalErrors != nil {
		return &proto.EvalResponse{Status: proto.ExecutionStatus_FAILURE}, evalErrors
	}

	return &proto.EvalResponse{Status: proto.ExecutionStatus_SUCCESS}, nil
}

func main() {
	logger := hclog.New(&hclog.LoggerOptions{
		Level:      hclog.Debug,
		JSONFormat: true,
	})

	compliancePluginObj := &CompliancePlugin{logger: logger}
	logger.Debug("initiating plugin")

	goplugin.Serve(&goplugin.ServeConfig{
		HandshakeConfig: runner.HandshakeConfig,
		Plugins: map[string]goplugin.Plugin{
			"runner": &runner.RunnerV2GRPCPlugin{Impl: compliancePluginObj},
		},
		GRPCServer: goplugin.DefaultGRPCServer,
	})
}
