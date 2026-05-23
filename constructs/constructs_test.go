package constructs

import (
	"strings"
	"sync"
	"testing"

	forge "github.com/nimbus-local/forge"
	"github.com/pulumi/pulumi/sdk/v3/go/common/resource"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

// ── mock resource monitor ─────────────────────────────────────────────────────

// testMocks implements pulumi.MockResourceMonitor.
// It records every resource registration so tests can assert on inputs
// (physical names, tags, environment variables) after pulumi.RunErr returns.
type testMocks struct {
	mu        sync.Mutex
	resources []capturedResource
}

type capturedResource struct {
	typeToken string
	name      string
	inputs    resource.PropertyMap
}

func newMocks() *testMocks { return &testMocks{} }

// find returns the first captured resource matching the given type token, or nil.
func (m *testMocks) find(typeToken string) *capturedResource {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := range m.resources {
		if m.resources[i].typeToken == typeToken {
			return &m.resources[i]
		}
	}
	return nil
}

// findAll returns all captured resources matching the given type token.
func (m *testMocks) findAll(typeToken string) []capturedResource {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []capturedResource
	for _, r := range m.resources {
		if r.typeToken == typeToken {
			out = append(out, r)
		}
	}
	return out
}

func (m *testMocks) NewResource(args pulumi.MockResourceArgs) (string, resource.PropertyMap, error) {
	outputs := args.Inputs.Copy()

	// str safely extracts a string from the input map.
	str := func(key string) string {
		if v, ok := args.Inputs[resource.PropertyKey(key)]; ok && v.IsString() {
			return v.StringValue()
		}
		return args.Name
	}

	switch args.TypeToken {
	case "aws:lambda/function:Function":
		name := str("name")
		outputs["arn"] = resource.NewStringProperty(
			"arn:aws:lambda:us-east-1:123456789012:function:" + name,
		)
	case "aws:iam/role:Role":
		outputs["arn"] = resource.NewStringProperty(
			"arn:aws:iam::123456789012:role/" + args.Name,
		)
	case "aws:dynamodb/table:Table":
		name := str("name")
		outputs["arn"] = resource.NewStringProperty(
			"arn:aws:dynamodb:us-east-1:123456789012:table/" + name,
		)
		outputs["streamArn"] = resource.NewStringProperty("")
	case "aws:s3/bucket:Bucket":
		bname := str("bucket")
		outputs["arn"] = resource.NewStringProperty("arn:aws:s3:::" + bname)
	case "aws:sqs/queue:Queue":
		name := str("name")
		outputs["arn"] = resource.NewStringProperty(
			"arn:aws:sqs:us-east-1:123456789012:" + name,
		)
		outputs["url"] = resource.NewStringProperty(
			"https://sqs.us-east-1.amazonaws.com/123456789012/" + name,
		)
	case "aws:sns/topic:Topic":
		name := str("name")
		outputs["arn"] = resource.NewStringProperty(
			"arn:aws:sns:us-east-1:123456789012:" + name,
		)
	case "aws:scheduler/schedule:Schedule":
		outputs["arn"] = resource.NewStringProperty(
			"arn:aws:scheduler:us-east-1:123456789012:schedule/default/" + args.Name,
		)
	case "aws:apigatewayv2/api:Api":
		name := str("name")
		apiID := args.Name + "-api-id"
		outputs["id"] = resource.NewStringProperty(apiID)
		outputs["apiEndpoint"] = resource.NewStringProperty("https://" + apiID + ".execute-api.us-east-1.amazonaws.com")
		outputs["executionArn"] = resource.NewStringProperty(
			"arn:aws:execute-api:us-east-1:123456789012:" + apiID,
		)
		_ = name
	case "aws:apigatewayv2/stage:Stage":
		apiID := "mock-api-id"
		outputs["invokeUrl"] = resource.NewStringProperty(
			"https://" + apiID + ".execute-api.us-east-1.amazonaws.com",
		)
	case "aws:apigatewayv2/integration:Integration":
		outputs["id"] = resource.NewStringProperty(args.Name + "-int-id")
	case "aws:apigatewayv2/route:Route":
		outputs["id"] = resource.NewStringProperty(args.Name + "-route-id")
	}

	m.mu.Lock()
	m.resources = append(m.resources, capturedResource{
		typeToken: args.TypeToken,
		name:      args.Name,
		inputs:    args.Inputs,
	})
	m.mu.Unlock()

	return args.Name + "-id", outputs, nil
}

func (m *testMocks) Call(args pulumi.MockCallArgs) (resource.PropertyMap, error) {
	// Handle SSM parameter lookups (NewSecret).
	if strings.Contains(args.Token, "ssm") || strings.Contains(args.Token, "getParameter") {
		path := ""
		if v, ok := args.Args[resource.PropertyKey("name")]; ok && v.IsString() {
			path = v.StringValue()
		}
		return resource.PropertyMap{
			"arn":           resource.NewStringProperty("arn:aws:ssm:us-east-1:123456789012:parameter" + path),
			"name":          resource.NewStringProperty(path),
			"type":          resource.NewStringProperty("SecureString"),
			"value":         resource.NewStringProperty("mock-secret-value"),
			"version":       resource.NewNumberProperty(1),
			"insecureValue": resource.NewStringProperty(""),
		}, nil
	}
	return args.Args, nil
}

// ── helpers ───────────────────────────────────────────────────────────────────

// testApp is the forge.AppConfig used in all tests.
var testApp = &forge.AppConfig{Name: "myapp"}

// runTest creates a mock Pulumi context, constructs a RunContext, and runs fn.
// Returned mocks can be inspected after runTest returns.
func runTest(t *testing.T, fn func(*forge.RunContext)) *testMocks {
	t.Helper()
	mocks := newMocks()
	err := pulumi.RunErr(func(pctx *pulumi.Context) error {
		ctx := forge.NewRunContext(pctx, testApp, "test", "123456789012")
		fn(ctx)
		return nil
	}, pulumi.WithMocks("myapp", "test", mocks))
	if err != nil {
		t.Fatalf("pulumi.RunErr: %v", err)
	}
	return mocks
}

// assertTag verifies that the resource tags contain the given key.
func assertTag(t *testing.T, inputs resource.PropertyMap, key string) {
	t.Helper()
	tags, ok := inputs[resource.PropertyKey("tags")]
	if !ok {
		t.Errorf("resource missing tags entirely")
		return
	}
	if !tags.IsObject() {
		t.Errorf("tags is not an object")
		return
	}
	if _, ok := tags.ObjectValue()[resource.PropertyKey(key)]; !ok {
		t.Errorf("tags missing key %q", key)
	}
}

// assertEnvVar checks that the Lambda function's environment variables include key.
func assertEnvVar(t *testing.T, inputs resource.PropertyMap, key string) {
	t.Helper()
	envProp, ok := inputs[resource.PropertyKey("environment")]
	if !ok {
		t.Errorf("Lambda missing environment block")
		return
	}
	varsProp, ok := envProp.ObjectValue()[resource.PropertyKey("variables")]
	if !ok {
		t.Errorf("Lambda environment missing variables")
		return
	}
	if _, ok := varsProp.ObjectValue()[resource.PropertyKey(key)]; !ok {
		t.Errorf("Lambda environment missing key %q", key)
	}
}

// ── testLinkable ──────────────────────────────────────────────────────────────

// testLinkable is a forge.Linkable backed by concrete (non-output) string values,
// used to verify that NewFunction merges linked env vars correctly.
type testLinkable struct {
	name string
	env  pulumi.StringMap
}

func (l *testLinkable) LinkEnv() pulumi.StringMap { return l.env }
func (l *testLinkable) LinkName() string          { return l.name }

// ── Function tests ────────────────────────────────────────────────────────────

func TestNewFunction_LinkEnvKeys(t *testing.T) {
	t.Parallel()
	runTest(t, func(ctx *forge.RunContext) {
		fn := NewFunction(ctx, "MyFn", &FunctionArgs{Handler: "bootstrap"})

		linkEnv := fn.LinkEnv()
		if _, ok := linkEnv["SST_FUNCTION_MY_FN_ARN"]; !ok {
			t.Error("LinkEnv missing SST_FUNCTION_MY_FN_ARN")
		}
		if len(linkEnv) != 1 {
			t.Errorf("LinkEnv has %d keys, want 1", len(linkEnv))
		}
	})
}

func TestNewFunction_LinkName(t *testing.T) {
	t.Parallel()
	runTest(t, func(ctx *forge.RunContext) {
		fn := NewFunction(ctx, "MyFn", nil)
		if fn.LinkName() != "MyFn" {
			t.Errorf("LinkName = %q, want %q", fn.LinkName(), "MyFn")
		}
	})
}

func TestNewFunction_PhysicalNameQualified(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		NewFunction(ctx, "Api", &FunctionArgs{Handler: "bootstrap"})
	})

	r := mocks.find("aws:lambda/function:Function")
	if r == nil {
		t.Fatal("Lambda function resource not registered")
	}
	name := r.inputs["name"].StringValue()
	if name != "myapp-test-Api" {
		t.Errorf("physical name = %q, want %q", name, "myapp-test-Api")
	}
}

