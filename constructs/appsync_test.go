package constructs

import (
	"strings"
	"testing"

	forge "github.com/nimbus-local/forge"
)

const testSchema = `
schema { query: Query }
type Query { hello: String }
`

func TestNewAppSync_GraphQLApiCreated(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		NewAppSync(ctx, "MyApi", &AppSyncArgs{Schema: testSchema})
	})

	if mocks.find("aws:appsync/graphQLApi:GraphQLApi") == nil {
		t.Error("GraphQL API not created")
	}
}

func TestNewAppSync_PhysicalNameQualified(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		NewAppSync(ctx, "MyApi", &AppSyncArgs{Schema: testSchema})
	})

	r := mocks.find("aws:appsync/graphQLApi:GraphQLApi")
	if r == nil {
		t.Fatal("GraphQL API not registered")
	}
	if r.inputs["name"].StringValue() != "myapp-test-MyApi" {
		t.Errorf("api name = %q, want myapp-test-MyApi", r.inputs["name"].StringValue())
	}
}

func TestNewAppSync_TagsApplied(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		NewAppSync(ctx, "MyApi", &AppSyncArgs{Schema: testSchema})
	})

	r := mocks.find("aws:appsync/graphQLApi:GraphQLApi")
	if r == nil {
		t.Fatal("GraphQL API not registered")
	}
	for _, tag := range []string{"forge:app", "forge:stage", "forge:name"} {
		assertTag(t, r.inputs, tag)
	}
}

func TestNewAppSync_SchemaSet(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		NewAppSync(ctx, "MyApi", &AppSyncArgs{Schema: testSchema})
	})

	r := mocks.find("aws:appsync/graphQLApi:GraphQLApi")
	if r == nil {
		t.Fatal("GraphQL API not registered")
	}
	if !strings.Contains(r.inputs["schema"].StringValue(), "Query") {
		t.Error("schema not set on GraphQL API")
	}
}

func TestNewAppSync_DefaultAuthIsAPIKey(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		NewAppSync(ctx, "MyApi", &AppSyncArgs{Schema: testSchema})
	})

	r := mocks.find("aws:appsync/graphQLApi:GraphQLApi")
	if r == nil {
		t.Fatal("GraphQL API not registered")
	}
	if r.inputs["authenticationType"].StringValue() != "API_KEY" {
		t.Errorf("authenticationType = %q, want API_KEY", r.inputs["authenticationType"].StringValue())
	}
}

func TestNewAppSync_APIKeyCreatedForAPIKeyAuth(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		NewAppSync(ctx, "MyApi", &AppSyncArgs{Schema: testSchema})
	})

	if mocks.find("aws:appsync/apiKey:ApiKey") == nil {
		t.Error("API key not created for API_KEY auth")
	}
}

func TestNewAppSync_NoAPIKeyForIAMAuth(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		NewAppSync(ctx, "MyApi", &AppSyncArgs{Schema: testSchema, Auth: AppSyncAuthIAM})
	})

	if mocks.find("aws:appsync/apiKey:ApiKey") != nil {
		t.Error("API key should not be created for AWS_IAM auth")
	}
}

func TestNewAppSync_NilArgsPanics(t *testing.T) {
	t.Parallel()
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for nil args (Schema required)")
		}
	}()
	runTest(t, func(ctx *forge.RunContext) {
		NewAppSync(ctx, "MyApi", nil)
	})
}

func TestNewAppSync_EmptySchemaPanics(t *testing.T) {
	t.Parallel()
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for empty Schema")
		}
	}()
	runTest(t, func(ctx *forge.RunContext) {
		NewAppSync(ctx, "MyApi", &AppSyncArgs{})
	})
}

func TestNewAppSync_LambdaDataSourceCreated(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		fn := NewFunction(ctx, "Resolver", &FunctionArgs{Handler: "bootstrap"})
		NewAppSync(ctx, "MyApi", &AppSyncArgs{
			Schema: testSchema,
			DataSources: []AppSyncDataSource{
				{Name: "LambdaDS", Type: AppSyncDataSourceLambda, Function: fn},
			},
		})
	})

	if mocks.find("aws:appsync/dataSource:DataSource") == nil {
		t.Error("data source not created")
	}
}

