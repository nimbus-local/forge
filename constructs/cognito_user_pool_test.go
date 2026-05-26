package constructs

import (
	"testing"

	forge "github.com/nimbus-local/forge"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

func TestNewCognitoUserPool_PoolAndClientCreated(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		NewCognitoUserPool(ctx, "Auth", nil)
	})

	if mocks.find("aws:cognito/userPool:UserPool") == nil {
		t.Error("Cognito User Pool not created")
	}
	if mocks.find("aws:cognito/userPoolClient:UserPoolClient") == nil {
		t.Error("Cognito User Pool client not created")
	}
}

func TestNewCognitoUserPool_PhysicalNameQualified(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		NewCognitoUserPool(ctx, "Auth", nil)
	})

	r := mocks.find("aws:cognito/userPool:UserPool")
	if r == nil {
		t.Fatal("User Pool not registered")
	}
	if r.inputs["name"].StringValue() != "myapp-test-Auth" {
		t.Errorf("pool name = %q, want myapp-test-Auth", r.inputs["name"].StringValue())
	}
}

func TestNewCognitoUserPool_TagsApplied(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		NewCognitoUserPool(ctx, "Auth", nil)
	})

	r := mocks.find("aws:cognito/userPool:UserPool")
	if r == nil {
		t.Fatal("User Pool not registered")
	}
	for _, tag := range []string{"forge:app", "forge:stage", "forge:name"} {
		assertTag(t, r.inputs, tag)
	}
}

func TestNewCognitoUserPool_DefaultMFAOff(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		NewCognitoUserPool(ctx, "Auth", nil)
	})

	r := mocks.find("aws:cognito/userPool:UserPool")
	if r == nil {
		t.Fatal("User Pool not registered")
	}
	if r.inputs["mfaConfiguration"].StringValue() != "OFF" {
		t.Errorf("mfaConfiguration = %q, want OFF", r.inputs["mfaConfiguration"].StringValue())
	}
}

func TestNewCognitoUserPool_MFAOn(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		NewCognitoUserPool(ctx, "Auth", &CognitoUserPoolArgs{
			MFA:           "on",
			SoftwareToken: true,
		})
	})

	r := mocks.find("aws:cognito/userPool:UserPool")
	if r == nil {
		t.Fatal("User Pool not registered")
	}
	if r.inputs["mfaConfiguration"].StringValue() != "on" {
		t.Errorf("mfaConfiguration = %q, want on", r.inputs["mfaConfiguration"].StringValue())
	}
}

func TestNewCognitoUserPool_AliasAttributes(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		NewCognitoUserPool(ctx, "Auth", &CognitoUserPoolArgs{
			Aliases: []string{"email", "phone_number"},
		})
	})

	r := mocks.find("aws:cognito/userPool:UserPool")
	if r == nil {
		t.Fatal("User Pool not registered")
	}
	aliases := r.inputs["aliasAttributes"]
	if !aliases.IsArray() || len(aliases.ArrayValue()) != 2 {
		t.Errorf("aliasAttributes = %v, want 2-element array", aliases)
	}
}

func TestNewCognitoUserPool_UsernameAttributes(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		NewCognitoUserPool(ctx, "Auth", &CognitoUserPoolArgs{
			Usernames: []string{"email"},
		})
	})

	r := mocks.find("aws:cognito/userPool:UserPool")
	if r == nil {
		t.Fatal("User Pool not registered")
	}
	usernames := r.inputs["usernameAttributes"]
	if !usernames.IsArray() || len(usernames.ArrayValue()) != 1 {
		t.Errorf("usernameAttributes = %v, want 1-element array", usernames)
	}
}

func TestNewCognitoUserPool_LinkEnvKeys(t *testing.T) {
	t.Parallel()
	runTest(t, func(ctx *forge.RunContext) {
		pool := NewCognitoUserPool(ctx, "Auth", nil)
		env := pool.LinkEnv()
		if _, ok := env["SST_COGNITO_AUTH_USER_POOL_ID"]; !ok {
			t.Error("LinkEnv missing SST_COGNITO_AUTH_USER_POOL_ID")
		}
		if _, ok := env["SST_COGNITO_AUTH_CLIENT_ID"]; !ok {
			t.Error("LinkEnv missing SST_COGNITO_AUTH_CLIENT_ID")
		}
		if len(env) != 2 {
			t.Errorf("LinkEnv has %d keys, want 2", len(env))
		}
	})
}

