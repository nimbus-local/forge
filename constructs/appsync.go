package constructs

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	forge "github.com/nimbus-local/forge"
	"github.com/pulumi/pulumi-aws/sdk/v7/go/aws/appsync"
	"github.com/pulumi/pulumi-aws/sdk/v7/go/aws/iam"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

// AppSync is an AppSync GraphQL API construct.
//
// LinkEnv keys injected into linked Functions:
//
//	SST_APPSYNC_<NAME>_URL     — GraphQL endpoint URL
//	SST_APPSYNC_<NAME>_API_ID  — AppSync API ID
//	SST_APPSYNC_<NAME>_API_KEY — API key value (only when Auth = AppSyncAuthAPIKey)
//
// Linked Functions automatically receive appsync:GraphQL on this API.
type AppSync struct {
	name   string
	api    *appsync.GraphQLApi
	apiKey *appsync.ApiKey
	ctx    *forge.RunContext
}

// AppSyncAuth is the primary authentication mode for an AppSync API.
type AppSyncAuth string

const (
	AppSyncAuthAPIKey  AppSyncAuth = "API_KEY"
	AppSyncAuthIAM     AppSyncAuth = "AWS_IAM"
	AppSyncAuthCognito AppSyncAuth = "AMAZON_COGNITO_USER_POOLS"
	AppSyncAuthOIDC    AppSyncAuth = "OPENID_CONNECT"
	AppSyncAuthLambda  AppSyncAuth = "AWS_LAMBDA"
)

// AppSyncDataSourceType identifies the backing service for an AppSync data source.
type AppSyncDataSourceType string

const (
	// AppSyncDataSourceLambda routes resolver calls to a Lambda function.
	AppSyncDataSourceLambda AppSyncDataSourceType = "AWS_LAMBDA"
	// AppSyncDataSourceDynamoDB routes resolver calls to a DynamoDB table.
	AppSyncDataSourceDynamoDB AppSyncDataSourceType = "AMAZON_DYNAMODB"
	// AppSyncDataSourceHTTP routes resolver calls to an HTTP endpoint.
	AppSyncDataSourceHTTP AppSyncDataSourceType = "HTTP"
	// AppSyncDataSourceNone is a local resolver that needs no backing service.
	AppSyncDataSourceNone AppSyncDataSourceType = "NONE"
)

// AppSyncDataSource wires a named backing service into the GraphQL API.
type AppSyncDataSource struct {
	// Name is the logical identifier referenced by resolvers. Must start with a
	// letter or underscore and contain only letters, digits, and underscores —
	// this is an AWS AppSync constraint.
	Name string

	// Type selects the backing service.
	Type AppSyncDataSourceType

	// Function is the Lambda data source (required when Type is AppSyncDataSourceLambda).
	// AppSync receives permission to invoke this function.
	Function *Function

	// Table is the DynamoDB data source (required when Type is AppSyncDataSourceDynamoDB).
	// AppSync receives permission to read and write this table.
	Table *DynamoDB

	// Endpoint is the HTTP endpoint URL (required when Type is AppSyncDataSourceHTTP).
	Endpoint string
}

// AppSyncResolver wires a schema field to a data source.
type AppSyncResolver struct {
	// TypeName is the parent GraphQL type (e.g. "Query", "Mutation").
	TypeName string

	// FieldName is the field on TypeName to resolve (e.g. "getItem").
	FieldName string

	// DataSource is the Name of the AppSyncDataSource to use for this resolver.
	DataSource string

	// RequestTemplate is the VTL request mapping template. For AWS_LAMBDA data
	// sources a Lambda pass-through template is used when this is empty. For all
	// other types a template must be provided.
	RequestTemplate string

	// ResponseTemplate is the VTL response mapping template. Defaults to a
	// $util.toJson($context.result) pass-through when empty.
	ResponseTemplate string
}