func TestNewAppSync_LambdaDataSourceType(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		fn := NewFunction(ctx, "Resolver", &FunctionArgs{Handler: "bootstrap"})
		NewAppSync(ctx, "MyApi", &AppSyncArgs{
			Schema: testSchema,
			DataSources: []AppSyncDataSource{
				{Name: "LambdaDS", Type: AppSyncDataSourceLambda, Function: fn},
			},
		})
	})

	r := mocks.find("aws:appsync/dataSource:DataSource")
	if r == nil {
		t.Fatal("data source not registered")
	}
	if r.inputs["type"].StringValue() != "AWS_LAMBDA" {
		t.Errorf("data source type = %q, want AWS_LAMBDA", r.inputs["type"].StringValue())
	}
}

func TestNewAppSync_LambdaDataSourceRoleCreated(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		fn := NewFunction(ctx, "Resolver", &FunctionArgs{Handler: "bootstrap"})
		NewAppSync(ctx, "MyApi", &AppSyncArgs{
			Schema: testSchema,
			DataSources: []AppSyncDataSource{
				{Name: "LambdaDS", Type: AppSyncDataSourceLambda, Function: fn},
			},
		})
	})

	// Expect at least 2 IAM roles: one for the Lambda function, one for the AppSync data source.
	roles := mocks.findAll("aws:iam/role:Role")
	if len(roles) < 2 {
		t.Errorf("expected at least 2 IAM roles (Lambda + AppSync DS), got %d", len(roles))
	}
}

func TestNewAppSync_LambdaDataSourceMissingFunctionPanics(t *testing.T) {
	t.Parallel()
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic when AWS_LAMBDA data source has no Function")
		}
	}()
	runTest(t, func(ctx *forge.RunContext) {
		NewAppSync(ctx, "MyApi", &AppSyncArgs{
			Schema: testSchema,
			DataSources: []AppSyncDataSource{
				{Name: "Bad", Type: AppSyncDataSourceLambda}, // no Function
			},
		})
	})
}

func TestNewAppSync_DynamoDBDataSourceCreated(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		tbl := NewDynamoDB(ctx, "Items", &DynamoDBArgs{
			Fields:       map[string]FieldType{"pk": FieldTypeString},
			PrimaryIndex: &PrimaryIndex{HashKey: "pk"},
		})
		NewAppSync(ctx, "MyApi", &AppSyncArgs{
			Schema: testSchema,
			DataSources: []AppSyncDataSource{
				{Name: "DynamoDS", Type: AppSyncDataSourceDynamoDB, Table: tbl},
			},
		})
	})

	r := mocks.find("aws:appsync/dataSource:DataSource")
	if r == nil {
		t.Fatal("data source not registered")
	}
	if r.inputs["type"].StringValue() != "AMAZON_DYNAMODB" {
		t.Errorf("data source type = %q, want AMAZON_DYNAMODB", r.inputs["type"].StringValue())
	}
}

func TestNewAppSync_DynamoDBDataSourceMissingTablePanics(t *testing.T) {
	t.Parallel()
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic when AMAZON_DYNAMODB data source has no Table")
		}
	}()
	runTest(t, func(ctx *forge.RunContext) {
		NewAppSync(ctx, "MyApi", &AppSyncArgs{
			Schema: testSchema,
			DataSources: []AppSyncDataSource{
				{Name: "Bad", Type: AppSyncDataSourceDynamoDB}, // no Table
			},
		})
	})
}

func TestNewAppSync_HTTPDataSourceCreated(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		NewAppSync(ctx, "MyApi", &AppSyncArgs{
			Schema: testSchema,
			DataSources: []AppSyncDataSource{
				{Name: "HttpDS", Type: AppSyncDataSourceHTTP, Endpoint: "https://example.com"},
			},
		})
	})

	r := mocks.find("aws:appsync/dataSource:DataSource")
	if r == nil {
		t.Fatal("data source not registered")
	}
	if r.inputs["type"].StringValue() != "HTTP" {
		t.Errorf("data source type = %q, want HTTP", r.inputs["type"].StringValue())
	}
}

func TestNewAppSync_HTTPDataSourceMissingEndpointPanics(t *testing.T) {
	t.Parallel()
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic when HTTP data source has no Endpoint")
		}
	}()
	runTest(t, func(ctx *forge.RunContext) {
		NewAppSync(ctx, "MyApi", &AppSyncArgs{
			Schema: testSchema,
			DataSources: []AppSyncDataSource{
				{Name: "Bad", Type: AppSyncDataSourceHTTP}, // no Endpoint
			},
		})
	})
}

