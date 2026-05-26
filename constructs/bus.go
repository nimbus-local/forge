package constructs

import (
	"encoding/json"
	"fmt"

	forge "github.com/nimbus-local/forge"
	"github.com/pulumi/pulumi-aws/sdk/v7/go/aws/cloudwatch"
	"github.com/pulumi/pulumi-aws/sdk/v7/go/aws/iam"
	awslambda "github.com/pulumi/pulumi-aws/sdk/v7/go/aws/lambda"
	"github.com/pulumi/pulumi-aws/sdk/v7/go/aws/sqs"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

// Bus is an EventBridge custom event bus construct.
//
// LinkEnv keys injected into linked Functions:
//
//	SST_BUS_<NAME>_NAME — event bus name
//	SST_BUS_<NAME>_ARN  — event bus ARN
//
// Linked Functions automatically receive events:PutEvents on this bus.
type Bus struct {
	name string
	bus  *cloudwatch.EventBus
	ctx  *forge.RunContext
}

// BusArgs configures a Bus construct.
type BusArgs struct {
	// Rules define EventBridge rules that route matching events to Lambda or SQS targets.
	Rules []BusRule

	// Tags merged with stage-level tags on every resource.
	Tags map[string]string
}

// BusRule matches events on the bus and routes them to one or more targets.
type BusRule struct {
	// Name is the logical name for this rule (used as a resource name suffix).
	Name string

	// Pattern is the EventBridge event pattern JSON string.
	// Either Pattern or Schedule must be set; they cannot both be set.
	Pattern string

	// Schedule is a cron or rate expression (e.g. "rate(5 minutes)").
	// Either Pattern or Schedule must be set; they cannot both be set.
	Schedule string

	// Targets are Lambda functions invoked when the rule matches.
	Targets []*FunctionArgs

	// QueueTargets are SQS queues that receive matching events.
	// EventBridge sends the event JSON as the message body.
	QueueTargets []*Queue
}

// NewBus creates an EventBridge custom event bus with optional rules and targets.
func NewBus(ctx *forge.RunContext, name string, args *BusArgs) *Bus {
	if args == nil {
		args = &BusArgs{}
	}

	pctx := ctx.Pulumi()
	tags := mergedTags(defaultTags(ctx, name), args.Tags)

	// ── Event bus ─────────────────────────────────────────────────────────────
	bus, err := cloudwatch.NewEventBus(pctx, name, &cloudwatch.EventBusArgs{
		Name: pulumi.String(qualifiedName(ctx, name)),
		Tags: tags,
	})
	panicOnErr(err, name+": event bus")

	// ── Rules and targets ─────────────────────────────────────────────────────
	for _, rule := range args.Rules {
		if rule.Name == "" {
			panic(fmt.Sprintf("forge [%s]: BusRule.Name must not be empty", name))
		}
		if rule.Pattern == "" && rule.Schedule == "" {
			panic(fmt.Sprintf("forge [%s]: BusRule %q must set Pattern or Schedule", name, rule.Name))
		}

		ruleName := fmt.Sprintf("%s-%s", name, rule.Name)
		ruleArgs := &cloudwatch.EventRuleArgs{
			Name:         pulumi.String(qualifiedName(ctx, ruleName)),
			EventBusName: bus.Name,
			Tags:         tags,
		}
		if rule.Pattern != "" {
			ruleArgs.EventPattern = pulumi.String(rule.Pattern)
		}
		if rule.Schedule != "" {
			ruleArgs.ScheduleExpression = pulumi.String(rule.Schedule)
		}

		r, err := cloudwatch.NewEventRule(pctx, ruleName, ruleArgs)
		panicOnErr(err, ruleName+": event rule")

		// ── Lambda targets ─────────────────────────────────────────────────
		for i, fnArgs := range rule.Targets {
			targetName := fmt.Sprintf("%s-target-%d", ruleName, i)
			fn := NewFunction(ctx, targetName, fnArgs)

			_, err = awslambda.NewPermission(pctx, targetName+"-perm", &awslambda.PermissionArgs{
				Action:    pulumi.String("lambda:InvokeFunction"),
				Function:  fn.ARN(),
				Principal: pulumi.String("events.amazonaws.com"),
				SourceArn: r.Arn,
			})
			panicOnErr(err, targetName+": lambda permission")

			_, err = cloudwatch.NewEventTarget(pctx, targetName, &cloudwatch.EventTargetArgs{
				Rule:         r.Name,
				EventBusName: bus.Name,
				Arn:          fn.ARN(),
			})
			panicOnErr(err, targetName+": event target")
		}

		// ── SQS queue targets ──────────────────────────────────────────────
		for i, q := range rule.QueueTargets {
			targetName := fmt.Sprintf("%s-queue-target-%d", ruleName, i)

			// Allow EventBridge to publish to the queue via a resource-based policy.
			queuePolicy := pulumi.All(r.Arn, q.ARN()).ApplyT(func(vals []interface{}) (string, error) {
				ruleArn := vals[0].(string)
				queueArn := vals[1].(string)
				doc := map[string]interface{}{
					"Version": "2012-10-17",
					"Statement": []map[string]interface{}{{
						"Effect":    "Allow",
						"Principal": map[string]interface{}{"Service": "events.amazonaws.com"},
						"Action":    "sqs:SendMessage",
						"Resource":  queueArn,
						"Condition": map[string]interface{}{
							"ArnEquals": map[string]interface{}{"aws:SourceArn": ruleArn},
						},
					}},
				}
				b, err := json.Marshal(doc)
				return string(b), err
			}).(pulumi.StringOutput)

			_, err = sqs.NewQueuePolicy(pctx, targetName+"-policy", &sqs.QueuePolicyArgs{
				QueueUrl: q.URL(),
				Policy:   queuePolicy,
			})
			panicOnErr(err, targetName+": sqs queue policy")

			_, err = cloudwatch.NewEventTarget(pctx, targetName, &cloudwatch.EventTargetArgs{
				Rule:         r.Name,
				EventBusName: bus.Name,
				Arn:          q.ARN(),
			})
			panicOnErr(err, targetName+": sqs event target")
		}
	}

	return &Bus{name: name, bus: bus, ctx: ctx}
}

// ── Accessors ─────────────────────────────────────────────────────────────────

// BusName returns the event bus name as a Pulumi output.
func (b *Bus) BusName() pulumi.StringOutput { return b.bus.Name }

// ARN returns the event bus ARN as a Pulumi output.
func (b *Bus) ARN() pulumi.StringOutput { return b.bus.Arn }

// EventBus returns the underlying EventBus resource.
func (b *Bus) EventBus() *cloudwatch.EventBus { return b.bus }

// ── IAM grant ─────────────────────────────────────────────────────────────────

// Grant attaches an inline IAM policy giving the role events:PutEvents on this
// bus. Called automatically when a Function links this construct.
func (b *Bus) Grant(role *iam.Role) {
	pctx := b.ctx.Pulumi()
	policy := b.bus.Arn.ApplyT(func(arn string) (string, error) {
		doc := map[string]interface{}{
			"Version": "2012-10-17",
			"Statement": []map[string]interface{}{{
				"Effect":   "Allow",
				"Action":   "events:PutEvents",
				"Resource": arn,
			}},
		}
		byt, err := json.Marshal(doc)
		return string(byt), err
	}).(pulumi.StringOutput)

	_, err := iam.NewRolePolicy(pctx, b.name+"-bus-grant", &iam.RolePolicyArgs{
		Role:   role.Name,
		Policy: policy,
	})
	panicOnErr(err, b.name+": events:PutEvents grant")
}

// ── Linkable ──────────────────────────────────────────────────────────────────

// LinkEnv implements forge.Linkable.
func (b *Bus) LinkEnv() pulumi.StringMap {
	key := envKey(b.name)
	return pulumi.StringMap{
		"SST_BUS_" + key + "_NAME": b.bus.Name,
		"SST_BUS_" + key + "_ARN":  b.bus.Arn,
	}
}

// LinkName implements forge.Linkable.
func (b *Bus) LinkName() string { return b.name }