func TestNewFunction_DefaultsApplied(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		NewFunction(ctx, "Fn", nil)
	})

	r := mocks.find("aws:lambda/function:Function")
	if r == nil {
		t.Fatal("Lambda function not registered")
	}
	if r.inputs["runtime"].StringValue() != RuntimeGo {
		t.Errorf("runtime = %q, want %q", r.inputs["runtime"].StringValue(), RuntimeGo)
	}
	if r.inputs["timeout"].NumberValue() != 10 {
		t.Errorf("timeout = %v, want 10", r.inputs["timeout"].NumberValue())
	}
	if r.inputs["memorySize"].NumberValue() != 128 {
		t.Errorf("memorySize = %v, want 128", r.inputs["memorySize"].NumberValue())
	}
}

func TestNewFunction_TagsApplied(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		NewFunction(ctx, "Fn", nil)
	})

	r := mocks.find("aws:lambda/function:Function")
	if r == nil {
		t.Fatal("Lambda function not registered")
	}
	for _, tag := range []string{"forge:app", "forge:stage", "forge:name"} {
		assertTag(t, r.inputs, tag)
	}
}

func TestNewFunction_ForgeStageInjected(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		NewFunction(ctx, "Fn", nil)
	})

	r := mocks.find("aws:lambda/function:Function")
	if r == nil {
		t.Fatal("Lambda function not registered")
	}
	assertEnvVar(t, r.inputs, "FORGE_STAGE")
}

