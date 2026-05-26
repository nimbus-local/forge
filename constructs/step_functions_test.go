package constructs

import (
	"strings"
	"testing"

	forge "github.com/nimbus-local/forge"
)

const minimalASL = `{
	"StartAt": "Done",
	"States": {
		"Done": {"Type": "Succeed"}
	}
}`

func TestNewStepFunctions_StateMachineCreated(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		NewStepFunctions(ctx, "Workflow", &StepFunctionsArgs{Definition: minimalASL})
	})

	if mocks.find("aws:sfn/stateMachine:StateMachine") == nil {
		t.Error("StateMachine not created")
	}
}

func TestNewStepFunctions_PhysicalNameQualified(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		NewStepFunctions(ctx, "Workflow", &StepFunctionsArgs{Definition: minimalASL})
	})

	r := mocks.find("aws:sfn/stateMachine:StateMachine")
	if r == nil {
		t.Fatal("StateMachine not registered")
	}
	if r.inputs["name"].StringValue() != "myapp-test-Workflow" {
		t.Errorf("name = %q, want myapp-test-Workflow", r.inputs["name"].StringValue())
	}
}

func TestNewStepFunctions_DefaultTypeIsStandard(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		NewStepFunctions(ctx, "Workflow", &StepFunctionsArgs{Definition: minimalASL})
	})

	r := mocks.find("aws:sfn/stateMachine:StateMachine")
	if r == nil {
		t.Fatal("StateMachine not registered")
	}
	if r.inputs["type"].StringValue() != "STANDARD" {
		t.Errorf("type = %q, want STANDARD", r.inputs["type"].StringValue())
	}
}

func TestNewStepFunctions_ExpressType(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		NewStepFunctions(ctx, "Workflow", &StepFunctionsArgs{
			Definition: minimalASL,
			Type:       "EXPRESS",
		})
	})

	r := mocks.find("aws:sfn/stateMachine:StateMachine")
	if r == nil {
		t.Fatal("StateMachine not registered")
	}
	if r.inputs["type"].StringValue() != "EXPRESS" {
		t.Errorf("type = %q, want EXPRESS", r.inputs["type"].StringValue())
	}
}

func TestNewStepFunctions_TagsApplied(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		NewStepFunctions(ctx, "Workflow", &StepFunctionsArgs{Definition: minimalASL})
	})

	r := mocks.find("aws:sfn/stateMachine:StateMachine")
	if r == nil {
		t.Fatal("StateMachine not registered")
	}
	for _, tag := range []string{"forge:app", "forge:stage", "forge:name"} {
		assertTag(t, r.inputs, tag)
	}
}

func TestNewStepFunctions_IAMRoleCreated(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		NewStepFunctions(ctx, "Workflow", &StepFunctionsArgs{Definition: minimalASL})
	})

	if mocks.find("aws:iam/role:Role") == nil {
		t.Error("IAM role not created")
	}
}

func TestNewStepFunctions_RoleTrustsPrincipalStates(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		NewStepFunctions(ctx, "Workflow", &StepFunctionsArgs{Definition: minimalASL})
	})

	r := mocks.find("aws:iam/role:Role")
	if r == nil {
		t.Fatal("IAM role not registered")
	}
	policy := r.inputs["assumeRolePolicy"].StringValue()
	if policy == "" {
		t.Fatal("assumeRolePolicy is empty")
	}
	if !strings.Contains(policy, "states.amazonaws.com") {
		t.Errorf("trust policy does not reference states.amazonaws.com: %s", policy)
	}
}

func TestNewStepFunctions_LambdaInvokePolicyCreated(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		NewStepFunctions(ctx, "Workflow", &StepFunctionsArgs{Definition: minimalASL})
	})

	if mocks.find("aws:iam/rolePolicy:RolePolicy") == nil {
		t.Error("lambda invoke role policy not created")
	}
}

func TestNewStepFunctions_DefinitionPassedThrough(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		NewStepFunctions(ctx, "Workflow", &StepFunctionsArgs{Definition: minimalASL})
	})

	r := mocks.find("aws:sfn/stateMachine:StateMachine")
	if r == nil {
		t.Fatal("StateMachine not registered")
	}
	if r.inputs["definition"].StringValue() != minimalASL {
		t.Errorf("definition = %q, want %q", r.inputs["definition"].StringValue(), minimalASL)
	}
}

