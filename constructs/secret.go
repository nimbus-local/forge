package constructs

import (
	"fmt"

	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/ssm"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	forge "github.com/sst-go/forge"
)

// SecretArgs configures a managed secret reference.
type SecretArgs struct {
	// Default is the fallback value when the SSM parameter does not yet exist.
	// WARNING: stored in Pulumi state — only use for non-sensitive placeholder defaults.
	Default string
}

// Secret resolves an SSM SecureString at deploy time and injects the value
// into any linked Lambda via SST_SECRET_<NAME>.
// The parameter path is /forge/<app>/<stage>/<name>.
type Secret struct {
	name  string
	value pulumi.StringOutput
	ctx   *forge.RunContext
}

// NewSecret creates a Secret construct that resolves an SSM SecureString at deploy time.
// If the parameter is not found and no Default is provided, deployment panics with an
// actionable message pointing the user to `forge secret set`.
func NewSecret(ctx *forge.RunContext, name string, args *SecretArgs) *Secret {
	if args == nil {
		args = &SecretArgs{}
	}

	path := fmt.Sprintf("/forge/%s/%s/%s", ctx.App.Name, ctx.Stage, name)
	withDecryption := true

	result, err := ssm.LookupParameter(ctx.Pulumi(), &ssm.LookupParameterArgs{
		Name:           path,
		WithDecryption: &withDecryption,
	})

	var value pulumi.StringOutput
	if err != nil {
		if args.Default == "" {
			panic(fmt.Sprintf("forge: secret %q not found at %s — run: forge secret set %s <value>", name, path, name))
		}
		value = pulumi.String(args.Default).ToStringOutput()
	} else {
		value = pulumi.String(result.Value).ToStringOutput()
	}

	return &Secret{name: name, value: value, ctx: ctx}
}

// Value returns the resolved secret value as a Pulumi output.
func (s *Secret) Value() pulumi.StringOutput { return s.value }

// LinkEnv implements Linkable — injects the resolved secret into linked Lambdas.
func (s *Secret) LinkEnv() pulumi.StringMap {
	key := envKey(s.name)
	return pulumi.StringMap{
		fmt.Sprintf("SST_SECRET_%s", key): s.value,
	}
}

// LinkName implements Linkable.
func (s *Secret) LinkName() string { return s.name }
