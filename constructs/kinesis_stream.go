package constructs

import (
	"encoding/json"
	"fmt"

	forge "github.com/nimbus-local/forge"
	"github.com/pulumi/pulumi-aws/sdk/v7/go/aws/iam"
	"github.com/pulumi/pulumi-aws/sdk/v7/go/aws/kinesis"
	awslambda "github.com/pulumi/pulumi-aws/sdk/v7/go/aws/lambda"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

// KinesisStream is a Kinesis Data Stream construct.
//
// LinkEnv keys injected into linked Functions:
//
//	SST_KINESIS_<NAME>_STREAM_NAME — stream name
//	SST_KINESIS_<NAME>_STREAM_ARN  — stream ARN
//
// Linked Functions automatically receive kinesis:Get*, kinesis:List*, kinesis:Describe*,
// and kinesis:PutRecord* permissions on the stream.
type KinesisStream struct {
	name   string
	stream *kinesis.Stream
	ctx    *forge.RunContext
}

// KinesisStreamArgs configures a KinesisStream construct.
type KinesisStreamArgs struct {
	// ShardCount is the number of shards for PROVISIONED mode. Defaults to 1.
	// Ignored when OnDemand is true.
	ShardCount int

	// OnDemand switches the stream to ON_DEMAND capacity mode (no shard management).
	// When true, ShardCount is ignored.
	OnDemand bool

	// RetentionHours is the data retention period in hours. Must be between 24 and 8760.
	// Defaults to 24.
	RetentionHours int

	// Consumers are Lambda functions triggered by this stream.
	Consumers []*FunctionArgs

	// BatchSize for the Lambda event source mappings. Defaults to 100.
	BatchSize int

	// BisectOnError splits a failing batch in two and retries each half independently,
	// reducing the impact of a single poison-pill record.
	BisectOnError bool

	// Tags merged with stage-level tags on every resource.
	Tags map[string]string
}

// NewKinesisStream creates a Kinesis Data Stream with optional Lambda consumers.
func NewKinesisStream(ctx *forge.RunContext, name string, args *KinesisStreamArgs) *KinesisStream {
	if args == nil {
		args = &KinesisStreamArgs{}
	}
	if args.BatchSize == 0 {
		args.BatchSize = 100
	}
	if args.RetentionHours == 0 {
		args.RetentionHours = 24
	}

	pctx := ctx.Pulumi()
	tags := mergedTags(defaultTags(ctx, name), args.Tags)

	// ── Stream ────────────────────────────────────────────────────────────────
	streamArgs := &kinesis.StreamArgs{
		Name:            pulumi.String(qualifiedName(ctx, name)),
		RetentionPeriod: pulumi.Int(args.RetentionHours),
		Tags:            tags,
	}

	if args.OnDemand {
		streamArgs.StreamModeDetails = &kinesis.StreamStreamModeDetailsArgs{
			StreamMode: pulumi.String("ON_DEMAND"),
		}
	} else {
		shards := args.ShardCount
		if shards == 0 {
			shards = 1
		}
		streamArgs.ShardCount = pulumi.Int(shards)
		streamArgs.StreamModeDetails = &kinesis.StreamStreamModeDetailsArgs{
			StreamMode: pulumi.String("PROVISIONED"),
		}
	}

	stream, err := kinesis.NewStream(pctx, name, streamArgs)
	panicOnErr(err, name+": kinesis stream")

	// ── Lambda consumers ──────────────────────────────────────────────────────
	for i, consumerArgs := range args.Consumers {
		consumerName := fmt.Sprintf("%s-consumer-%d", name, i)
		fn := NewFunction(ctx, consumerName, consumerArgs)

		policy := stream.Arn.ApplyT(func(arn string) (string, error) {
			doc := map[string]interface{}{
				"Version": "2012-10-17",
				"Statement": []map[string]interface{}{{
					"Effect": "Allow",
					"Action": []string{
						"kinesis:GetRecords",
						"kinesis:GetShardIterator",
						"kinesis:ListShards",
						"kinesis:DescribeStream",
						"kinesis:DescribeStreamSummary",
					},
					"Resource": arn,
				}},
			}
			b, err := json.Marshal(doc)
			return string(b), err
		}).(pulumi.StringOutput)

		_, err = iam.NewRolePolicy(pctx, consumerName+"-kinesis-policy", &iam.RolePolicyArgs{
			Role:   fn.Role().Name,
			Policy: policy,
		})
		panicOnErr(err, consumerName+": kinesis consume policy")

		esmArgs := &awslambda.EventSourceMappingArgs{
			EventSourceArn:             stream.Arn,
			FunctionName:               fn.ARN(),
			StartingPosition:           pulumi.String("LATEST"),
			BatchSize:                  pulumi.Int(args.BatchSize),
			BisectBatchOnFunctionError: pulumi.Bool(args.BisectOnError),
			FunctionResponseTypes:      pulumi.StringArray{pulumi.String("ReportBatchItemFailures")},
		}

		_, err = awslambda.NewEventSourceMapping(pctx, consumerName+"-esm", esmArgs)
		panicOnErr(err, consumerName+": event source mapping")
	}

	return &KinesisStream{name: name, stream: stream, ctx: ctx}
}

// ── Accessors ─────────────────────────────────────────────────────────────────

// Name returns the stream name as a Pulumi output.
func (k *KinesisStream) Name() pulumi.StringOutput { return k.stream.Name }

// ARN returns the stream ARN as a Pulumi output.
func (k *KinesisStream) ARN() pulumi.StringOutput { return k.stream.Arn }

// Stream returns the underlying Kinesis stream resource.
func (k *KinesisStream) Stream() *kinesis.Stream { return k.stream }

// ── IAM grant ─────────────────────────────────────────────────────────────────

// Grant attaches an inline IAM policy giving the role kinesis:PutRecord* and read
// permissions on this stream. Called automatically when a Function links this construct.
func (k *KinesisStream) Grant(role *iam.Role) {
	pctx := k.ctx.Pulumi()
	policy := k.stream.Arn.ApplyT(func(arn string) (string, error) {
		doc := map[string]interface{}{
			"Version": "2012-10-17",
			"Statement": []map[string]interface{}{{
				"Effect": "Allow",
				"Action": []string{
					"kinesis:PutRecord",
					"kinesis:PutRecords",
					"kinesis:GetRecords",
					"kinesis:GetShardIterator",
					"kinesis:ListShards",
					"kinesis:DescribeStream",
					"kinesis:DescribeStreamSummary",
				},
				"Resource": arn,
			}},
		}
		b, err := json.Marshal(doc)
		return string(b), err
	}).(pulumi.StringOutput)

	_, err := iam.NewRolePolicy(pctx, k.name+"-kinesis-grant", &iam.RolePolicyArgs{
		Role:   role.Name,
		Policy: policy,
	})
	panicOnErr(err, k.name+": kinesis grant")
}

// ── Linkable ──────────────────────────────────────────────────────────────────

// LinkEnv implements forge.Linkable.
func (k *KinesisStream) LinkEnv() pulumi.StringMap {
	key := envKey(k.name)
	return pulumi.StringMap{
		"SST_KINESIS_" + key + "_STREAM_NAME": k.stream.Name,
		"SST_KINESIS_" + key + "_STREAM_ARN":  k.stream.Arn,
	}
}

// LinkName implements forge.Linkable.
func (k *KinesisStream) LinkName() string { return k.name }