func TestNewStepFunctions_EmptyDefinitionPanics(t *testing.T) {
	t.Parallel()
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for empty Definition")
		}
	}()
	runTest(t, func(ctx *forge.RunContext) {
		NewStepFunctions(ctx, "Workflow", &StepFunctionsArgs{})
	})
}

func TestNewStepFunctions_NilArgsPanics(t *testing.T) {
	t.Parallel()
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for nil args (empty Definition)")
		}
	}()
	runTest(t, func(ctx *forge.RunContext) {
		NewStepFunctions(ctx, "Workflow", nil)
	})
}

func TestNewStepFunctions_InvalidTypePanics(t *testing.T) {
	t.Parallel()
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for invalid Type")
		}
	}()
	runTest(t, func(ctx *forge.RunContext) {
		NewStepFunctions(ctx, "Workflow", &StepFunctionsArgs{
			Definition: minimalASL,
			Type:       "INVALID",
		})
	})
}

func TestNewStepFunctions_GrantCreatesPolicy(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		sf := NewStepFunctions(ctx, "Workflow", &StepFunctionsArgs{Definition: minimalASL})
		fn := NewFunction(ctx, "Caller", &FunctionArgs{Handler: "bootstrap"})
		sf.Grant(fn.Role())
	})

	policies := mocks.findAll("aws:iam/rolePolicy:RolePolicy")
	// Expect at least 2: the lambda-invoke policy on the SM role + the grant policy on the caller role.
	if len(policies) < 2 {
		t.Errorf("expected at least 2 role policies, got %d", len(policies))
	}
}

func TestNewStepFunctions_LinkEnvKeys(t *testing.T) {
	t.Parallel()
	runTest(t, func(ctx *forge.RunContext) {
		sf := NewStepFunctions(ctx, "Workflow", &StepFunctionsArgs{Definition: minimalASL})
		env := sf.LinkEnv()
		if _, ok := env["SST_SFN_WORKFLOW_ARN"]; !ok {
			t.Error("LinkEnv missing SST_SFN_WORKFLOW_ARN")
		}
		if _, ok := env["SST_SFN_WORKFLOW_NAME"]; !ok {
			t.Error("LinkEnv missing SST_SFN_WORKFLOW_NAME")
		}
		if len(env) != 2 {
			t.Errorf("LinkEnv has %d keys, want 2", len(env))
		}
	})
}

func TestNewStepFunctions_CamelCaseLinkEnvKey(t *testing.T) {
	t.Parallel()
	runTest(t, func(ctx *forge.RunContext) {
		sf := NewStepFunctions(ctx, "OrderWorkflow", &StepFunctionsArgs{Definition: minimalASL})
		env := sf.LinkEnv()
		if _, ok := env["SST_SFN_ORDER_WORKFLOW_ARN"]; !ok {
			t.Error("LinkEnv missing SST_SFN_ORDER_WORKFLOW_ARN")
		}
	})
}

func TestNewStepFunctions_LinkName(t *testing.T) {
	t.Parallel()
	runTest(t, func(ctx *forge.RunContext) {
		sf := NewStepFunctions(ctx, "Workflow", &StepFunctionsArgs{Definition: minimalASL})
		if sf.LinkName() != "Workflow" {
			t.Errorf("LinkName = %q, want Workflow", sf.LinkName())
		}
	})
}

func TestNewStepFunctions_ImplementsLinkable(t *testing.T) {
	t.Parallel()
	runTest(t, func(ctx *forge.RunContext) {
		sf := NewStepFunctions(ctx, "Workflow", &StepFunctionsArgs{Definition: minimalASL})
		var _ forge.Linkable = sf
	})
}

func TestNewStepFunctions_CustomTags(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		NewStepFunctions(ctx, "Workflow", &StepFunctionsArgs{
			Definition: minimalASL,
			Tags:       map[string]string{"team": "platform"},
		})
	})

	r := mocks.find("aws:sfn/stateMachine:StateMachine")
	if r == nil {
		t.Fatal("StateMachine not registered")
	}
	assertTag(t, r.inputs, "team")
}
