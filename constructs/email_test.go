package constructs

import (
	"testing"

	forge "github.com/nimbus-local/forge"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

func TestNewEmail_IdentityAndConfigSetCreated(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		NewEmail(ctx, "Notify", &EmailArgs{Sender: "hello@example.com"})
	})

	if mocks.find("aws:sesv2/emailIdentity:EmailIdentity") == nil {
		t.Error("SESv2 email identity not created")
	}
	if mocks.find("aws:sesv2/configurationSet:ConfigurationSet") == nil {
		t.Error("SESv2 configuration set not created")
	}
}

func TestNewEmail_SenderSetOnIdentity(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		NewEmail(ctx, "Notify", &EmailArgs{Sender: "hello@example.com"})
	})

	r := mocks.find("aws:sesv2/emailIdentity:EmailIdentity")
	if r == nil {
		t.Fatal("email identity not created")
	}
	if r.inputs["emailIdentity"].StringValue() != "hello@example.com" {
		t.Errorf("emailIdentity = %q, want hello@example.com", r.inputs["emailIdentity"].StringValue())
	}
}

func TestNewEmail_ConfigSetNameQualified(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		NewEmail(ctx, "Notify", &EmailArgs{Sender: "hello@example.com"})
	})

	r := mocks.find("aws:sesv2/configurationSet:ConfigurationSet")
	if r == nil {
		t.Fatal("configuration set not created")
	}
	name := r.inputs["configurationSetName"].StringValue()
	if name != "myapp-test-Notify" {
		t.Errorf("configurationSetName = %q, want myapp-test-Notify", name)
	}
}

func TestNewEmail_ConfigSetLinkedToIdentity(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		NewEmail(ctx, "Notify", &EmailArgs{Sender: "hello@example.com"})
	})

	identity := mocks.find("aws:sesv2/emailIdentity:EmailIdentity")
	if identity == nil {
		t.Fatal("email identity not created")
	}
	// The identity must reference the configuration set name.
	cfgSetName := identity.inputs["configurationSetName"]
	if cfgSetName.IsNull() || cfgSetName.StringValue() == "" {
		t.Error("emailIdentity does not reference configurationSetName")
	}
}

func TestNewEmail_LinkEnvKeys(t *testing.T) {
	t.Parallel()
	runTest(t, func(ctx *forge.RunContext) {
		e := NewEmail(ctx, "Notify", &EmailArgs{Sender: "hello@example.com"})
		env := e.LinkEnv()
		if _, ok := env["SST_EMAIL_NOTIFY_SENDER"]; !ok {
			t.Error("LinkEnv missing SST_EMAIL_NOTIFY_SENDER")
		}
		if _, ok := env["SST_EMAIL_NOTIFY_CONFIG_SET"]; !ok {
			t.Error("LinkEnv missing SST_EMAIL_NOTIFY_CONFIG_SET")
		}
		if len(env) != 2 {
			t.Errorf("LinkEnv has %d keys, want 2", len(env))
		}
	})
}

func TestNewEmail_LinkName(t *testing.T) {
	t.Parallel()
	runTest(t, func(ctx *forge.RunContext) {
		e := NewEmail(ctx, "Notify", &EmailArgs{Sender: "hello@example.com"})
		if e.LinkName() != "Notify" {
			t.Errorf("LinkName = %q, want Notify", e.LinkName())
		}
	})
}

func TestNewEmail_TagsApplied(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		NewEmail(ctx, "Notify", &EmailArgs{Sender: "hello@example.com"})
	})

	r := mocks.find("aws:sesv2/emailIdentity:EmailIdentity")
	if r == nil {
		t.Fatal("email identity not created")
	}
	for _, tag := range []string{"forge:app", "forge:stage", "forge:name"} {
		assertTag(t, r.inputs, tag)
	}
}

func TestNewEmail_DomainSenderWorks(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		NewEmail(ctx, "Newsletter", &EmailArgs{Sender: "example.com"})
	})

	r := mocks.find("aws:sesv2/emailIdentity:EmailIdentity")
	if r == nil {
		t.Fatal("email identity not created for domain sender")
	}
	if r.inputs["emailIdentity"].StringValue() != "example.com" {
		t.Errorf("emailIdentity = %q, want example.com", r.inputs["emailIdentity"].StringValue())
	}
}

func TestNewEmail_EmptySenderPanics(t *testing.T) {
	t.Parallel()
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for empty Sender")
		}
	}()

	mocks := newMocks()
	_ = pulumi.RunErr(func(pctx *pulumi.Context) error {
		ctx := forge.NewRunContext(pctx, testApp, "test", "123456789012")
		NewEmail(ctx, "Bad", &EmailArgs{}) // empty Sender → should panic
		return nil
	}, pulumi.WithMocks("myapp", "test", mocks))
}

func TestNewEmail_NilArgsPanics(t *testing.T) {
	t.Parallel()
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for nil args (no Sender)")
		}
	}()

	mocks := newMocks()
	_ = pulumi.RunErr(func(pctx *pulumi.Context) error {
		ctx := forge.NewRunContext(pctx, testApp, "test", "123456789012")
		NewEmail(ctx, "Bad", nil) // nil → should panic (no Sender)
		return nil
	}, pulumi.WithMocks("myapp", "test", mocks))
}

func TestNewEmail_ImplementsLinkable(t *testing.T) {
	t.Parallel()
	runTest(t, func(ctx *forge.RunContext) {
		e := NewEmail(ctx, "Notify", &EmailArgs{Sender: "hello@example.com"})
		// Verify it satisfies the forge.Linkable interface at compile time.
		var _ forge.Linkable = e
	})
}
