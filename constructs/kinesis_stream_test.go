package constructs

import (
	"testing"

	forge "github.com/nimbus-local/forge"
)

func TestNewKinesisStream_StreamCreated(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		NewKinesisStream(ctx, "Events", nil)
	})

	if mocks.find("aws:kinesis/stream:Stream") == nil {
		t.Error("Kinesis stream not created")
	}
}

func TestNewKinesisStream_PhysicalNameQualified(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		NewKinesisStream(ctx, "Events", nil)
	})

	r := mocks.find("aws:kinesis/stream:Stream")
	if r == nil {
		t.Fatal("Kinesis stream not registered")
	}
	if r.inputs["name"].StringValue() != "myapp-test-Events" {
		t.Errorf("stream name = %q, want myapp-test-Events", r.inputs["name"].StringValue())
	}
}

func TestNewKinesisStream_TagsApplied(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		NewKinesisStream(ctx, "Events", nil)
	})

	r := mocks.find("aws:kinesis/stream:Stream")
	if r == nil {
		t.Fatal("Kinesis stream not registered")
	}
	for _, tag := range []string{"forge:app", "forge:stage", "forge:name"} {
		assertTag(t, r.inputs, tag)
	}
}

func TestNewKinesisStream_DefaultProvisionedMode(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		NewKinesisStream(ctx, "Events", nil)
	})

	r := mocks.find("aws:kinesis/stream:Stream")
	if r == nil {
		t.Fatal("Kinesis stream not registered")
	}
	mode := r.inputs["streamModeDetails"]
	if !mode.IsObject() {
		t.Fatal("streamModeDetails not set")
	}
	if mode.ObjectValue()["streamMode"].StringValue() != "PROVISIONED" {
		t.Errorf("streamMode = %q, want PROVISIONED", mode.ObjectValue()["streamMode"].StringValue())
	}
	if r.inputs["shardCount"].NumberValue() != 1 {
		t.Errorf("shardCount = %v, want 1", r.inputs["shardCount"].NumberValue())
	}
}

func TestNewKinesisStream_OnDemandMode(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		NewKinesisStream(ctx, "Events", &KinesisStreamArgs{OnDemand: true})
	})

	r := mocks.find("aws:kinesis/stream:Stream")
	if r == nil {
		t.Fatal("Kinesis stream not registered")
	}
	mode := r.inputs["streamModeDetails"]
	if !mode.IsObject() {
		t.Fatal("streamModeDetails not set")
	}
	if mode.ObjectValue()["streamMode"].StringValue() != "ON_DEMAND" {
		t.Errorf("streamMode = %q, want ON_DEMAND", mode.ObjectValue()["streamMode"].StringValue())
	}
}

func TestNewKinesisStream_CustomShardCount(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		NewKinesisStream(ctx, "Events", &KinesisStreamArgs{ShardCount: 4})
	})

	r := mocks.find("aws:kinesis/stream:Stream")
	if r == nil {
		t.Fatal("Kinesis stream not registered")
	}
	if r.inputs["shardCount"].NumberValue() != 4 {
		t.Errorf("shardCount = %v, want 4", r.inputs["shardCount"].NumberValue())
	}
}

func TestNewKinesisStream_DefaultRetentionHours(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		NewKinesisStream(ctx, "Events", nil)
	})

	r := mocks.find("aws:kinesis/stream:Stream")
	if r == nil {
		t.Fatal("Kinesis stream not registered")
	}
	if r.inputs["retentionPeriod"].NumberValue() != 24 {
		t.Errorf("retentionPeriod = %v, want 24", r.inputs["retentionPeriod"].NumberValue())
	}
}

func TestNewKinesisStream_CustomRetentionHours(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		NewKinesisStream(ctx, "Events", &KinesisStreamArgs{RetentionHours: 168})
	})

	r := mocks.find("aws:kinesis/stream:Stream")
	if r == nil {
		t.Fatal("Kinesis stream not registered")
	}
	if r.inputs["retentionPeriod"].NumberValue() != 168 {
		t.Errorf("retentionPeriod = %v, want 168", r.inputs["retentionPeriod"].NumberValue())
	}
}