func TestNewAppSync_NoneDataSourceCreated(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		NewAppSync(ctx, "MyApi", &AppSyncArgs{
			Schema: testSchema,
			DataSources: []AppSyncDataSource{
				{Name: "LocalDS", Type: AppSyncDataSourceNone},
			},
		})
	})

	r := mocks.find("aws:appsync/dataSource:DataSource")
	if r == nil {
		t.Fatal("data source not registered")
	}
	if r.inputs["type"].StringValue() != "NONE" {
		t.Errorf("data source type = %q, want NONE", r.inputs["type"].StringValue())
	}
}

func TestNewAppSync_NoneDataSourceNoRoleCreated(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		NewAppSync(ctx, "MyApi", &AppSyncArgs{
			Schema: testSchema,
			DataSources: []AppSyncDataSource{
				{Name: "LocalDS", Type: AppSyncDataSourceNone},
			},
		})
	})

	// NONE type should not create any IAM role.
	if mocks.find("aws:iam/role:Role") != nil {
		t.Error("IAM role should not be created for NONE data source")
	}
}

func TestNewAppSync_EmptyDataSourceNamePanics(t *testing.T) {
	t.Parallel()
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for empty data source Name")
		}
	}()
	runTest(t, func(ctx *forge.RunContext) {
		NewAppSync(ctx, "MyApi", &AppSyncArgs{
			Schema: testSchema,
			DataSources: []AppSyncDataSource{
				{Type: AppSyncDataSourceNone}, // Name empty
			},
		})
	})
}

func TestNewAppSync_ResolverCreated(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		fn := NewFunction(ctx, "Handler", &FunctionArgs{Handler: "bootstrap"})
		NewAppSync(ctx, "MyApi", &AppSyncArgs{
			Schema: testSchema,
			DataSources: []AppSyncDataSource{
				{Name: "LambdaDS", Type: AppSyncDataSourceLambda, Function: fn},
			},
			Resolvers: []AppSyncResolver{
				{TypeName: "Query", FieldName: "hello", DataSource: "LambdaDS"},
			},
		})
	})

	if mocks.find("aws:appsync/resolver:Resolver") == nil {
		t.Error("resolver not created")
	}
}

func TestNewAppSync_ResolverTypeName(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		fn := NewFunction(ctx, "Handler", &FunctionArgs{Handler: "bootstrap"})
		NewAppSync(ctx, "MyApi", &AppSyncArgs{
			Schema: testSchema,
			DataSources: []AppSyncDataSource{
				{Name: "LambdaDS", Type: AppSyncDataSourceLambda, Function: fn},
			},
			Resolvers: []AppSyncResolver{
				{TypeName: "Query", FieldName: "hello", DataSource: "LambdaDS"},
			},
		})
	})

	r := mocks.find("aws:appsync/resolver:Resolver")
	if r == nil {
		t.Fatal("resolver not registered")
	}
	if r.inputs["type"].StringValue() != "Query" {
		t.Errorf("resolver type = %q, want Query", r.inputs["type"].StringValue())
	}
	if r.inputs["field"].StringValue() != "hello" {
		t.Errorf("resolver field = %q, want hello", r.inputs["field"].StringValue())
	}
}

func TestNewAppSync_ResolverDefaultTemplates(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		fn := NewFunction(ctx, "Handler", &FunctionArgs{Handler: "bootstrap"})
		NewAppSync(ctx, "MyApi", &AppSyncArgs{
			Schema: testSchema,
			DataSources: []AppSyncDataSource{
				{Name: "LambdaDS", Type: AppSyncDataSourceLambda, Function: fn},
			},
			Resolvers: []AppSyncResolver{
				{TypeName: "Query", FieldName: "hello", DataSource: "LambdaDS"},
			},
		})
	})

	r := mocks.find("aws:appsync/resolver:Resolver")
	if r == nil {
		t.Fatal("resolver not registered")
	}
	if !strings.Contains(r.inputs["requestTemplate"].StringValue(), "Invoke") {
		t.Error("default requestTemplate should contain Lambda Invoke operation")
	}
	if !strings.Contains(r.inputs["responseTemplate"].StringValue(), "$util.toJson") {
		t.Error("default responseTemplate should contain $util.toJson")
	}
}