// AppSyncArgs configures an AppSync GraphQL API construct.
type AppSyncArgs struct {
	// Schema is the GraphQL SDL schema string. Required.
	Schema string

	// Auth sets the primary authentication mode. Defaults to AppSyncAuthAPIKey.
	// When AppSyncAuthAPIKey is used, an API key is automatically created with a
	// 365-day expiry and its value is exported in LinkEnv as
	// SST_APPSYNC_<NAME>_API_KEY.
	//
	// Set ApiKeyExpiry to pin the expiry date and avoid a key update on every
	// deploy. Format: RFC3339, e.g. "2027-01-01T00:00:00Z".
	Auth AppSyncAuth

	// ApiKeyExpiry overrides the auto-computed API key expiry (RFC3339 timestamp).
	// Only used when Auth is AppSyncAuthAPIKey. AWS limits expiry to ≤365 days
	// from creation. When empty, 365 days from the current wall-clock time is used,
	// which may trigger a key update on each deploy as the date advances.
	ApiKeyExpiry string

	// DataSources define the named backends available to resolvers.
	DataSources []AppSyncDataSource

	// Resolvers wire schema fields to data sources.
	Resolvers []AppSyncResolver

	// Tags merged with stage-level tags on every resource.
	Tags map[string]string
}

// NewAppSync creates an AppSync GraphQL API construct.
func NewAppSync(ctx *forge.RunContext, name string, args *AppSyncArgs) *AppSync {
	if args == nil {
		args = &AppSyncArgs{}
	}
	if args.Auth == "" {
		args.Auth = AppSyncAuthAPIKey
	}
	if args.Schema == "" {
		panic(fmt.Sprintf("forge [%s]: AppSyncArgs.Schema is required", name))
	}

	pctx := ctx.Pulumi()
	tags := mergedTags(defaultTags(ctx, name), args.Tags)

	// ── GraphQL API ───────────────────────────────────────────────────────────
	api, err := appsync.NewGraphQLApi(pctx, name, &appsync.GraphQLApiArgs{
		Name:               pulumi.String(qualifiedName(ctx, name)),
		AuthenticationType: pulumi.String(string(args.Auth)),
		Schema:             pulumi.String(args.Schema),
		Tags:               tags,
	})
	panicOnErr(err, name+": graphql api")

	as := &AppSync{name: name, api: api, ctx: ctx}

	// ── API key ───────────────────────────────────────────────────────────────
	if args.Auth == AppSyncAuthAPIKey {
		expiry := args.ApiKeyExpiry
		if expiry == "" {
			expiry = time.Now().UTC().Add(365 * 24 * time.Hour).Truncate(time.Hour).Format(time.RFC3339)
		}
		apiKey, err := appsync.NewApiKey(pctx, name+"-key", &appsync.ApiKeyArgs{
			ApiId:   api.ID(),
			Expires: pulumi.String(expiry),
		})
		panicOnErr(err, name+": api key")
		as.apiKey = apiKey
	}

	// ── Data sources ──────────────────────────────────────────────────────────
	dsMap := map[string]*appsync.DataSource{}
	for _, ds := range args.DataSources {
		if ds.Name == "" {
			panic(fmt.Sprintf("forge [%s]: AppSyncDataSource.Name must not be empty", name))
		}
		dsMap[ds.Name] = as.addDataSource(ds, tags)
	}

	// ── Resolvers ─────────────────────────────────────────────────────────────
	for _, r := range args.Resolvers {
		if r.TypeName == "" || r.FieldName == "" {
			panic(fmt.Sprintf("forge [%s]: resolver TypeName and FieldName must not be empty", name))
		}
		ds, ok := dsMap[r.DataSource]
		if !ok {
			panic(fmt.Sprintf("forge [%s]: resolver %s.%s references unknown data source %q",
				name, r.TypeName, r.FieldName, r.DataSource))
		}
		as.addResolver(r, ds)
	}

	return as
}