func TestNewKinesisStream_ConsumerLambdaCreated(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		NewKinesisStream(ctx, "Events", &KinesisStreamArgs{
			Consumers: []*FunctionArgs{{Handler: "bootstrap"}},
		})
	})

	if mocks.find("aws:lambda/function:Function") == nil {
		t.Error("consumer Lambda not created")
	}
	if mocks.find("aws:lambda/eventSourceMapping:EventSourceMapping") == nil {
		t.Error("event source mapping not created for consumer")
	}
}

func TestNewKinesisStream_MultipleConsumers(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		NewKinesisStream(ctx, "Events", &KinesisStreamArgs{
			Consumers: []*FunctionArgs{
				{Handler: "bootstrap"},
				{Handler: "bootstrap"},
			},
		})
	})

	lambdas := mocks.findAll("aws:lambda/function:Function")
	if len(lambdas) != 2 {
		t.Errorf("expected 2 consumer Lambdas, got %d", len(lambdas))
	}
	esms := mocks.findAll("aws:lambda/eventSourceMapping:EventSourceMapping")
	if len(esms) != 2 {
		t.Errorf("expected 2 event source mappings, got %d", len(esms))
	}
}

func TestNewKinesisStream_NoConsumerByDefault(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		NewKinesisStream(ctx, "Events", nil)
	})

	if mocks.find("aws:lambda/function:Function") != nil {
		t.Error("Lambda should not be created when no consumers specified")
	}
	if mocks.find("aws:lambda/eventSourceMapping:EventSourceMapping") != nil {
		t.Error("ESM should not be created when no consumers specified")
	}
}

func TestNewKinesisStream_LinkEnvKeys(t *testing.T) {
	t.Parallel()
	runTest(t, func(ctx *forge.RunContext) {
		s := NewKinesisStream(ctx, "Events", nil)
		env := s.LinkEnv()
		if _, ok := env["SST_KINESIS_EVENTS_STREAM_NAME"]; !ok {
			t.Error("LinkEnv missing SST_KINESIS_EVENTS_STREAM_NAME")
		}
		if _, ok := env["SST_KINESIS_EVENTS_STREAM_ARN"]; !ok {
			t.Error("LinkEnv missing SST_KINESIS_EVENTS_STREAM_ARN")
		}
		if len(env) != 2 {
			t.Errorf("LinkEnv has %d keys, want 2", len(env))
		}
	})
}

func TestNewKinesisStream_CamelCaseLinkEnvKey(t *testing.T) {
	t.Parallel()
	runTest(t, func(ctx *forge.RunContext) {
		s := NewKinesisStream(ctx, "UserEvents", nil)
		env := s.LinkEnv()
		if _, ok := env["SST_KINESIS_USER_EVENTS_STREAM_NAME"]; !ok {
			t.Error("LinkEnv missing SST_KINESIS_USER_EVENTS_STREAM_NAME")
		}
	})
}

func TestNewKinesisStream_LinkName(t *testing.T) {
	t.Parallel()
	runTest(t, func(ctx *forge.RunContext) {
		s := NewKinesisStream(ctx, "Events", nil)
		if s.LinkName() != "Events" {
			t.Errorf("LinkName = %q, want Events", s.LinkName())
		}
	})
}

func TestNewKinesisStream_ImplementsLinkable(t *testing.T) {
	t.Parallel()
	runTest(t, func(ctx *forge.RunContext) {
		s := NewKinesisStream(ctx, "Events", nil)
		var _ forge.Linkable = s
	})
}

func TestNewKinesisStream_NilArgsSafe(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		NewKinesisStream(ctx, "Events", nil)
	})

	if mocks.find("aws:kinesis/stream:Stream") == nil {
		t.Error("Kinesis stream not created with nil args")
	}
}

func TestNewKinesisStream_ConsumerIAMPolicyCreated(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		NewKinesisStream(ctx, "Events", &KinesisStreamArgs{
			Consumers: []*FunctionArgs{{Handler: "bootstrap"}},
		})
	})

	if mocks.find("aws:iam/rolePolicy:RolePolicy") == nil {
		t.Error("IAM role policy not created for consumer")
	}
}
