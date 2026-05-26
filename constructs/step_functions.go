package constructs

import (
	"encoding/json"
	"fmt"

	forge "github.com/nimbus-local/forge"
	"github.com/pulumi/pulumi-aws/sdk/v7/go/aws/iam"
	"github.com/pulumi/pulumi-aws/sdk/v7/go/aws/sfn"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

// StepFunctions creates an AWS Step Functions state machine from a raw ASL JSON definition.
// It mirrors sst.aws.StepFunctions.
//
// The state machine receives an IAM execution role with lambda:InvokeFunction so Task
// states can call Lambda. Use Grant to give a Function permission to start executions.
type StepFunctions struct {
	name         string
	stateMachine *sfn.StateMachine
	role         *iam.Role
	ctx          *forge.RunContext
}

// StepFunctionsArgs configures the state machine.
type StepFunctionsArgs struct {
	// Definition is the Amazon States Language (ASL) JSON string. Required.
	Definition string
	// Type is the state machine type: "STANDARD" (default) or "EXPRESS".
	Type string
	// Tags are merged with the default forge tags.
	Tags map[string]string
}

// NewStepFunctions creates a Step Functions state machine construct.
// args.Definition must be a non-empty ASL JSON string.
func NewStepFunctions(ctx *forge.RunContext, name string, args *StepFunctionsArgs) *StepFunctions {
	if args == nil {
		args = &StepFunctionsArgs{}
	}
	if args.Definition == "" {
		panic(fmt.Sprintf("NewStepFunctions %q: Definition is required", name))
	}
	smType := args.Type
	if smType == "" {
		smType = "STANDARD"
	}
	if smType != "STANDARD" && smType != "EXPRESS" {
		panic(fmt.Sprintf("NewStepFunctions %q: Type must be STANDARD or EXPRESS, got %q", name, smType))
	}

	pctx := ctx.Pulumi()

	tags := mergedTags(defaultTags(ctx, name), args.Tags)

	role, err := iam.NewRole(pctx, name+"-role", &iam.RoleArgs{
		AssumeRolePolicy: pulumi.String(`{
			"Version": "2012-10-17",
			"Statement": [{
				"Effect": "Allow",
				"Principal": {"Service": "states.amazonaws.com"},
				"Action": "sts:AssumeRole"
			}]
		}`),
		Tags: tags,
	})
	panicOnErr(err, name+": iam role")

	// Allow the state machine to invoke Lambda functions (for Task states).
	_, err = iam.NewRolePolicy(pctx, name+"-lambda-invoke", &iam.RolePolicyArgs{
		Role: role.Name,
		Policy: pulumi.String(`{
			"Version": "2012-10-17",
			"Statement": [{
				"Effect": "Allow",
				"Action": "lambda:InvokeFunction",
				"Resource": "*"
			}]
		}`),
	})
	panicOnErr(err, name+": lambda invoke policy")

	sm, err := sfn.NewStateMachine(pctx, name, &sfn.StateMachineArgs{
		Name:       pulumi.String(qualifiedName(ctx, name)),
		Definition: pulumi.String(args.Definition),
		RoleArn:    role.Arn,
		Type:       pulumi.String(smType),
		Tags:       tags,
	})
	panicOnErr(err, name+": state machine")

	return &StepFunctions{name: name, stateMachine: sm, role: role, ctx: ctx}
}

// ARN returns the state machine ARN as a Pulumi output.
func (s *StepFunctions) ARN() pulumi.StringOutput { return s.stateMachine.Arn }

// Name returns the physical state machine name as a Pulumi output.
func (s *StepFunctions) Name() pulumi.StringOutput { return s.stateMachine.Name }

// Grant adds an IAM policy to role granting it states:StartExecution on this state
// machine, plus states:DescribeExecution and states:StopExecution on all executions.
func (s *StepFunctions) Grant(role *iam.Role) {
	pctx := s.ctx.Pulumi()
	policy := s.stateMachine.Arn.ApplyT(func(arn string) (string, error) {
		doc := map[string]interface{}{
			"Version": "2012-10-17",
			"Statement": []map[string]interface{}{
				{
					"Effect":   "Allow",
					"Action":   "states:StartExecution",
					"Resource": arn,
				},
				{
					"Effect":   "Allow",
					"Action":   []string{"states:DescribeExecution", "states:StopExecution"},
					"Resource": "*",
				},
			},
		}
		b, err := json.Marshal(doc)
		return string(b), err
	}).(pulumi.StringOutput)

	_, err := iam.NewRolePolicy(pctx, s.name+"-sfn-grant", &iam.RolePolicyArgs{
		Role:   role.Name,
		Policy: policy,
	})
	panicOnErr(err, s.name+": sfn grant policy")
}

// LinkEnv implements Linkable.
func (s *StepFunctions) LinkEnv() pulumi.StringMap {
	key := envKey(s.name)
	return pulumi.StringMap{
		fmt.Sprintf("SST_SFN_%s_ARN", key):  s.stateMachine.Arn,
		fmt.Sprintf("SST_SFN_%s_NAME", key): s.stateMachine.Name,
	}
}

// LinkName implements Linkable.
func (s *StepFunctions) LinkName() string { return s.name }