func TestNewFunction_LinkInjectsEnvVars(t *testing.T) {
	t.Parallel()
	link := &testLinkable{
		name: "MyTable",
		env: pulumi.StringMap{
			"SST_TABLE_MY_TABLE_NAME": pulumi.String("myapp-test-MyTable"),
			"SST_TABLE_MY_TABLE_ARN":  pulumi.String("arn:aws:dynamodb:::table/myapp-test-MyTable"),
		},
	}
	mocks := runTest(t, func(ctx *forge.RunContext) {
		NewFunction(ctx, "Fn", &FunctionArgs{
			Handler: "bootstrap",
			Link:    []forge.Linkable{link},
		})
	})

	r := mocks.find("aws:lambda/function:Function")
	if r == nil {
		t.Fatal("Lambda function not registered")
	}
	assertEnvVar(t, r.inputs, "SST_TABLE_MY_TABLE_NAME")
	assertEnvVar(t, r.inputs, "SST_TABLE_MY_TABLE_ARN")
}

func TestNewFunction_ExplicitEnvVarsMerged(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		NewFunction(ctx, "Fn", &FunctionArgs{
			Handler:     "bootstrap",
			Environment: map[string]string{"MY_VAR": "my-value"},
		})
	})

	r := mocks.find("aws:lambda/function:Function")
	if r == nil {
		t.Fatal("Lambda function not registered")
	}
	assertEnvVar(t, r.inputs, "MY_VAR")
}

func TestNewFunction_IAMRoleCreated(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		NewFunction(ctx, "Fn", nil)
	})

	if mocks.find("aws:iam/role:Role") == nil {
		t.Error("IAM role not registered")
	}
}

func TestNewFunction_LogGroupCreated(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		NewFunction(ctx, "Fn", nil)
	})

	r := mocks.find("aws:cloudwatch/logGroup:LogGroup")
	if r == nil {
		t.Fatal("CloudWatch log group not registered")
	}
	// Log group name must include the qualified function name.
	logName := r.inputs["name"].StringValue()
	if !strings.Contains(logName, "myapp-test-Fn") {
		t.Errorf("log group name %q does not contain qualified function name", logName)
	}
}

// ── DynamoDB tests ────────────────────────────────────────────────────────────

func TestNewDynamoDB_LinkEnvKeys(t *testing.T) {
	t.Parallel()
	runTest(t, func(ctx *forge.RunContext) {
		tbl := NewDynamoDB(ctx, "UsersTable", &DynamoDBArgs{
			Fields:       map[string]FieldType{"pk": FieldTypeString},
			PrimaryIndex: &PrimaryIndex{HashKey: "pk"},
		})

		linkEnv := tbl.LinkEnv()
		if _, ok := linkEnv["SST_TABLE_USERS_TABLE_NAME"]; !ok {
			t.Error("LinkEnv missing SST_TABLE_USERS_TABLE_NAME")
		}
		if _, ok := linkEnv["SST_TABLE_USERS_TABLE_ARN"]; !ok {
			t.Error("LinkEnv missing SST_TABLE_USERS_TABLE_ARN")
		}
		if len(linkEnv) != 2 {
			t.Errorf("LinkEnv has %d keys, want 2", len(linkEnv))
		}
	})
}

func TestNewDynamoDB_LinkName(t *testing.T) {
	t.Parallel()
	runTest(t, func(ctx *forge.RunContext) {
		tbl := NewDynamoDB(ctx, "UsersTable", &DynamoDBArgs{
			Fields:       map[string]FieldType{"pk": FieldTypeString},
			PrimaryIndex: &PrimaryIndex{HashKey: "pk"},
		})
		if tbl.LinkName() != "UsersTable" {
			t.Errorf("LinkName = %q, want %q", tbl.LinkName(), "UsersTable")
		}
	})
}

func TestNewDynamoDB_PhysicalNameQualified(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		NewDynamoDB(ctx, "Items", &DynamoDBArgs{
			Fields:       map[string]FieldType{"pk": FieldTypeString},
			PrimaryIndex: &PrimaryIndex{HashKey: "pk"},
		})
	})

	r := mocks.find("aws:dynamodb/table:Table")
	if r == nil {
		t.Fatal("DynamoDB table not registered")
	}
	name := r.inputs["name"].StringValue()
	if name != "myapp-test-Items" {
		t.Errorf("physical name = %q, want %q", name, "myapp-test-Items")
	}
}

func TestNewDynamoDB_TagsApplied(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		NewDynamoDB(ctx, "T", &DynamoDBArgs{
			Fields:       map[string]FieldType{"pk": FieldTypeString},
			PrimaryIndex: &PrimaryIndex{HashKey: "pk"},
		})
	})
	r := mocks.find("aws:dynamodb/table:Table")
	if r == nil {
		t.Fatal("DynamoDB table not registered")
	}
	for _, tag := range []string{"forge:app", "forge:stage", "forge:name"} {
		assertTag(t, r.inputs, tag)
	}
}

func TestNewDynamoDB_DefaultBillingMode(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		NewDynamoDB(ctx, "T", &DynamoDBArgs{
			Fields:       map[string]FieldType{"pk": FieldTypeString},
			PrimaryIndex: &PrimaryIndex{HashKey: "pk"},
		})
	})

	r := mocks.find("aws:dynamodb/table:Table")
	if r == nil {
		t.Fatal("DynamoDB table not registered")
	}
	if r.inputs["billingMode"].StringValue() != "PAY_PER_REQUEST" {
		t.Errorf("billingMode = %q, want PAY_PER_REQUEST", r.inputs["billingMode"].StringValue())
	}
}

