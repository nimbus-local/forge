package constructs

import (
	"strings"
	"testing"

	forge "github.com/nimbus-local/forge"
)

func TestNewBus_EventBusCreated(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		NewBus(ctx, "Events", nil)
	})

	if mocks.find("aws:cloudwatch/eventBus:EventBus") == nil {
		t.Error("EventBus not created")
	}
}

func TestNewBus_PhysicalNameQualified(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		NewBus(ctx, "Events", nil)
	})

	r := mocks.find("aws:cloudwatch/eventBus:EventBus")
	if r == nil {
		t.Fatal("EventBus not registered")
	}
	if r.inputs["name"].StringValue() != "myapp-test-Events" {
		t.Errorf("bus name = %q, want myapp-test-Events", r.inputs["name"].StringValue())
	}
}

func TestNewBus_TagsApplied(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		NewBus(ctx, "Events", nil)
	})

	r := mocks.find("aws:cloudwatch/eventBus:EventBus")
	if r == nil {
		t.Fatal("EventBus not registered")
	}
	for _, tag := range []string{"forge:app", "forge:stage", "forge:name"} {
		assertTag(t, r.inputs, tag)
	}
}

func TestNewBus_NilArgsSafe(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		NewBus(ctx, "Events", nil)
	})

	if mocks.find("aws:cloudwatch/eventBus:EventBus") == nil {
		t.Error("EventBus not created with nil args")
	}
}

func TestNewBus_NoRulesByDefault(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		NewBus(ctx, "Events", nil)
	})

	if mocks.find("aws:cloudwatch/eventRule:EventRule") != nil {
		t.Error("EventRule should not be created with no rules")
	}
}

func TestNewBus_RuleCreatedWithPattern(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		NewBus(ctx, "Events", &BusArgs{
			Rules: []BusRule{
				{
					Name:    "orders",
					Pattern: `{"source":["myapp.orders"]}`,
				},
			},
		})
	})

	if mocks.find("aws:cloudwatch/eventRule:EventRule") == nil {
		t.Error("EventRule not created")
	}
}

func TestNewBus_RulePhysicalNameQualified(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		NewBus(ctx, "Events", &BusArgs{
			Rules: []BusRule{
				{Name: "orders", Pattern: `{"source":["myapp.orders"]}`},
			},
		})
	})

	r := mocks.find("aws:cloudwatch/eventRule:EventRule")
	if r == nil {
		t.Fatal("EventRule not registered")
	}
	if r.inputs["name"].StringValue() != "myapp-test-Events-orders" {
		t.Errorf("rule name = %q, want myapp-test-Events-orders", r.inputs["name"].StringValue())
	}
}

func TestNewBus_RuleCreatedWithSchedule(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		NewBus(ctx, "Events", &BusArgs{
			Rules: []BusRule{
				{Name: "heartbeat", Schedule: "rate(5 minutes)"},
			},
		})
	})

	r := mocks.find("aws:cloudwatch/eventRule:EventRule")
	if r == nil {
		t.Fatal("EventRule not registered")
	}
	if r.inputs["scheduleExpression"].StringValue() != "rate(5 minutes)" {
		t.Errorf("scheduleExpression = %q, want %q", r.inputs["scheduleExpression"].StringValue(), "rate(5 minutes)")
	}
}

func TestNewBus_LambdaTargetCreated(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		NewBus(ctx, "Events", &BusArgs{
			Rules: []BusRule{
				{
					Name:    "orders",
					Pattern: `{"source":["myapp.orders"]}`,
					Targets: []*FunctionArgs{{Handler: "bootstrap"}},
				},
			},
		})
	})

	if mocks.find("aws:lambda/function:Function") == nil {
		t.Error("Lambda function not created for target")
	}
	if mocks.find("aws:cloudwatch/eventTarget:EventTarget") == nil {
		t.Error("EventTarget not created for Lambda")
	}
	if mocks.find("aws:lambda/permission:Permission") == nil {
		t.Error("Lambda permission not created for EventBridge")
	}
}

func TestNewBus_MultipleLambdaTargets(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		NewBus(ctx, "Events", &BusArgs{
			Rules: []BusRule{
				{
					Name:    "orders",
					Pattern: `{"source":["myapp.orders"]}`,
					Targets: []*FunctionArgs{
						{Handler: "bootstrap"},
						{Handler: "bootstrap"},
					},
				},
			},
		})
	})

	lambdas := mocks.findAll("aws:lambda/function:Function")
	if len(lambdas) != 2 {
		t.Errorf("expected 2 Lambda functions, got %d", len(lambdas))
	}
	targets := mocks.findAll("aws:cloudwatch/eventTarget:EventTarget")
	if len(targets) != 2 {
		t.Errorf("expected 2 EventTargets, got %d", len(targets))
	}
}