func TestNewAppSync_ResolverCustomTemplates(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		fn := NewFunction(ctx, "Handler", &FunctionArgs{Handler: "bootstrap"})
		NewAppSync(ctx, "MyApi", &AppSyncArgs{
			Schema: testSchema,
			DataSources: []AppSyncDataSource{
				{Name: "LambdaDS", Type: AppSyncDataSourceLambda, Function: fn},
			},
			Resolvers: []AppSyncResolver{
				{
					TypeName:         "Query",
					FieldName:        "hello",
					DataSource:       "LambdaDS",
					RequestTemplate:  `{"custom":"request"}`,
					ResponseTemplate: `{"custom":"response"}`,
				},
			},
		})
	})

	r := mocks.find("aws:appsync/resolver:Resolver")
	if r == nil {
		t.Fatal("resolver not registered")
	}
	if r.inputs["requestTemplate"].StringValue() != `{"custom":"request"}` {
		t.Errorf("requestTemplate = %q, want custom template", r.inputs["requestTemplate"].StringValue())
	}
	if r.inputs["responseTemplate"].StringValue() != `{"custom":"response"}` {
		t.Errorf("responseTemplate = %q, want custom template", r.inputs["responseTemplate"].StringValue())
	}
}

func TestNewAppSync_ResolverUnknownDataSourcePanics(t *testing.T) {
	t.Parallel()
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic when resolver references unknown data source")
		}
	}()
	runTest(t, func(ctx *forge.RunContext) {
		NewAppSync(ctx, "MyApi", &AppSyncArgs{
			Schema: testSchema,
			Resolvers: []AppSyncResolver{
				{TypeName: "Query", FieldName: "hello", DataSource: "DoesNotExist"},
			},
		})
	})
}

func TestNewAppSync_ResolverEmptyTypeNamePanics(t *testing.T) {
	t.Parallel()
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for resolver with empty TypeName")
		}
	}()
	runTest(t, func(ctx *forge.RunContext) {
		fn := NewFunction(ctx, "Handler", &FunctionArgs{Handler: "bootstrap"})
		NewAppSync(ctx, "MyApi", &AppSyncArgs{
			Schema: testSchema,
			DataSources: []AppSyncDataSource{
				{Name: "DS", Type: AppSyncDataSourceLambda, Function: fn},
			},
			Resolvers: []AppSyncResolver{
				{FieldName: "hello", DataSource: "DS"}, // TypeName empty
			},
		})
	})
}

func TestNewAppSync_MultipleResolvers(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		fn := NewFunction(ctx, "Handler", &FunctionArgs{Handler: "bootstrap"})
		NewAppSync(ctx, "MyApi", &AppSyncArgs{
			Schema: testSchema,
			DataSources: []AppSyncDataSource{
				{Name: "LambdaDS", Type: AppSyncDataSourceLambda, Function: fn},
			},
			Resolvers: []AppSyncResolver{
				{TypeName: "Query", FieldName: "getItem", DataSource: "LambdaDS"},
				{TypeName: "Query", FieldName: "listItems", DataSource: "LambdaDS"},
				{TypeName: "Mutation", FieldName: "createItem", DataSource: "LambdaDS"},
			},
		})
	})

	resolvers := mocks.findAll("aws:appsync/resolver:Resolver")
	if len(resolvers) != 3 {
		t.Errorf("expected 3 resolvers, got %d", len(resolvers))
	}
}

func TestNewAppSync_MultipleDataSources(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		fn := NewFunction(ctx, "Handler", &FunctionArgs{Handler: "bootstrap"})
		tbl := NewDynamoDB(ctx, "Items", &DynamoDBArgs{
			Fields:       map[string]FieldType{"pk": FieldTypeString},
			PrimaryIndex: &PrimaryIndex{HashKey: "pk"},
		})
		NewAppSync(ctx, "MyApi", &AppSyncArgs{
			Schema: testSchema,
			DataSources: []AppSyncDataSource{
				{Name: "LambdaDS", Type: AppSyncDataSourceLambda, Function: fn},
				{Name: "DynamoDS", Type: AppSyncDataSourceDynamoDB, Table: tbl},
			},
		})
	})

	ds := mocks.findAll("aws:appsync/dataSource:DataSource")
	if len(ds) != 2 {
		t.Errorf("expected 2 data sources, got %d", len(ds))
	}
}

func TestNewAppSync_LinkEnvKeysAPIKey(t *testing.T) {
	t.Parallel()
	runTest(t, func(ctx *forge.RunContext) {
		as := NewAppSync(ctx, "MyApi", &AppSyncArgs{Schema: testSchema})
		env := as.LinkEnv()
		if _, ok := env["SST_APPSYNC_MY_API_URL"]; !ok {
			t.Error("LinkEnv missing SST_APPSYNC_MY_API_URL")
		}
		if _, ok := env["SST_APPSYNC_MY_API_API_ID"]; !ok {
			t.Error("LinkEnv missing SST_APPSYNC_MY_API_API_ID")
		}
		if _, ok := env["SST_APPSYNC_MY_API_API_KEY"]; !ok {
			t.Error("LinkEnv missing SST_APPSYNC_MY_API_API_KEY (expected for API_KEY auth)")
		}
		if len(env) != 3 {
			t.Errorf("LinkEnv has %d keys, want 3", len(env))
		}
	})
}