func TestNewDynamoDB_NilPrimaryIndexPanics(t *testing.T) {
	t.Parallel()
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for nil PrimaryIndex")
		}
	}()

	mocks := newMocks()
	_ = pulumi.RunErr(func(pctx *pulumi.Context) error {
		ctx := forge.NewRunContext(pctx, testApp, "test", "123456789012")
		NewDynamoDB(ctx, "T", &DynamoDBArgs{
			Fields: map[string]FieldType{"pk": FieldTypeString},
			// PrimaryIndex intentionally omitted
		})
		return nil
	}, pulumi.WithMocks("myapp", "test", mocks))
}

// ── Bucket tests ──────────────────────────────────────────────────────────────

func TestNewBucket_LinkEnvKeys(t *testing.T) {
	t.Parallel()
	runTest(t, func(ctx *forge.RunContext) {
		b := NewBucket(ctx, "Uploads", nil)

		linkEnv := b.LinkEnv()
		if _, ok := linkEnv["SST_BUCKET_UPLOADS_NAME"]; !ok {
			t.Error("LinkEnv missing SST_BUCKET_UPLOADS_NAME")
		}
		if _, ok := linkEnv["SST_BUCKET_UPLOADS_ARN"]; !ok {
			t.Error("LinkEnv missing SST_BUCKET_UPLOADS_ARN")
		}
		if len(linkEnv) != 2 {
			t.Errorf("LinkEnv has %d keys, want 2", len(linkEnv))
		}
	})
}

func TestNewBucket_LinkName(t *testing.T) {
	t.Parallel()
	runTest(t, func(ctx *forge.RunContext) {
		b := NewBucket(ctx, "Uploads", nil)
		if b.LinkName() != "Uploads" {
			t.Errorf("LinkName = %q, want %q", b.LinkName(), "Uploads")
		}
	})
}

func TestNewBucket_BucketNameFormat(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		NewBucket(ctx, "Uploads", nil)
	})

	r := mocks.find("aws:s3/bucket:Bucket")
	if r == nil {
		t.Fatal("S3 bucket not registered")
	}
	// bucketName = "<app>-<stage>-<name>-<accountID>" lowercased
	bname := r.inputs["bucket"].StringValue()
	want := "myapp-test-uploads-123456789012"
	if bname != want {
		t.Errorf("bucket name = %q, want %q", bname, want)
	}
}

func TestNewBucket_PublicAccessBlockCreatedByDefault(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		NewBucket(ctx, "B", nil)
	})

	if mocks.find("aws:s3/bucketPublicAccessBlock:BucketPublicAccessBlock") == nil {
		t.Error("public access block not created for private bucket")
	}
}

func TestNewBucket_PublicSkipsAccessBlock(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		NewBucket(ctx, "B", &BucketArgs{Public: true})
	})

	if mocks.find("aws:s3/bucketPublicAccessBlock:BucketPublicAccessBlock") != nil {
		t.Error("public access block should not be created for public bucket")
	}
}

func TestNewBucket_VersioningCreatedWhenEnabled(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		NewBucket(ctx, "B", &BucketArgs{Versioning: true})
	})

	if mocks.find("aws:s3/bucketVersioningV2:BucketVersioningV2") == nil {
		t.Error("versioning resource not created when Versioning: true")
	}
}

func TestNewBucket_TagsApplied(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		NewBucket(ctx, "B", nil)
	})
	r := mocks.find("aws:s3/bucket:Bucket")
	if r == nil {
		t.Fatal("S3 bucket not registered")
	}
	for _, tag := range []string{"forge:app", "forge:stage", "forge:name"} {
		assertTag(t, r.inputs, tag)
	}
}

// ── Queue tests ───────────────────────────────────────────────────────────────

func TestNewQueue_LinkEnvKeys(t *testing.T) {
	t.Parallel()
	runTest(t, func(ctx *forge.RunContext) {
		q := NewQueue(ctx, "Jobs", nil)

		linkEnv := q.LinkEnv()
		if _, ok := linkEnv["SST_QUEUE_JOBS_URL"]; !ok {
			t.Error("LinkEnv missing SST_QUEUE_JOBS_URL")
		}
		if _, ok := linkEnv["SST_QUEUE_JOBS_ARN"]; !ok {
			t.Error("LinkEnv missing SST_QUEUE_JOBS_ARN")
		}
		if len(linkEnv) != 2 {
			t.Errorf("LinkEnv has %d keys, want 2", len(linkEnv))
		}
	})
}

func TestNewQueue_LinkName(t *testing.T) {
	t.Parallel()
	runTest(t, func(ctx *forge.RunContext) {
		q := NewQueue(ctx, "Jobs", nil)
		if q.LinkName() != "Jobs" {
			t.Errorf("LinkName = %q, want %q", q.LinkName(), "Jobs")
		}
	})
}

func TestNewQueue_PhysicalNameQualified(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		NewQueue(ctx, "Jobs", nil)
	})

	r := mocks.find("aws:sqs/queue:Queue")
	if r == nil {
		t.Fatal("SQS queue not registered")
	}
	name := r.inputs["name"].StringValue()
	if name != "myapp-test-Jobs" {
		t.Errorf("queue name = %q, want %q", name, "myapp-test-Jobs")
	}
}

