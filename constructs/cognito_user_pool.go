package constructs

import (
	"encoding/json"
	"fmt"

	forge "github.com/nimbus-local/forge"
	"github.com/pulumi/pulumi-aws/sdk/v7/go/aws/cognito"
	"github.com/pulumi/pulumi-aws/sdk/v7/go/aws/iam"
	awslambda "github.com/pulumi/pulumi-aws/sdk/v7/go/aws/lambda"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

// CognitoUserPool creates an Amazon Cognito User Pool with a default app client.
//
// LinkEnv keys injected into linked Functions:
//
//	SST_COGNITO_<NAME>_USER_POOL_ID — Cognito User Pool ID
//	SST_COGNITO_<NAME>_CLIENT_ID    — default app client ID
//
// Linked Functions automatically receive cognito-idp:* permissions on the pool.
type CognitoUserPool struct {
	name   string
	pool   *cognito.UserPool
	client *cognito.UserPoolClient
	ctx    *forge.RunContext
}

// CognitoUserPoolArgs configures a CognitoUserPool construct.
type CognitoUserPoolArgs struct {
	// Aliases allows users to sign in with email, phone, or preferred_username
	// in addition to their username. Cannot be changed after pool creation.
	Aliases []string

	// Usernames allows email or phone to be used as the username itself.
	// Mutually exclusive with Aliases. Cannot be changed after pool creation.
	Usernames []string

	// MFA configures multi-factor authentication ("on" or "optional").
	// When enabled, configure SoftwareToken or SMS settings as well.
	// Defaults to "off".
	MFA string

	// SoftwareToken enables TOTP-based MFA (Google Authenticator, etc.).
	SoftwareToken bool

	// Triggers wires Lambda functions to Cognito lifecycle events.
	Triggers *CognitoTriggers

	// Tags merged with stage-level tags on every resource.
	Tags map[string]string
}

// CognitoTriggers maps Cognito User Pool trigger events to Lambda functions.
// Each field accepts a *Function created with NewFunction, or nil to skip.
type CognitoTriggers struct {
	PreSignUp                   *Function
	PostConfirmation            *Function
	PreAuthentication           *Function
	PostAuthentication          *Function
	CustomMessage               *Function
	PreTokenGeneration          *Function
	UserMigration               *Function
	DefineAuthChallenge         *Function
	CreateAuthChallenge         *Function
	VerifyAuthChallengeResponse *Function
}

// NewCognitoUserPool creates a Cognito User Pool with a default app client.
func NewCognitoUserPool(ctx *forge.RunContext, name string, args *CognitoUserPoolArgs) *CognitoUserPool {
	if args == nil {
		args = &CognitoUserPoolArgs{}
	}

	pctx := ctx.Pulumi()
	tags := mergedTags(defaultTags(ctx, name), args.Tags)

	// ── User Pool ─────────────────────────────────────────────────────────────
	poolArgs := &cognito.UserPoolArgs{
		Name: pulumi.String(qualifiedName(ctx, name)),
		Tags: tags,
		// Sane defaults: email-based recovery, verified email attribute.
		AccountRecoverySetting: &cognito.UserPoolAccountRecoverySettingArgs{
			RecoveryMechanisms: cognito.UserPoolAccountRecoverySettingRecoveryMechanismArray{
				&cognito.UserPoolAccountRecoverySettingRecoveryMechanismArgs{
					Name:     pulumi.String("verified_email"),
					Priority: pulumi.Int(1),
				},
			},
		},
	}

	if len(args.Aliases) > 0 {
		aliases := make(pulumi.StringArray, len(args.Aliases))
		for i, a := range args.Aliases {
			aliases[i] = pulumi.String(a)
		}
		poolArgs.AliasAttributes = aliases
	}

	if len(args.Usernames) > 0 {
		usernames := make(pulumi.StringArray, len(args.Usernames))
		for i, u := range args.Usernames {
			usernames[i] = pulumi.String(u)
		}
		poolArgs.UsernameAttributes = usernames
	}

	mfa := args.MFA
	if mfa == "" {
		mfa = "OFF"
	}
	poolArgs.MfaConfiguration = pulumi.String(mfa)

	if args.SoftwareToken {
		poolArgs.SoftwareTokenMfaConfiguration = &cognito.UserPoolSoftwareTokenMfaConfigurationArgs{
			Enabled: pulumi.Bool(true),
		}
	}

	if args.Triggers != nil {
		poolArgs.LambdaConfig = buildLambdaConfig(pctx, name, args.Triggers)
	}

	pool, err := cognito.NewUserPool(pctx, name+"-pool", poolArgs)
	panicOnErr(err, name+": user pool")

	// ── Default app client ────────────────────────────────────────────────────
	client, err := cognito.NewUserPoolClient(pctx, name+"-client", &cognito.UserPoolClientArgs{
		Name:       pulumi.String(qualifiedName(ctx, name)),
		UserPoolId: pool.ID(),
		ExplicitAuthFlows: pulumi.StringArray{
			pulumi.String("ALLOW_USER_SRP_AUTH"),
			pulumi.String("ALLOW_REFRESH_TOKEN_AUTH"),
		},
	})
	panicOnErr(err, name+": user pool client")

	return &CognitoUserPool{
		name:   name,
		pool:   pool,
		client: client,
		ctx:    ctx,
	}
}

// buildLambdaConfig constructs the LambdaConfig block and wires invoke permissions.
func buildLambdaConfig(pctx *pulumi.Context, name string, t *CognitoTriggers) *cognito.UserPoolLambdaConfigArgs {
	cfg := &cognito.UserPoolLambdaConfigArgs{}

	wire := func(label string, fn *Function) pulumi.StringInput {
		if fn == nil {
			return nil
		}
		// Allow Cognito to invoke the Lambda.
		_, err := awslambda.NewPermission(pctx, name+"-trigger-"+label, &awslambda.PermissionArgs{
			Action:    pulumi.String("lambda:InvokeFunction"),
			Function:  fn.resource.Name,
			Principal: pulumi.String("cognito-idp.amazonaws.com"),
		})
		panicOnErr(err, fmt.Sprintf("%s: lambda permission for trigger %s", name, label))
		return fn.ARN()
	}

	if v := wire("pre-signup", t.PreSignUp); v != nil {
		cfg.PreSignUp = v
	}
	if v := wire("post-confirmation", t.PostConfirmation); v != nil {
		cfg.PostConfirmation = v
	}
	if v := wire("pre-auth", t.PreAuthentication); v != nil {
		cfg.PreAuthentication = v
	}
	if v := wire("post-auth", t.PostAuthentication); v != nil {
		cfg.PostAuthentication = v
	}
	if v := wire("custom-message", t.CustomMessage); v != nil {
		cfg.CustomMessage = v
	}
	if v := wire("pre-token", t.PreTokenGeneration); v != nil {
		cfg.PreTokenGenerationConfig = &cognito.UserPoolLambdaConfigPreTokenGenerationConfigArgs{
			LambdaArn:     v,
			LambdaVersion: pulumi.String("V1_0"),
		}
	}
	if v := wire("user-migration", t.UserMigration); v != nil {
		cfg.UserMigration = v
	}
	if v := wire("define-auth", t.DefineAuthChallenge); v != nil {
		cfg.DefineAuthChallenge = v
	}
	if v := wire("create-auth", t.CreateAuthChallenge); v != nil {
		cfg.CreateAuthChallenge = v
	}
	if v := wire("verify-auth", t.VerifyAuthChallengeResponse); v != nil {
		cfg.VerifyAuthChallengeResponse = v
	}

	return cfg
}

// ── Accessors ─────────────────────────────────────────────────────────────────

// ID returns the Cognito User Pool ID.
func (c *CognitoUserPool) ID() pulumi.StringOutput { return c.pool.ID().ToStringOutput() }

// ARN returns the Cognito User Pool ARN.
func (c *CognitoUserPool) ARN() pulumi.StringOutput { return c.pool.Arn }

// ClientID returns the default app client ID.
func (c *CognitoUserPool) ClientID() pulumi.StringOutput { return c.client.ID().ToStringOutput() }

// Pool returns the underlying Cognito User Pool resource.
func (c *CognitoUserPool) Pool() *cognito.UserPool { return c.pool }

// Client returns the default app client resource.
func (c *CognitoUserPool) Client() *cognito.UserPoolClient { return c.client }

// ── IAM grant ─────────────────────────────────────────────────────────────────

// Grant attaches an inline IAM policy to the given role giving it cognito-idp:*
// on this User Pool. Called automatically when a Function links this construct.
func (c *CognitoUserPool) Grant(role *iam.Role) {
	pctx := c.ctx.Pulumi()
	policy := c.pool.Arn.ApplyT(func(arn string) (string, error) {
		doc := map[string]interface{}{
			"Version": "2012-10-17",
			"Statement": []map[string]interface{}{{
				"Effect":   "Allow",
				"Action":   "cognito-idp:*",
				"Resource": arn,
			}},
		}
		b, err := json.Marshal(doc)
		return string(b), err
	}).(pulumi.StringOutput)

	_, err := iam.NewRolePolicy(pctx, c.name+"-cognito-grant", &iam.RolePolicyArgs{
		Role:   role.Name,
		Policy: policy,
	})
	panicOnErr(err, c.name+": cognito-idp grant")
}

// ── Linkable ──────────────────────────────────────────────────────────────────

// LinkEnv implements forge.Linkable.
func (c *CognitoUserPool) LinkEnv() pulumi.StringMap {
	k := envKey(c.name)
	return pulumi.StringMap{
		"SST_COGNITO_" + k + "_USER_POOL_ID": c.ID(),
		"SST_COGNITO_" + k + "_CLIENT_ID":    c.ClientID(),
	}
}

// LinkName implements forge.Linkable.
func (c *CognitoUserPool) LinkName() string { return c.name }