func TestNewBus_MultipleRules(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		NewBus(ctx, "Events", &BusArgs{
			Rules: []BusRule{
				{Name: "orders", Pattern: `{"source":["orders"]}`},
				{Name: "payments", Pattern: `{"source":["payments"]}`},
			},
		})
	})

	rules := mocks.findAll("aws:cloudwatch/eventRule:EventRule")
	if len(rules) != 2 {
		t.Errorf("expected 2 rules, got %d", len(rules))
	}
}

func TestNewBus_SQSTargetCreated(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		q := NewQueue(ctx, "DeadEvents", nil)
		NewBus(ctx, "Events", &BusArgs{
			Rules: []BusRule{
				{
					Name:         "orders",
					Pattern:      `{"source":["myapp.orders"]}`,
					QueueTargets: []*Queue{q},
				},
			},
		})
	})

	if mocks.find("aws:cloudwatch/eventTarget:EventTarget") == nil {
		t.Error("EventTarget not created for SQS queue")
	}
	if mocks.find("aws:sqs/queuePolicy:QueuePolicy") == nil {
		t.Error("SQS QueuePolicy not created for EventBridge target")
	}
}

func TestNewBus_EmptyRuleNamePanics(t *testing.T) {
	t.Parallel()
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for empty BusRule.Name")
		}
	}()
	runTest(t, func(ctx *forge.RunContext) {
		NewBus(ctx, "Events", &BusArgs{
			Rules: []BusRule{{Pattern: `{"source":["x"]}`}}, // Name empty
		})
	})
}

func TestNewBus_RuleMissingPatternAndSchedulePanics(t *testing.T) {
	t.Parallel()
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for rule with no pattern or schedule")
		}
	}()
	runTest(t, func(ctx *forge.RunContext) {
		NewBus(ctx, "Events", &BusArgs{
			Rules: []BusRule{{Name: "bad"}}, // neither Pattern nor Schedule
		})
	})
}

func TestNewBus_LinkEnvKeys(t *testing.T) {
	t.Parallel()
	runTest(t, func(ctx *forge.RunContext) {
		b := NewBus(ctx, "Events", nil)
		env := b.LinkEnv()
		if _, ok := env["SST_BUS_EVENTS_NAME"]; !ok {
			t.Error("LinkEnv missing SST_BUS_EVENTS_NAME")
		}
		if _, ok := env["SST_BUS_EVENTS_ARN"]; !ok {
			t.Error("LinkEnv missing SST_BUS_EVENTS_ARN")
		}
		if len(env) != 2 {
			t.Errorf("LinkEnv has %d keys, want 2", len(env))
		}
	})
}

func TestNewBus_CamelCaseLinkEnvKey(t *testing.T) {
	t.Parallel()
	runTest(t, func(ctx *forge.RunContext) {
		b := NewBus(ctx, "OrderEvents", nil)
		env := b.LinkEnv()
		if _, ok := env["SST_BUS_ORDER_EVENTS_NAME"]; !ok {
			t.Error("LinkEnv missing SST_BUS_ORDER_EVENTS_NAME")
		}
	})
}

func TestNewBus_LinkName(t *testing.T) {
	t.Parallel()
	runTest(t, func(ctx *forge.RunContext) {
		b := NewBus(ctx, "Events", nil)
		if b.LinkName() != "Events" {
			t.Errorf("LinkName = %q, want Events", b.LinkName())
		}
	})
}

func TestNewBus_ImplementsLinkable(t *testing.T) {
	t.Parallel()
	runTest(t, func(ctx *forge.RunContext) {
		b := NewBus(ctx, "Events", nil)
		var _ forge.Linkable = b
	})
}

func TestNewBus_LambdaPermissionPrincipalIsEventBridge(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		NewBus(ctx, "Events", &BusArgs{
			Rules: []BusRule{
				{
					Name:    "orders",
					Pattern: `{"source":["myapp"]}`,
					Targets: []*FunctionArgs{{Handler: "bootstrap"}},
				},
			},
		})
	})

	perm := mocks.find("aws:lambda/permission:Permission")
	if perm == nil {
		t.Fatal("Lambda permission not registered")
	}
	if !strings.Contains(perm.inputs["principal"].StringValue(), "events.amazonaws.com") {
		t.Errorf("permission principal = %q, want events.amazonaws.com", perm.inputs["principal"].StringValue())
	}
}

func TestNewBus_RuleEventPatternSet(t *testing.T) {
	t.Parallel()
	pattern := `{"source":["myapp.orders"]}`
	mocks := runTest(t, func(ctx *forge.RunContext) {
		NewBus(ctx, "Events", &BusArgs{
			Rules: []BusRule{
				{Name: "orders", Pattern: pattern},
			},
		})
	})

	r := mocks.find("aws:cloudwatch/eventRule:EventRule")
	if r == nil {
		t.Fatal("EventRule not registered")
	}
	if r.inputs["eventPattern"].StringValue() != pattern {
		t.Errorf("eventPattern = %q, want %q", r.inputs["eventPattern"].StringValue(), pattern)
	}
}