func TestNewQueue_DLQCreatedWhenEnabled(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		NewQueue(ctx, "Jobs", &QueueArgs{DeadLetterQueue: true})
	})

	// Expect two SQS queues: main + DLQ.
	queues := mocks.findAll("aws:sqs/queue:Queue")
	if len(queues) != 2 {
		t.Errorf("expected 2 SQS queues (main + DLQ), got %d", len(queues))
	}
}

func TestNewQueue_DLQNotCreatedByDefault(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		NewQueue(ctx, "Jobs", nil)
	})

	queues := mocks.findAll("aws:sqs/queue:Queue")
	if len(queues) != 1 {
		t.Errorf("expected 1 SQS queue, got %d", len(queues))
	}
}

func TestNewQueue_ConsumerLambdaCreated(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		NewQueue(ctx, "Jobs", &QueueArgs{
			Consumer: &FunctionArgs{Handler: "bootstrap"},
		})
	})

	if mocks.find("aws:lambda/function:Function") == nil {
		t.Error("consumer Lambda not created")
	}
	if mocks.find("aws:lambda/eventSourceMapping:EventSourceMapping") == nil {
		t.Error("event source mapping not created for consumer")
	}
}

// ── Topic tests ───────────────────────────────────────────────────────────────

func TestNewTopic_LinkEnvKeys(t *testing.T) {
	t.Parallel()
	runTest(t, func(ctx *forge.RunContext) {
		tp := NewTopic(ctx, "Events", nil)

		linkEnv := tp.LinkEnv()
		if _, ok := linkEnv["SST_TOPIC_EVENTS_ARN"]; !ok {
			t.Error("LinkEnv missing SST_TOPIC_EVENTS_ARN")
		}
		if len(linkEnv) != 1 {
			t.Errorf("LinkEnv has %d keys, want 1", len(linkEnv))
		}
	})
}

func TestNewTopic_LinkName(t *testing.T) {
	t.Parallel()
	runTest(t, func(ctx *forge.RunContext) {
		tp := NewTopic(ctx, "Events", nil)
		if tp.LinkName() != "Events" {
			t.Errorf("LinkName = %q, want %q", tp.LinkName(), "Events")
		}
	})
}

func TestNewTopic_PhysicalNameQualified(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		NewTopic(ctx, "Events", nil)
	})

	r := mocks.find("aws:sns/topic:Topic")
	if r == nil {
		t.Fatal("SNS topic not registered")
	}
	name := r.inputs["name"].StringValue()
	if name != "myapp-test-Events" {
		t.Errorf("topic name = %q, want %q", name, "myapp-test-Events")
	}
}

func TestNewTopic_SubscriberLambdaCreated(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		NewTopic(ctx, "Events", &TopicArgs{
			Subscribers: []*FunctionArgs{
				{Handler: "bootstrap"},
			},
		})
	})

	if mocks.find("aws:lambda/function:Function") == nil {
		t.Error("subscriber Lambda not created")
	}
	if mocks.find("aws:sns/topicSubscription:TopicSubscription") == nil {
		t.Error("SNS subscription not created")
	}
}

func TestNewTopic_FIFONameSuffix(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		NewTopic(ctx, "Orders", &TopicArgs{FIFO: true})
	})

	r := mocks.find("aws:sns/topic:Topic")
	if r == nil {
		t.Fatal("SNS topic not registered")
	}
	name := r.inputs["name"].StringValue()
	if !strings.HasSuffix(name, ".fifo") {
		t.Errorf("FIFO topic name %q should end with .fifo", name)
	}
}

// ── Secret tests ──────────────────────────────────────────────────────────────

func TestNewSecret_LinkEnvKeys(t *testing.T) {
	t.Parallel()
	runTest(t, func(ctx *forge.RunContext) {
		s := NewSecret(ctx, "DbPassword", nil)

		linkEnv := s.LinkEnv()
		if _, ok := linkEnv["SST_SECRET_DB_PASSWORD"]; !ok {
			t.Error("LinkEnv missing SST_SECRET_DB_PASSWORD")
		}
		if len(linkEnv) != 1 {
			t.Errorf("LinkEnv has %d keys, want 1", len(linkEnv))
		}
	})
}

func TestNewSecret_LinkName(t *testing.T) {
	t.Parallel()
	runTest(t, func(ctx *forge.RunContext) {
		s := NewSecret(ctx, "DbPassword", nil)
		if s.LinkName() != "DbPassword" {
			t.Errorf("LinkName = %q, want %q", s.LinkName(), "DbPassword")
		}
	})
}

func TestNewSecret_DefaultUsedWhenSSMFails(t *testing.T) {
	t.Parallel()
	// Use a mock that always fails SSM calls so we exercise the Default path.
	mocks := newMocks()
	err := pulumi.RunErr(func(pctx *pulumi.Context) error {
		ctx := forge.NewRunContext(pctx, testApp, "test", "123456789012")
		s := NewSecret(ctx, "ApiKey", &SecretArgs{Default: "fallback-value"})
		if s.LinkName() != "ApiKey" {
			t.Errorf("LinkName = %q, want ApiKey", s.LinkName())
		}
		return nil
	}, pulumi.WithMocks("myapp", "test", &failSSMMocks{inner: mocks}))
	if err != nil {
		t.Fatalf("pulumi.RunErr: %v", err)
	}
}

