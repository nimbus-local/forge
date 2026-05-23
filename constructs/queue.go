package constructs

import (
	"fmt"

	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/iam"
	awslambda "github.com/pulumi/pulumi-aws/sdk/v6/go/aws/lambda"
	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/sqs"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	forge "github.com/nimbus-local/forge"
)

// QueueArgs configures an SQS queue construct.
type QueueArgs struct {
	// Consumer is an optional Lambda function that processes messages from the queue.
	Consumer *FunctionArgs
	// Fifo creates a FIFO queue (.fifo suffix is appended automatically).
	Fifo bool
	// VisibilityTimeout in seconds. Defaults to 30.
	VisibilityTimeout int
	// BatchSize for the Lambda event source mapping. Defaults to 10.
	BatchSize int
	// DeadLetterQueue creates a sibling DLQ with a max receive count of 3.
	DeadLetterQueue bool
}

// Queue is an SQS queue construct.
type Queue struct {
	name     string
	resource *sqs.Queue
	dlq      *sqs.Queue
	ctx      *forge.RunContext
}

// NewQueue creates an SQS queue construct, optionally with a Lambda consumer and DLQ.
func NewQueue(ctx *forge.RunContext, name string, args *QueueArgs) *Queue {
	if args == nil {
		args = &QueueArgs{}
	}
	if args.VisibilityTimeout == 0 {
		args.VisibilityTimeout = 30
	}
	if args.BatchSize == 0 {
		args.BatchSize = 10
	}

	pctx := ctx.Pulumi()

	// ── Dead-letter queue ─────────────────────────────────────────────────────
	var dlq *sqs.Queue
	if args.DeadLetterQueue {
		dlqName := qualifiedName(ctx, name+"-dlq")
		if args.Fifo {
			dlqName += ".fifo"
		}
		var err error
		dlq, err = sqs.NewQueue(pctx, name+"-dlq", &sqs.QueueArgs{
			Name:                    pulumi.String(dlqName),
			FifoQueue:               pulumi.Bool(args.Fifo),
			MessageRetentionSeconds: pulumi.Int(1209600), // 14 days
			Tags:                    defaultTags(ctx, name+"-dlq"),
		})
		panicOnErr(err, name+": dlq")
	}

	// ── Main queue ────────────────────────────────────────────────────────────
	queueName := qualifiedName(ctx, name)
	if args.Fifo {
		queueName += ".fifo"
	}

	queueArgs := &sqs.QueueArgs{
		Name:                     pulumi.String(queueName),
		FifoQueue:                pulumi.Bool(args.Fifo),
		VisibilityTimeoutSeconds: pulumi.Int(args.VisibilityTimeout),
		Tags:                     defaultTags(ctx, name),
	}

	if dlq != nil {
		queueArgs.RedrivePolicy = dlq.Arn.ApplyT(func(dlqArn string) (string, error) {
			return fmt.Sprintf(`{"deadLetterTargetArn":"%s","maxReceiveCount":3}`, dlqArn), nil
		}).(pulumi.StringOutput)
	}

	queue, err := sqs.NewQueue(pctx, name, queueArgs)
	panicOnErr(err, name+": sqs queue")

	// ── Lambda consumer ───────────────────────────────────────────────────────
	if args.Consumer != nil {
		fn := NewFunction(ctx, name+"-consumer", args.Consumer)

		// Attach SQS consume permissions to the Lambda execution role.
		_, err = iam.NewRolePolicy(pctx, name+"-sqs-policy", &iam.RolePolicyArgs{
			Role: fn.Role().Name,
			Policy: queue.Arn.ApplyT(func(arn string) (string, error) {
				return fmt.Sprintf(`{
					"Version": "2012-10-17",
					"Statement": [{
						"Effect": "Allow",
						"Action": [
							"sqs:ReceiveMessage",
							"sqs:DeleteMessage",
							"sqs:GetQueueAttributes"
						],
						"Resource": "%s"
					}]
				}`, arn), nil
			}).(pulumi.StringOutput),
		})
		panicOnErr(err, name+": sqs consume policy")

		// Event source mapping to trigger the Lambda from the queue.
		_, err = awslambda.NewEventSourceMapping(pctx, name+"-esm", &awslambda.EventSourceMappingArgs{
			EventSourceArn:        queue.Arn,
			FunctionName:          fn.ARN(),
			BatchSize:             pulumi.Int(args.BatchSize),
			FunctionResponseTypes: pulumi.StringArray{pulumi.String("ReportBatchItemFailures")},
		})
		panicOnErr(err, name+": event source mapping")
	}

	return &Queue{name: name, resource: queue, dlq: dlq, ctx: ctx}
}

// URL returns the queue URL as a Pulumi output.
func (q *Queue) URL() pulumi.StringOutput { return q.resource.Url }

// ARN returns the queue ARN as a Pulumi output.
func (q *Queue) ARN() pulumi.StringOutput { return q.resource.Arn }

// DLQURL returns the dead-letter queue URL, or an empty string if no DLQ was created.
func (q *Queue) DLQURL() pulumi.StringOutput {
	if q.dlq == nil {
		return pulumi.String("").ToStringOutput()
	}
	return q.dlq.Url
}

// LinkEnv implements Linkable — injects the queue URL and ARN into linked Lambdas.
func (q *Queue) LinkEnv() pulumi.StringMap {
	key := envKey(q.name)
	return pulumi.StringMap{
		fmt.Sprintf("SST_QUEUE_%s_URL", key): q.resource.Url,
		fmt.Sprintf("SST_QUEUE_%s_ARN", key): q.resource.Arn,
	}
}

// LinkName implements Linkable.
func (q *Queue) LinkName() string { return q.name }