func TestNewCognitoUserPool_LinkName(t *testing.T) {
	t.Parallel()
	runTest(t, func(ctx *forge.RunContext) {
		pool := NewCognitoUserPool(ctx, "Auth", nil)
		if pool.LinkName() != "Auth" {
			t.Errorf("LinkName = %q, want Auth", pool.LinkName())
		}
	})
}

func TestNewCognitoUserPool_ImplementsLinkable(t *testing.T) {
	t.Parallel()
	runTest(t, func(ctx *forge.RunContext) {
		pool := NewCognitoUserPool(ctx, "Auth", nil)
		var _ forge.Linkable = pool
	})
}

func TestNewCognitoUserPool_TriggersCreatePermissions(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		preSignUp := NewFunction(ctx, "PreSignUp", &FunctionArgs{Handler: "bootstrap"})
		NewCognitoUserPool(ctx, "Auth", &CognitoUserPoolArgs{
			Triggers: &CognitoTriggers{
				PreSignUp: preSignUp,
			},
		})
	})

	if mocks.find("aws:lambda/permission:Permission") == nil {
		t.Error("Lambda invoke permission not created for trigger")
	}
}

func TestNewCognitoUserPool_NilArgsSafe(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		NewCognitoUserPool(ctx, "Auth", nil)
	})

	if mocks.find("aws:cognito/userPool:UserPool") == nil {
		t.Error("User Pool not created with nil args")
	}
}

func TestNewCognitoUserPool_ClientHasSRPAuthFlow(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		NewCognitoUserPool(ctx, "Auth", nil)
	})

	r := mocks.find("aws:cognito/userPoolClient:UserPoolClient")
	if r == nil {
		t.Fatal("User Pool client not registered")
	}
	flows := r.inputs["explicitAuthFlows"]
	if !flows.IsArray() {
		t.Error("explicitAuthFlows should be an array")
	}
	found := false
	for _, v := range flows.ArrayValue() {
		if v.StringValue() == "ALLOW_USER_SRP_AUTH" {
			found = true
			break
		}
	}
	if !found {
		t.Error("ALLOW_USER_SRP_AUTH not in explicitAuthFlows")
	}
}

func TestNewCognitoUserPool_CamelCaseLinkEnvKey(t *testing.T) {
	t.Parallel()
	runTest(t, func(ctx *forge.RunContext) {
		pool := NewCognitoUserPool(ctx, "MyUserPool", nil)
		env := pool.LinkEnv()
		if _, ok := env["SST_COGNITO_MY_USER_POOL_USER_POOL_ID"]; !ok {
			t.Error("LinkEnv missing SST_COGNITO_MY_USER_POOL_USER_POOL_ID")
		}
	})
}

func TestNewCognitoUserPool_AccountRecoverySet(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		NewCognitoUserPool(ctx, "Auth", nil)
	})

	r := mocks.find("aws:cognito/userPool:UserPool")
	if r == nil {
		t.Fatal("User Pool not registered")
	}
	recovery := r.inputs["accountRecoverySetting"]
	if recovery.IsNull() {
		t.Error("accountRecoverySetting should be set")
	}
}

// TestNewCognitoUserPool_EmptyNilPanicsOnMisuse ensures the pool+client are always created.
func TestNewCognitoUserPool_EmptyArgsCreatesPool(t *testing.T) {
	t.Parallel()
	mocks := newMocks()
	err := pulumi.RunErr(func(pctx *pulumi.Context) error {
		ctx := forge.NewRunContext(pctx, testApp, "test", "123456789012")
		NewCognitoUserPool(ctx, "Auth", &CognitoUserPoolArgs{})
		return nil
	}, pulumi.WithMocks("myapp", "test", mocks))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mocks.find("aws:cognito/userPool:UserPool") == nil {
		t.Error("User Pool not created with empty args")
	}
}