// addDataSource creates the AWS DataSource and any required IAM role.
func (as *AppSync) addDataSource(ds AppSyncDataSource, tags pulumi.StringMap) *appsync.DataSource {
	pctx := as.ctx.Pulumi()
	resName := as.name + "-ds-" + strings.ToLower(ds.Name)

	args := &appsync.DataSourceArgs{
		ApiId: as.api.ID(),
		Name:  pulumi.String(ds.Name),
		Type:  pulumi.String(string(ds.Type)),
	}

	switch ds.Type {
	case AppSyncDataSourceLambda:
		if ds.Function == nil {
			panic(fmt.Sprintf("forge [%s]: data source %q with type AWS_LAMBDA requires Function", as.name, ds.Name))
		}
		role := as.dsRole(resName, tags, ds.Function.ARN(), func(arn string) string {
			return jsonPolicy([]map[string]interface{}{{
				"Effect":   "Allow",
				"Action":   "lambda:InvokeFunction",
				"Resource": arn,
			}})
		})
		args.ServiceRoleArn = role.Arn
		args.LambdaConfig = &appsync.DataSourceLambdaConfigArgs{
			FunctionArn: ds.Function.ARN(),
		}

	case AppSyncDataSourceDynamoDB:
		if ds.Table == nil {
			panic(fmt.Sprintf("forge [%s]: data source %q with type AMAZON_DYNAMODB requires Table", as.name, ds.Name))
		}
		role := as.dsRole(resName, tags, ds.Table.ARN(), func(arn string) string {
			return jsonPolicy([]map[string]interface{}{{
				"Effect": "Allow",
				"Action": []string{
					"dynamodb:GetItem", "dynamodb:PutItem", "dynamodb:UpdateItem",
					"dynamodb:DeleteItem", "dynamodb:Query", "dynamodb:Scan",
				},
				"Resource": []string{arn, arn + "/index/*"},
			}})
		})
		args.ServiceRoleArn = role.Arn
		args.DynamodbConfig = &appsync.DataSourceDynamodbConfigArgs{
			TableName: ds.Table.TableName(),
		}

	case AppSyncDataSourceHTTP:
		if ds.Endpoint == "" {
			panic(fmt.Sprintf("forge [%s]: data source %q with type HTTP requires Endpoint", as.name, ds.Name))
		}
		args.HttpConfig = &appsync.DataSourceHttpConfigArgs{
			Endpoint: pulumi.String(ds.Endpoint),
		}

	case AppSyncDataSourceNone:
		// NONE type: local resolvers, no backing service or role needed.

	default:
		panic(fmt.Sprintf("forge [%s]: data source %q has unsupported type %q", as.name, ds.Name, ds.Type))
	}

	res, err := appsync.NewDataSource(pctx, resName, args)
	panicOnErr(err, as.name+": data source "+ds.Name)
	return res
}

// dsRole creates an IAM role with the appsync.amazonaws.com principal that AppSync
// assumes when calling the backing service. policyFn receives the resource ARN
// (resolved at apply time) and returns the JSON policy body.
func (as *AppSync) dsRole(name string, tags pulumi.StringMap, resourceArn pulumi.StringOutput, policyFn func(string) string) *iam.Role {
	pctx := as.ctx.Pulumi()

	role, err := iam.NewRole(pctx, name+"-role", &iam.RoleArgs{
		AssumeRolePolicy: pulumi.String(`{
			"Version": "2012-10-17",
			"Statement": [{
				"Effect": "Allow",
				"Principal": { "Service": "appsync.amazonaws.com" },
				"Action": "sts:AssumeRole"
			}]
		}`),
		Tags: tags,
	})
	panicOnErr(err, name+": ds role")

	policy := resourceArn.ApplyT(func(arn string) (string, error) {
		return policyFn(arn), nil
	}).(pulumi.StringOutput)

	_, err = iam.NewRolePolicy(pctx, name+"-policy", &iam.RolePolicyArgs{
		Role:   role.Name,
		Policy: policy,
	})
	panicOnErr(err, name+": ds role policy")

	return role
}

