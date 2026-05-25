package constructs

import (
	forge "github.com/nimbus-local/forge"
	"github.com/pulumi/pulumi-aws/sdk/v7/go/aws/sesv2"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

// Email creates an SES v2 email identity for sending email from an address or domain.
//
// For address identities (e.g. "hello@example.com"), AWS sends a verification email;
// the address must be clicked before SES will deliver from it.
//
// For domain identities (e.g. "example.com"), DKIM tokens are available via the
// DKIMTokens() output — add them as CNAME records in your DNS to enable signing.
// AWS SES stays in sandbox mode until production access is requested via the console.
//
// LinkEnv keys injected into linked Functions:
//
//	SST_EMAIL_<NAME>_SENDER      — verified From address / domain
//	SST_EMAIL_<NAME>_CONFIG_SET  — configuration set name
type Email struct {
	name             string
	identity         *sesv2.EmailIdentity
	configurationSet *sesv2.ConfigurationSet
	ctx              *forge.RunContext
}

// EmailArgs configures an Email construct.
type EmailArgs struct {
	// Sender is the email address or domain to send from.
	// For an address identity: "hello@example.com" (AWS sends a verification email).
	// For a domain identity: "example.com" (add DKIM CNAME records from DKIMTokens() to DNS).
	Sender string

	// Tags merged with stage-level tags on every resource.
	Tags map[string]string
}

// NewEmail creates an Email construct backed by an SESv2 email identity.
func NewEmail(ctx *forge.RunContext, name string, args *EmailArgs) *Email {
	if args == nil {
		args = &EmailArgs{}
	}
	if args.Sender == "" {
		panic("forge: EmailArgs.Sender is required")
	}

	pctx := ctx.Pulumi()
	tags := mergedTags(defaultTags(ctx, name), args.Tags)

	// ── Configuration set ─────────────────────────────────────────────────────
	cfgSet, err := sesv2.NewConfigurationSet(pctx, name+"-cfg", &sesv2.ConfigurationSetArgs{
		ConfigurationSetName: pulumi.String(qualifiedName(ctx, name)),
		Tags:                 tags,
	})
	panicOnErr(err, name+": configuration set")

	// ── Email identity ────────────────────────────────────────────────────────
	identity, err := sesv2.NewEmailIdentity(pctx, name+"-identity", &sesv2.EmailIdentityArgs{
		EmailIdentity:        pulumi.String(args.Sender),
		ConfigurationSetName: cfgSet.ConfigurationSetName,
		Tags:                 tags,
	})
	panicOnErr(err, name+": email identity")

	return &Email{
		name:             name,
		identity:         identity,
		configurationSet: cfgSet,
		ctx:              ctx,
	}
}

// ── Accessors ─────────────────────────────────────────────────────────────────

// Sender returns the configured email address or domain.
func (e *Email) Sender() pulumi.StringOutput { return e.identity.EmailIdentity }

// ARN returns the SES identity ARN.
func (e *Email) ARN() pulumi.StringOutput { return e.identity.Arn }

// ConfigSet returns the SESv2 configuration set name.
func (e *Email) ConfigSet() pulumi.StringOutput { return e.configurationSet.ConfigurationSetName }

// DKIMTokens returns the three DKIM signing tokens for domain identities.
// Add each as a CNAME: <token>._domainkey.<domain> → <token>.dkim.amazonses.com
func (e *Email) DKIMTokens() pulumi.StringArrayOutput {
	return e.identity.DkimSigningAttributes.Tokens()
}

// ── Linkable ──────────────────────────────────────────────────────────────────

// LinkEnv implements forge.Linkable.
func (e *Email) LinkEnv() pulumi.StringMap {
	k := envKey(e.name)
	return pulumi.StringMap{
		"SST_EMAIL_" + k + "_SENDER":     e.Sender(),
		"SST_EMAIL_" + k + "_CONFIG_SET": e.ConfigSet(),
	}
}

// LinkName implements forge.Linkable.
func (e *Email) LinkName() string { return e.name }