func TestNewSecret_PanicsWithoutDefaultWhenSSMFails(t *testing.T) {
	t.Parallel()
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic when SSM fails and no Default is set")
		}
	}()

	mocks := newMocks()
	_ = pulumi.RunErr(func(pctx *pulumi.Context) error {
		ctx := forge.NewRunContext(pctx, testApp, "test", "123456789012")
		NewSecret(ctx, "ApiKey", nil) // no Default → should panic
		return nil
	}, pulumi.WithMocks("myapp", "test", &failSSMMocks{inner: mocks}))
}

// failSSMMocks wraps testMocks but always errors on SSM calls.
type failSSMMocks struct{ inner *testMocks }

func (m *failSSMMocks) NewResource(args pulumi.MockResourceArgs) (string, resource.PropertyMap, error) {
	return m.inner.NewResource(args)
}
func (m *failSSMMocks) Call(args pulumi.MockCallArgs) (resource.PropertyMap, error) {
	if strings.Contains(args.Token, "ssm") || strings.Contains(args.Token, "getParameter") {
		return nil, &mockSSMNotFound{}
	}
	return args.Args, nil
}

type mockSSMNotFound struct{}

func (e *mockSSMNotFound) Error() string { return "ParameterNotFound" }

// ── Cron tests ────────────────────────────────────────────────────────────────

func TestNewCron_ScheduleAndFunctionCreated(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		NewCron(ctx, "Hourly", &CronArgs{
			Schedule: "rate(1 hour)",
			Job:      &FunctionArgs{Handler: "bootstrap"},
		})
	})

	if mocks.find("aws:lambda/function:Function") == nil {
		t.Error("Lambda function not created for cron job")
	}
	if mocks.find("aws:scheduler/schedule:Schedule") == nil {
		t.Error("EventBridge schedule not created")
	}
}

func TestNewCron_PhysicalScheduleNameQualified(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		NewCron(ctx, "Daily", &CronArgs{
			Schedule: "rate(1 day)",
			Job:      &FunctionArgs{Handler: "bootstrap"},
		})
	})

	r := mocks.find("aws:scheduler/schedule:Schedule")
	if r == nil {
		t.Fatal("EventBridge schedule not registered")
	}
	name := r.inputs["name"].StringValue()
	if name != "myapp-test-Daily" {
		t.Errorf("schedule name = %q, want %q", name, "myapp-test-Daily")
	}
}

func TestNewCron_EmptySchedulePanics(t *testing.T) {
	t.Parallel()
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for empty Schedule")
		}
	}()

	mocks := newMocks()
	_ = pulumi.RunErr(func(pctx *pulumi.Context) error {
		ctx := forge.NewRunContext(pctx, testApp, "test", "123456789012")
		NewCron(ctx, "Bad", &CronArgs{}) // empty Schedule → should panic
		return nil
	}, pulumi.WithMocks("myapp", "test", mocks))
}

func TestNewCron_IAMRoleForSchedulerCreated(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		NewCron(ctx, "Hourly", &CronArgs{
			Schedule: "rate(1 hour)",
			Job:      &FunctionArgs{Handler: "bootstrap"},
		})
	})

	// Expect two IAM roles: one for the Lambda, one for the EventBridge scheduler.
	roles := mocks.findAll("aws:iam/role:Role")
	if len(roles) < 2 {
		t.Errorf("expected at least 2 IAM roles (Lambda + scheduler), got %d", len(roles))
	}
}

// ── ApiGatewayV2 tests ────────────────────────────────────────────────────────

func TestNewApiGatewayV2_LinkEnvKeys(t *testing.T) {
	t.Parallel()
	runTest(t, func(ctx *forge.RunContext) {
		api := NewApiGatewayV2(ctx, "MyApi", nil)

		linkEnv := api.LinkEnv()
		if _, ok := linkEnv["SST_API_MY_API_URL"]; !ok {
			t.Error("LinkEnv missing SST_API_MY_API_URL")
		}
		if len(linkEnv) != 1 {
			t.Errorf("LinkEnv has %d keys, want 1", len(linkEnv))
		}
	})
}

func TestNewApiGatewayV2_LinkName(t *testing.T) {
	t.Parallel()
	runTest(t, func(ctx *forge.RunContext) {
		api := NewApiGatewayV2(ctx, "MyApi", nil)
		if api.LinkName() != "MyApi" {
			t.Errorf("LinkName = %q, want %q", api.LinkName(), "MyApi")
		}
	})
}

func TestNewApiGatewayV2_PhysicalNameQualified(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		NewApiGatewayV2(ctx, "MyApi", nil)
	})

	r := mocks.find("aws:apigatewayv2/api:Api")
	if r == nil {
		t.Fatal("API Gateway not registered")
	}
	name := r.inputs["name"].StringValue()
	if name != "myapp-test-MyApi" {
		t.Errorf("api name = %q, want %q", name, "myapp-test-MyApi")
	}
}

func TestNewApiGatewayV2_StageCreated(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		NewApiGatewayV2(ctx, "MyApi", nil)
	})

	r := mocks.find("aws:apigatewayv2/stage:Stage")
	if r == nil {
		t.Fatal("API Gateway stage not registered")
	}
	if r.inputs["name"].StringValue() != "$default" {
		t.Errorf("stage name = %q, want $default", r.inputs["name"].StringValue())
	}
	if !r.inputs["autoDeploy"].IsBool() || !r.inputs["autoDeploy"].BoolValue() {
		t.Error("autoDeploy should be true")
	}
}

