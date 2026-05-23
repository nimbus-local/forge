module checklist-full-infra

go 1.24

require (
	github.com/pulumi/pulumi-aws/sdk/v6 v6.27.0
	github.com/pulumi/pulumi/sdk/v3 v3.148.0
	github.com/nimbus-local/forge v0.0.0
)

replace github.com/nimbus-local/forge => ../../..
