# AWS CloudFront CCF Plugin

This plugin collects read-only metadata for the AWS CloudFront distributions in an account, evaluates CCF Rego policy bundles against each distribution, and emits evidence back through the CCF agent.

## How it fits in CCF

The CCF agent starts this binary through HashiCorp `go-plugin` (runner protocol v2), passes configuration and policy paths over gRPC, and receives generated evidence through the runner callback. This repository does not call the CCF API directly.

During `Init`, the plugin registers the `cloudfront-distribution` subject template (identified by `account_id` and `distribution_id`) and the subject and risk templates discovered from the supplied policy bundles.

## Default policy bundle mapping

| Repository | Primary input |
| --- | --- |
| [`plugin-aws-cloudfront-policies`](https://github.com/compliance-framework/plugin-aws-cloudfront-policies) | one CloudFront distribution per evaluation |

## Configuration

| Key | Required | Description |
| --- | --- | --- |
| `policy_labels` | no | JSON object of extra labels added to every evidence, e.g. `{"team":"platform"}`. |

CloudFront is a global service, so there is no region setting: every AWS call (CloudFront and `sts:GetCallerIdentity`) goes to `us-east-1`. AWS credentials come from the default AWS SDK credential chain.

Any agent-supplied `policy_data` is passed through to Rego as `data.*`, on top of the bundle's own `data.json` (agent-supplied keys win).

## Data collected

For every distribution returned by the CloudFront `ListDistributions` API (all pages), the plugin reads its tags with `ListTagsForResource` and builds this policy input:

| Field | Source |
| --- | --- |
| `account_id` | the caller's AWS account |
| `distribution_id` | the distribution ID |
| `arn` | the distribution ARN |
| `domain_name` | the distribution's CloudFront domain name |
| `status` | the distribution status |
| `enabled` | whether the distribution is enabled |
| `certificate_arn` | the viewer certificate's ACM certificate ARN (omitted when there is none) |
| `cache_behaviours` | `path_pattern` and `viewer_protocol_policy` for the default cache behaviour (reported as path `/*`) and each path-based cache behaviour |
| `tags` | the distribution's tags (empty when they cannot be read) |

Each evidence carries the labels `provider=aws`, `type=cloudfront-distribution`, `distribution_id`, `resource_arn`, `account_id` and `domain_name`, plus any `policy_labels`.

## Development

Run the local test suite with:

```shell
go test ./...
```

Or use the Makefile wrapper:

```shell
make test
```

Build the plugin binary with:

```shell
make build
```

This writes the compiled plugin to `dist/plugin`.

## Releasing

- Releases come from release-please PRs: merging one tags `vX.Y.Z` and publishes the plugin.
- Release candidates (`vX.Y.Z-rcN`) come from the `cut-prerelease` workflow.

## Related repositories

- [plugin-aws-cloudfront-policies](https://github.com/compliance-framework/plugin-aws-cloudfront-policies)