// addResolver creates the AWS Resolver for one AppSyncResolver.
func (as *AppSync) addResolver(r AppSyncResolver, ds *appsync.DataSource) {
	pctx := as.ctx.Pulumi()
	resName := fmt.Sprintf("%s-res-%s-%s",
		as.name, strings.ToLower(r.TypeName), strings.ToLower(r.FieldName))

	req := r.RequestTemplate
	if req == "" {
		// Lambda pass-through: forward $context.args as the payload.
		req = `{"version":"2017-02-28","operation":"Invoke","payload":$util.toJson($context.args)}`
	}
	resp := r.ResponseTemplate
	if resp == "" {
		resp = `$util.toJson($context.result)`
	}

	_, err := appsync.NewResolver(pctx, resName, &appsync.ResolverArgs{
		ApiId:            as.api.ID(),
		Type:             pulumi.String(r.TypeName),
		Field:            pulumi.String(r.FieldName),
		DataSource:       ds.Name,
		RequestTemplate:  pulumi.String(req),
		ResponseTemplate: pulumi.String(resp),
	})
	panicOnErr(err, as.name+": resolver "+r.TypeName+"."+r.FieldName)
}

// ── Accessors ─────────────────────────────────────────────────────────────────

// URL returns the GraphQL endpoint URL as a Pulumi output.
// Equivalent to the "GRAPHQL" entry in the API's Uris map.
func (as *AppSync) URL() pulumi.StringOutput {
	return as.api.Uris.ApplyT(func(uris map[string]string) string {
		return uris["GRAPHQL"]
	}).(pulumi.StringOutput)
}

// APIID returns the AppSync API ID as a Pulumi output.
func (as *AppSync) APIID() pulumi.StringOutput {
	return as.api.ID().ToStringOutput()
}

// GraphQLApi returns the underlying GraphQL API resource.
func (as *AppSync) GraphQLApi() *appsync.GraphQLApi { return as.api }

// ── IAM grant ─────────────────────────────────────────────────────────────────

// Grant attaches an inline IAM policy giving the role appsync:GraphQL on every
// field in this API. Called automatically when a Function links this construct.
func (as *AppSync) Grant(role *iam.Role) {
	pctx := as.ctx.Pulumi()
	policy := as.api.Arn.ApplyT(func(arn string) (string, error) {
		return jsonPolicy([]map[string]interface{}{{
			"Effect":   "Allow",
			"Action":   "appsync:GraphQL",
			"Resource": arn + "/*",
		}}), nil
	}).(pulumi.StringOutput)

	_, err := iam.NewRolePolicy(pctx, as.name+"-appsync-grant", &iam.RolePolicyArgs{
		Role:   role.Name,
		Policy: policy,
	})
	panicOnErr(err, as.name+": appsync:GraphQL grant")
}

// ── Linkable ──────────────────────────────────────────────────────────────────

// LinkEnv implements forge.Linkable.
func (as *AppSync) LinkEnv() pulumi.StringMap {
	key := envKey(as.name)
	env := pulumi.StringMap{
		"SST_APPSYNC_" + key + "_URL":    as.URL(),
		"SST_APPSYNC_" + key + "_API_ID": as.APIID(),
	}
	if as.apiKey != nil {
		env["SST_APPSYNC_"+key+"_API_KEY"] = as.apiKey.Key
	}
	return env
}

// LinkName implements forge.Linkable.
func (as *AppSync) LinkName() string { return as.name }

// ── helpers ───────────────────────────────────────────────────────────────────

// jsonPolicy serialises an IAM policy document with the given statements.
func jsonPolicy(statements []map[string]interface{}) string {
	doc := map[string]interface{}{
		"Version":   "2012-10-17",
		"Statement": statements,
	}
	b, _ := json.Marshal(doc)
	return string(b)
}