func TestNewApiGatewayV2_TagsApplied(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		NewApiGatewayV2(ctx, "MyApi", nil)
	})

	r := mocks.find("aws:apigatewayv2/api:Api")
	if r == nil {
		t.Fatal("API Gateway not registered")
	}
	for _, tag := range []string{"forge:app", "forge:stage", "forge:name"} {
		assertTag(t, r.inputs, tag)
	}
}

func TestApiGatewayV2_RouteCreatesLambdaAndIntegration(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		api := NewApiGatewayV2(ctx, "MyApi", nil)
		api.Route("GET /users", &RouteArgs{Handler: "bootstrap"})
	})

	if mocks.find("aws:lambda/function:Function") == nil {
		t.Error("route Lambda not created")
	}
	if mocks.find("aws:apigatewayv2/integration:Integration") == nil {
		t.Error("API Gateway integration not created")
	}
	if mocks.find("aws:apigatewayv2/route:Route") == nil {
		t.Error("API Gateway route not created")
	}
	if mocks.find("aws:lambda/permission:Permission") == nil {
		t.Error("Lambda invoke permission not created")
	}
}

func TestApiGatewayV2_RouteWithExistingFunction(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		fn := NewFunction(ctx, "Handler", &FunctionArgs{Handler: "bootstrap"})
		api := NewApiGatewayV2(ctx, "MyApi", nil)
		api.Route("POST /submit", &RouteArgs{Function: fn})
	})

	// One Lambda (pre-created), one integration, one route.
	fns := mocks.findAll("aws:lambda/function:Function")
	if len(fns) != 1 {
		t.Errorf("expected 1 Lambda (pre-created, not duplicated), got %d", len(fns))
	}
	if mocks.find("aws:apigatewayv2/integration:Integration") == nil {
		t.Error("integration not created for existing-function route")
	}
}

func TestApiGatewayV2_MultipleRoutes(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		api := NewApiGatewayV2(ctx, "MyApi", nil)
		api.Route("GET /a", &RouteArgs{Handler: "bootstrap"})
		api.Route("POST /b", &RouteArgs{Handler: "bootstrap"})
		api.Route("DELETE /c", &RouteArgs{Handler: "bootstrap"})
	})

	routes := mocks.findAll("aws:apigatewayv2/route:Route")
	if len(routes) != 3 {
		t.Errorf("expected 3 routes, got %d", len(routes))
	}
	lambdas := mocks.findAll("aws:lambda/function:Function")
	if len(lambdas) != 3 {
		t.Errorf("expected 3 Lambdas (one per route), got %d", len(lambdas))
	}
}

// ── Additional DynamoDB tests ─────────────────────────────────────────────────

func TestNewDynamoDB_WithGSI(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		NewDynamoDB(ctx, "Items", &DynamoDBArgs{
			Fields: map[string]FieldType{
				"pk":  FieldTypeString,
				"gsi": FieldTypeString,
			},
			PrimaryIndex: &PrimaryIndex{HashKey: "pk"},
			GlobalIndexes: []GlobalIndex{
				{Name: "gsi-index", HashKey: "gsi"},
			},
		})
	})

	r := mocks.find("aws:dynamodb/table:Table")
	if r == nil {
		t.Fatal("DynamoDB table not registered")
	}
	gsis, ok := r.inputs["globalSecondaryIndexes"]
	if !ok {
		t.Fatal("globalSecondaryIndexes not set on table")
	}
	if !gsis.IsArray() || len(gsis.ArrayValue()) != 1 {
		t.Errorf("expected 1 GSI, got: %v", gsis)
	}
}

func TestNewDynamoDB_WithStreams(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		NewDynamoDB(ctx, "Items", &DynamoDBArgs{
			Fields:        map[string]FieldType{"pk": FieldTypeString},
			PrimaryIndex:  &PrimaryIndex{HashKey: "pk"},
			StreamEnabled: true,
		})
	})

	r := mocks.find("aws:dynamodb/table:Table")
	if r == nil {
		t.Fatal("DynamoDB table not registered")
	}
	if !r.inputs["streamEnabled"].IsBool() || !r.inputs["streamEnabled"].BoolValue() {
		t.Error("streamEnabled should be true")
	}
	// Default stream view type.
	if r.inputs["streamViewType"].StringValue() != "NEW_AND_OLD_IMAGES" {
		t.Errorf("streamViewType = %q, want NEW_AND_OLD_IMAGES", r.inputs["streamViewType"].StringValue())
	}
}

func TestNewDynamoDB_WithRangeKey(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		NewDynamoDB(ctx, "Events", &DynamoDBArgs{
			Fields: map[string]FieldType{
				"pk": FieldTypeString,
				"sk": FieldTypeString,
			},
			PrimaryIndex: &PrimaryIndex{HashKey: "pk", RangeKey: "sk"},
		})
	})

	r := mocks.find("aws:dynamodb/table:Table")
	if r == nil {
		t.Fatal("DynamoDB table not registered")
	}
	if r.inputs["rangeKey"].StringValue() != "sk" {
		t.Errorf("rangeKey = %q, want %q", r.inputs["rangeKey"].StringValue(), "sk")
	}
}

// ── staticsite pure-helper tests ──────────────────────────────────────────────
// These helpers are unexported but accessible because the test is in package constructs.

