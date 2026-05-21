package constructs

import (
	"fmt"

	forge "github.com/sst-go/forge"
	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/iam"
	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/scheduler"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

// CronArgs configures a scheduled Lambda invocation via EventBridge Scheduler.
type CronArgs struct {
	// Schedule is either a rate expression ("rate(5 minutes)") or a cron expression ("cron(0 12 * * ? *)").
	Schedule string
	// Job defines the Lambda function to invoke on the schedule.
	Job *FunctionArgs
	// Enabled controls whether the schedule is active. Nil or true means enabled; set to false to disable.
	Enabled *bool
}

// Cron creates an EventBridge Scheduler schedule that invokes a Lambda function.
type Cron struct {
	name     string
	fn       *Function
	schedule *scheduler.Schedule
	ctx      *forge.RunContext
}

// NewCron creates a Cron construct backed by EventBridge Scheduler.
func NewCron(ctx *forge.RunContext, name string, args *CronArgs) *Cron {
	if args == nil {
		args = &CronArgs{}
	}
	if args.Schedule == "" {
		panic("forge: CronArgs.Schedule must not be empty for " + name)
	}
	if args.Job == nil {
		args.Job = &FunctionArgs{}
	}

	pctx := ctx.Pulumi()

	fn := NewFunction(ctx, name, args.Job)

	// IAM role that allows EventBridge Scheduler to invoke the Lambda.
	role, err := iam.NewRole(pctx, name+"-scheduler-role", &iam.RoleArgs{
		AssumeRolePolicy: pulumi.String(`{
			"Version": "2012-10-17",
			"Statement": [{
				"Effect": "Allow",
				"Principal": { "Service": "scheduler.amazonaws.com" },
				"Action": "sts:AssumeRole"
			}]
		}`),
		Tags: defaultTags(ctx, name),
	})
	panicOnErr(err, name+": scheduler role")

	_, err = iam.NewRolePolicy(pctx, name+"-scheduler-policy", &iam.RolePolicyArgs{
		Role: role.Name,
		Policy: fn.ARN().ApplyT(func(arn string) (string, error) {
			return fmt.Sprintf(`{
				"Version": "2012-10-17",
				"Statement": [{
					"Effect": "Allow",
					"Action": "lambda:InvokeFunction",
					"Resource": "%s"
				}]
			}`, arn), nil
		}).(pulumi.StringOutput),
	})
	panicOnErr(err, name+": scheduler policy")

	state := "ENABLED"
	if args.Enabled != nil && !*args.Enabled {
		state = "DISABLED"
	}

	sched, err := scheduler.NewSchedule(pctx, name+"-schedule", &scheduler.ScheduleArgs{
		Name:               pulumi.String(qualifiedName(ctx, name)),
		ScheduleExpression: pulumi.String(args.Schedule),
		State:              pulumi.String(state),
		Target: &scheduler.ScheduleTargetArgs{
			Arn:     fn.ARN(),
			RoleArn: role.Arn,
		},
		FlexibleTimeWindow: &scheduler.ScheduleFlexibleTimeWindowArgs{
			Mode: pulumi.String("OFF"),
		},
	})
	panicOnErr(err, name+": eventbridge schedule")

	return &Cron{name: name, fn: fn, schedule: sched, ctx: ctx}
}

// FunctionARN returns the underlying Lambda function ARN.
func (c *Cron) FunctionARN() pulumi.StringOutput { return c.fn.ARN() }

// ScheduleArn returns the EventBridge Scheduler schedule ARN.
func (c *Cron) ScheduleArn() pulumi.StringOutput { return c.schedule.Arn }