func TestNewAppSync_LinkEnvKeysIAM(t *testing.T) {
	t.Parallel()
	runTest(t, func(ctx *forge.RunContext) {
		as := NewAppSync(ctx, "MyApi", &AppSyncArgs{Schema: testSchema, Auth: AppSyncAuthIAM})
		env := as.LinkEnv()
		if _, ok := env["SST_APPSYNC_MY_API_URL"]; !ok {
			t.Error("LinkEnv missing SST_APPSYNC_MY_API_URL")
		}
		if _, ok := env["SST_APPSYNC_MY_API_API_ID"]; !ok {
			t.Error("LinkEnv missing SST_APPSYNC_MY_API_API_ID")
		}
		if _, ok := env["SST_APPSYNC_MY_API_API_KEY"]; ok {
			t.Error("LinkEnv should not contain API_KEY for AWS_IAM auth")
		}
		if len(env) != 2 {
			t.Errorf("LinkEnv has %d keys, want 2", len(env))
		}
	})
}

func TestNewAppSync_CamelCaseLinkEnvKey(t *testing.T) {
	t.Parallel()
	runTest(t, func(ctx *forge.RunContext) {
		as := NewAppSync(ctx, "UserGraph", &AppSyncArgs{Schema: testSchema, Auth: AppSyncAuthIAM})
		env := as.LinkEnv()
		if _, ok := env["SST_APPSYNC_USER_GRAPH_URL"]; !ok {
			t.Error("LinkEnv missing SST_APPSYNC_USER_GRAPH_URL")
		}
	})
}

func TestNewAppSync_LinkName(t *testing.T) {
	t.Parallel()
	runTest(t, func(ctx *forge.RunContext) {
		as := NewAppSync(ctx, "MyApi", &AppSyncArgs{Schema: testSchema})
		if as.LinkName() != "MyApi" {
			t.Errorf("LinkName = %q, want MyApi", as.LinkName())
		}
	})
}

func TestNewAppSync_ImplementsLinkable(t *testing.T) {
	t.Parallel()
	runTest(t, func(ctx *forge.RunContext) {
		as := NewAppSync(ctx, "MyApi", &AppSyncArgs{Schema: testSchema})
		var _ forge.Linkable = as
	})
}

func TestNewAppSync_GrantCreatesIAMPolicy(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		fn := NewFunction(ctx, "Consumer", &FunctionArgs{Handler: "bootstrap"})
		as := NewAppSync(ctx, "MyApi", &AppSyncArgs{Schema: testSchema})
		as.Grant(fn.Role())
	})

	policies := mocks.findAll("aws:iam/rolePolicy:RolePolicy")
	found := false
	for _, p := range policies {
		if v, ok := p.inputs["policy"]; ok && strings.Contains(v.StringValue(), "appsync:GraphQL") {
			found = true
			break
		}
	}
	if !found {
		t.Error("no IAM role policy grants appsync:GraphQL")
	}
}

func TestNewAppSync_UnsupportedDataSourceTypePanics(t *testing.T) {
	t.Parallel()
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for unsupported data source type")
		}
	}()
	runTest(t, func(ctx *forge.RunContext) {
		NewAppSync(ctx, "MyApi", &AppSyncArgs{
			Schema: testSchema,
			DataSources: []AppSyncDataSource{
				{Name: "Bad", Type: AppSyncDataSourceType("UNSUPPORTED")},
			},
		})
	})
}

func TestNewAppSync_APIKeyExpiryOverride(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		NewAppSync(ctx, "MyApi", &AppSyncArgs{
			Schema:       testSchema,
			ApiKeyExpiry: "2027-01-01T00:00:00Z",
		})
	})

	r := mocks.find("aws:appsync/apiKey:ApiKey")
	if r == nil {
		t.Fatal("API key not registered")
	}
	if r.inputs["expires"].StringValue() != "2027-01-01T00:00:00Z" {
		t.Errorf("api key expires = %q, want 2027-01-01T00:00:00Z", r.inputs["expires"].StringValue())
	}
}
