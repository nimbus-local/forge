package constructs

import (
	"fmt"

	awslambda "github.com/pulumi/pulumi-aws/sdk/v6/go/aws/lambda"
	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/sns"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	forge "github.com/sst-go/forge"
)

// TopicArgs configures an SNS topic construct.
type TopicArgs struct {
	// Subscribers are Lambda functions that receive every message published to this topic.
	Subscribers []*FunctionArgs
	// FIFO creates a FIFO topic with content-based deduplication enabled.
	FIFO bool
}

// Topic is an SNS topic construct.
type Topic struct {
	name     string
	resource *sns.Topic
	ctx      *forge.RunContext
}

// NewTopic creates an SNS topic construct and subscribes any provided Lambda functions.
func NewTopic(ctx *forge.RunContext, name string, args *TopicArgs) *Topic {
	if args == nil {
		args = &TopicArgs{}
	}

	pctx := ctx.Pulumi()

	topicName := qualifiedName(ctx, name)
	topicArgs := &sns.TopicArgs{
		Name: pulumi.String(topicName),
		Tags: defaultTags(ctx, name),
	}
	if args.FIFO {
		topicArgs.Name = pulumi.String(topicName + ".fifo")
		topicArgs.FifoTopic = pulumi.Bool(true)
		topicArgs.ContentBasedDeduplication = pulumi.Bool(true)
	}

	topic, err := sns.NewTopic(pctx, name, topicArgs)
	panicOnErr(err, name+": sns topic")

	// ── Lambda subscribers ────────────────────────────────────────────────────
	for i, subArgs := range args.Subscribers {
		subName := fmt.Sprintf("%s-sub-%d", name, i)
		fn := NewFunction(ctx, subName, subArgs)

		// Allow SNS to invoke this Lambda.
		_, err = awslambda.NewPermission(pctx, subName+"-perm", &awslambda.PermissionArgs{
			Action:    pulumi.String("lambda:InvokeFunction"),
			Function:  fn.ARN(),
			Principal: pulumi.String("sns.amazonaws.com"),
			SourceArn: topic.Arn,
		})
		panicOnErr(err, subName+": sns invoke permission")

		_, err = sns.NewTopicSubscription(pctx, subName+"-sub", &sns.TopicSubscriptionArgs{
			Topic:    topic.Arn,
			Protocol: pulumi.String("lambda"),
			Endpoint: fn.ARN(),
		})
		panicOnErr(err, subName+": topic subscription")
	}

	return &Topic{name: name, resource: topic, ctx: ctx}
}

// ARN returns the topic ARN as a Pulumi output.
func (t *Topic) ARN() pulumi.StringOutput { return t.resource.Arn }

// LinkEnv implements Linkable — injects the topic ARN into linked Lambdas.
func (t *Topic) LinkEnv() pulumi.StringMap {
	key := envKey(t.name)
	return pulumi.StringMap{
		fmt.Sprintf("SST_TOPIC_%s_ARN", key): t.resource.Arn,
	}
}

// LinkName implements Linkable.
func (t *Topic) LinkName() string { return t.name }