func TestDetectMIME(t *testing.T) {
	t.Parallel()
	cases := []struct {
		file string
		want string
	}{
		{"index.html", "text/html; charset=utf-8"},
		{"styles.css", "text/css; charset=utf-8"},
		{"app.js", "application/javascript; charset=utf-8"},
		{"data.json", "application/json"},
		{"image.png", "image/png"},
		{"photo.jpg", "image/jpeg"},
		{"icon.ico", "image/x-icon"},
		{"font.woff2", "font/woff2"},
		{"unknown.forgetestunknown", "application/octet-stream"},
		{"no-extension", "application/octet-stream"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.file, func(t *testing.T) {
			t.Parallel()
			if got := detectMIME(tc.file); got != tc.want {
				t.Errorf("detectMIME(%q) = %q, want %q", tc.file, got, tc.want)
			}
		})
	}
}

func TestSiteCacheControl(t *testing.T) {
	t.Parallel()
	immutable := "public, max-age=31536000, immutable"
	revalidate := "public, max-age=0, must-revalidate"

	cases := []struct{ key, want string }{
		{"_next/static/chunks/main.js", immutable},
		{"assets/logo.png", immutable},
		{"static/favicon.ico", immutable},
		{"index.html", revalidate},
		{"api/data.json", revalidate},
		{"robots.txt", revalidate},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.key, func(t *testing.T) {
			t.Parallel()
			if got := siteCacheControl(tc.key); got != tc.want {
				t.Errorf("siteCacheControl(%q) = %q, want %q", tc.key, got, tc.want)
			}
		})
	}
}

func TestSiteResName(t *testing.T) {
	t.Parallel()
	cases := []struct{ prefix, key, want string }{
		{"mysite", "index.html", "mysite-obj-index-html"},
		{"mysite", "_next/static/main.js", "mysite-obj-_next--static--main-js"},
		{"mysite", "assets/logo.png", "mysite-obj-assets--logo-png"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.key, func(t *testing.T) {
			t.Parallel()
			if got := siteResName(tc.prefix, tc.key); got != tc.want {
				t.Errorf("siteResName(%q, %q) = %q, want %q", tc.prefix, tc.key, got, tc.want)
			}
		})
	}
}

func TestCfBucketPolicy(t *testing.T) {
	t.Parallel()
	bucketARN := "arn:aws:s3:::my-bucket"
	distARN := "arn:aws:cloudfront::123456789012:distribution/ABCDEF"

	policy := cfBucketPolicy(bucketARN, distARN)

	if !strings.Contains(policy, bucketARN) {
		t.Errorf("policy missing bucketARN %q", bucketARN)
	}
	if !strings.Contains(policy, distARN) {
		t.Errorf("policy missing distARN %q", distARN)
	}
	if !strings.Contains(policy, "s3:GetObject") {
		t.Error("policy missing s3:GetObject action")
	}
	if !strings.Contains(policy, "cloudfront.amazonaws.com") {
		t.Error("policy missing CloudFront principal")
	}
}

// ── addApiInvokePolicy test ───────────────────────────────────────────────────

func TestAddApiInvokePolicy(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		// Create a real (mock-backed) IAM role to pass to addApiInvokePolicy.
		fn := NewFunction(ctx, "Fn", nil)
		apiArn := pulumi.String("arn:aws:execute-api:us-east-1:123456789012:abc123").ToStringOutput()
		addApiInvokePolicy(ctx.Pulumi(), "test-api", fn.Role(), apiArn)
	})

	if mocks.find("aws:iam/rolePolicy:RolePolicy") == nil {
		t.Error("IAM inline policy not created by addApiInvokePolicy")
	}
}

// ── buildContainerDefs test ───────────────────────────────────────────────────

func TestBuildContainerDefs_BasicShape(t *testing.T) {
	t.Parallel()
	runTest(t, func(ctx *forge.RunContext) {
		args := &ServiceArgs{
			Image:       "nginx:latest",
			Port:        8080,
			Environment: map[string]string{"MY_VAR": "my-value"},
		}
		out := buildContainerDefs(ctx, "MySvc", args, "/aws/ecs/MySvc", "us-east-1")

		// Verify the output resolves to valid JSON containing the container name.
		out.ApplyT(func(defs string) error {
			if !strings.Contains(defs, "myapp-test-MySvc") {
				t.Errorf("container defs missing qualified container name, got: %s", defs)
			}
			if !strings.Contains(defs, "nginx:latest") {
				t.Errorf("container defs missing image, got: %s", defs)
			}
			if !strings.Contains(defs, "MY_VAR") {
				t.Errorf("container defs missing environment variable, got: %s", defs)
			}
			if !strings.Contains(defs, "FORGE_STAGE") {
				t.Errorf("container defs missing FORGE_STAGE, got: %s", defs)
			}
			if !strings.Contains(defs, "8080") {
				t.Errorf("container defs missing port mapping, got: %s", defs)
			}
			return nil
		})
	})
}

func TestBuildContainerDefs_NoPort(t *testing.T) {
	t.Parallel()
	runTest(t, func(ctx *forge.RunContext) {
		args := &ServiceArgs{Image: "worker:latest"} // Port = 0
		out := buildContainerDefs(ctx, "Worker", args, "/aws/ecs/Worker", "us-east-1")

		out.ApplyT(func(defs string) error {
			// Port mappings array should be empty when Port == 0.
			if strings.Contains(defs, "containerPort") {
				t.Errorf("expected no port mapping when Port=0, got: %s", defs)
			}
			return nil
		})
	})
}
