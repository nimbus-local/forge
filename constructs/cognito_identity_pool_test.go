package constructs

import (
	"testing"

	forge "github.com/nimbus-local/forge"
)

func TestNewCognitoIdentityPool_PoolCreated(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		NewCognitoIdentityPool(ctx, "Federated", nil)
	})

	if mocks.find("aws:cognito/identityPool:IdentityPool") == nil {
		t.Error("Cognito Identity Pool not created")
	}
}

func TestNewCognitoIdentityPool_PhysicalNameQualified(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		NewCognitoIdentityPool(ctx, "Federated", nil)
	})

	r := mocks.find("aws:cognito/identityPool:IdentityPool")
	if r == nil {
		t.Fatal("Identity Pool not registered")
	}
	if r.inputs["identityPoolName"].StringValue() != "myapp-test-Federated" {
		t.Errorf("identityPoolName = %q, want myapp-test-Federated", r.inputs["identityPoolName"].StringValue())
	}
}

func TestNewCognitoIdentityPool_TagsApplied(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		NewCognitoIdentityPool(ctx, "Federated", nil)
	})

	r := mocks.find("aws:cognito/identityPool:IdentityPool")
	if r == nil {
		t.Fatal("Identity Pool not registered")
	}
	for _, tag := range []string{"forge:app", "forge:stage", "forge:name"} {
		assertTag(t, r.inputs, tag)
	}
}

func TestNewCognitoIdentityPool_AuthenticatedRoleCreated(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		NewCognitoIdentityPool(ctx, "Federated", nil)
	})

	roles := mocks.findAll("aws:iam/role:Role")
	if len(roles) == 0 {
		t.Error("no IAM roles created — expect at least authenticated role")
	}
}

func TestNewCognitoIdentityPool_NoUnauthRoleByDefault(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		NewCognitoIdentityPool(ctx, "Federated", nil)
	})

	roles := mocks.findAll("aws:iam/role:Role")
	// Only the authenticated role should be created.
	if len(roles) != 1 {
		t.Errorf("expected 1 IAM role (auth only), got %d", len(roles))
	}
}

func TestNewCognitoIdentityPool_UnauthRoleCreatedWhenEnabled(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		NewCognitoIdentityPool(ctx, "Federated", &CognitoIdentityPoolArgs{
			AllowUnauthenticated: true,
		})
	})

	roles := mocks.findAll("aws:iam/role:Role")
	if len(roles) != 2 {
		t.Errorf("expected 2 IAM roles (auth + unauth), got %d", len(roles))
	}
}

func TestNewCognitoIdentityPool_UnauthDisabledByDefault(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		NewCognitoIdentityPool(ctx, "Federated", nil)
	})

	r := mocks.find("aws:cognito/identityPool:IdentityPool")
	if r == nil {
		t.Fatal("Identity Pool not registered")
	}
	if r.inputs["allowUnauthenticatedIdentities"].IsBool() && r.inputs["allowUnauthenticatedIdentities"].BoolValue() {
		t.Error("allowUnauthenticatedIdentities should be false by default")
	}
}

func TestNewCognitoIdentityPool_RoleAttachmentCreated(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		NewCognitoIdentityPool(ctx, "Federated", nil)
	})

	if mocks.find("aws:cognito/identityPoolRoleAttachment:IdentityPoolRoleAttachment") == nil {
		t.Error("Identity Pool role attachment not created")
	}
}

func TestNewCognitoIdentityPool_LinkEnvKeys(t *testing.T) {
	t.Parallel()
	runTest(t, func(ctx *forge.RunContext) {
		pool := NewCognitoIdentityPool(ctx, "Federated", nil)
		env := pool.LinkEnv()
		if _, ok := env["SST_IDENTITY_POOL_FEDERATED_ID"]; !ok {
			t.Error("LinkEnv missing SST_IDENTITY_POOL_FEDERATED_ID")
		}
		if len(env) != 1 {
			t.Errorf("LinkEnv has %d keys, want 1", len(env))
		}
	})
}

func TestNewCognitoIdentityPool_LinkName(t *testing.T) {
	t.Parallel()
	runTest(t, func(ctx *forge.RunContext) {
		pool := NewCognitoIdentityPool(ctx, "Federated", nil)
		if pool.LinkName() != "Federated" {
			t.Errorf("LinkName = %q, want Federated", pool.LinkName())
		}
	})
}

func TestNewCognitoIdentityPool_ImplementsLinkable(t *testing.T) {
	t.Parallel()
	runTest(t, func(ctx *forge.RunContext) {
		pool := NewCognitoIdentityPool(ctx, "Federated", nil)
		var _ forge.Linkable = pool
	})
}

func TestNewCognitoIdentityPool_WithUserPool(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		userPool := NewCognitoUserPool(ctx, "Auth", nil)
		NewCognitoIdentityPool(ctx, "Federated", &CognitoIdentityPoolArgs{
			UserPools: []UserPoolProvider{{UserPool: userPool}},
		})
	})

	r := mocks.find("aws:cognito/identityPool:IdentityPool")
	if r == nil {
		t.Fatal("Identity Pool not registered")
	}
	providers := r.inputs["cognitoIdentityProviders"]
	if !providers.IsArray() || len(providers.ArrayValue()) != 1 {
		t.Errorf("cognitoIdentityProviders = %v, want 1-element array", providers)
	}
}

func TestNewCognitoIdentityPool_NilArgsSafe(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		NewCognitoIdentityPool(ctx, "Federated", nil)
	})

	if mocks.find("aws:cognito/identityPool:IdentityPool") == nil {
		t.Error("Identity Pool not created with nil args")
	}
}

func TestNewCognitoIdentityPool_CamelCaseLinkEnvKey(t *testing.T) {
	t.Parallel()
	runTest(t, func(ctx *forge.RunContext) {
		pool := NewCognitoIdentityPool(ctx, "MyIdentity", nil)
		env := pool.LinkEnv()
		if _, ok := env["SST_IDENTITY_POOL_MY_IDENTITY_ID"]; !ok {
			t.Error("LinkEnv missing SST_IDENTITY_POOL_MY_IDENTITY_ID")
		}
	})
}
