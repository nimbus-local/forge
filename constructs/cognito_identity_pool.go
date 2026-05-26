package constructs

import (
	"encoding/json"
	"fmt"

	forge "github.com/nimbus-local/forge"
	aws "github.com/pulumi/pulumi-aws/sdk/v7/go/aws"
	"github.com/pulumi/pulumi-aws/sdk/v7/go/aws/cognito"
	"github.com/pulumi/pulumi-aws/sdk/v7/go/aws/iam"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

// CognitoIdentityPool creates an Amazon Cognito Identity Pool that federates
// Cognito User Pools and other identity providers into temporary AWS credentials.
//
// Two IAM roles are created automatically:
//   - Authenticated role: for users who have signed in via a linked provider
//   - Unauthenticated role: for guest users (only when AllowUnauthenticated is true)
//
// LinkEnv keys injected into linked Functions:
//
//	SST_IDENTITY_POOL_<NAME>_ID — Cognito Identity Pool ID
//
// Linked Functions automatically receive cognito-identity:* permissions on the pool.
type CognitoIdentityPool struct {
	name       string
	pool       *cognito.IdentityPool
	authRole   *iam.Role
	unauthRole *iam.Role
	ctx        *forge.RunContext
}

// UserPoolProvider links a Cognito User Pool + client to this identity pool.
type UserPoolProvider struct {
	// UserPool is the CognitoUserPool construct to federate.
	UserPool *CognitoUserPool
}

// CognitoIdentityPoolArgs configures a CognitoIdentityPool construct.
type CognitoIdentityPoolArgs struct {
	// UserPools links one or more Cognito User Pools as identity providers.
	UserPools []UserPoolProvider

	// AllowUnauthenticated allows guest (unauthenticated) access.
	// Defaults to false.
	AllowUnauthenticated bool

	// Tags merged with stage-level tags on every resource.
	Tags map[string]string
}

// NewCognitoIdentityPool creates a Cognito Identity Pool with authenticated and
// (optionally) unauthenticated IAM roles.
func NewCognitoIdentityPool(ctx *forge.RunContext, name string, args *CognitoIdentityPoolArgs) *CognitoIdentityPool {
	if args == nil {
		args = &CognitoIdentityPoolArgs{}
	}

	pctx := ctx.Pulumi()
	tags := mergedTags(defaultTags(ctx, name), args.Tags)

	// ── Identity Pool ─────────────────────────────────────────────────────────
	poolArgs := &cognito.IdentityPoolArgs{
		IdentityPoolName:               pulumi.String(qualifiedName(ctx, name)),
		AllowUnauthenticatedIdentities: pulumi.Bool(args.AllowUnauthenticated),
		Tags:                           tags,
	}

	if len(args.UserPools) > 0 {
		region := aws.GetRegionOutput(pctx, aws.GetRegionOutputArgs{}).Name()
		providers := make(cognito.IdentityPoolCognitoIdentityProviderArray, len(args.UserPools))
		for i, up := range args.UserPools {
			providers[i] = &cognito.IdentityPoolCognitoIdentityProviderArgs{
				ClientId: up.UserPool.ClientID(),
				ProviderName: pulumi.Sprintf(
					"cognito-idp.%s.amazonaws.com/%s",
					region,
					up.UserPool.ID(),
				),
				ServerSideTokenCheck: pulumi.Bool(false),
			}
		}
		poolArgs.CognitoIdentityProviders = providers
	}

	pool, err := cognito.NewIdentityPool(pctx, name+"-pool", poolArgs)
	panicOnErr(err, name+": identity pool")

	// ── Authenticated IAM role ────────────────────────────────────────────────
	authAssumePolicy := pool.ID().ApplyT(func(id string) (string, error) {
		doc := map[string]interface{}{
			"Version": "2012-10-17",
			"Statement": []map[string]interface{}{{
				"Effect": "Allow",
				"Principal": map[string]string{
					"Federated": "cognito-identity.amazonaws.com",
				},
				"Action": "sts:AssumeRoleWithWebIdentity",
				"Condition": map[string]interface{}{
					"StringEquals": map[string]string{
						"cognito-identity.amazonaws.com:aud": id,
					},
					"ForAnyValue:StringLike": map[string]string{
						"cognito-identity.amazonaws.com:amr": "authenticated",
					},
				},
			}},
		}
		b, err := json.Marshal(doc)
		return string(b), err
	}).(pulumi.StringOutput)

	authRole, err := iam.NewRole(pctx, name+"-auth-role", &iam.RoleArgs{
		AssumeRolePolicy: authAssumePolicy,
		Tags:             tags,
	})
	panicOnErr(err, name+": authenticated role")

	// ── Unauthenticated IAM role ──────────────────────────────────────────────
	var unauthRole *iam.Role
	if args.AllowUnauthenticated {
		unauthAssumePolicy := pool.ID().ApplyT(func(id string) (string, error) {
			doc := map[string]interface{}{
				"Version": "2012-10-17",
				"Statement": []map[string]interface{}{{
					"Effect": "Allow",
					"Principal": map[string]string{
						"Federated": "cognito-identity.amazonaws.com",
					},
					"Action": "sts:AssumeRoleWithWebIdentity",
					"Condition": map[string]interface{}{
						"StringEquals": map[string]string{
							"cognito-identity.amazonaws.com:aud": id,
						},
						"ForAnyValue:StringLike": map[string]string{
							"cognito-identity.amazonaws.com:amr": "unauthenticated",
						},
					},
				}},
			}
			b, err := json.Marshal(doc)
			return string(b), err
		}).(pulumi.StringOutput)

		unauthRole, err = iam.NewRole(pctx, name+"-unauth-role", &iam.RoleArgs{
			AssumeRolePolicy: unauthAssumePolicy,
			Tags:             tags,
		})
		panicOnErr(err, name+": unauthenticated role")
	}

	// ── Role attachment ───────────────────────────────────────────────────────
	roles := pulumi.StringMap{
		"authenticated": authRole.Arn,
	}
	if unauthRole != nil {
		roles["unauthenticated"] = unauthRole.Arn
	}
	_, err = cognito.NewIdentityPoolRoleAttachment(pctx, name+"-roles", &cognito.IdentityPoolRoleAttachmentArgs{
		IdentityPoolId: pool.ID(),
		Roles:          roles,
	})
	panicOnErr(err, name+": role attachment")

	return &CognitoIdentityPool{
		name:       name,
		pool:       pool,
		authRole:   authRole,
		unauthRole: unauthRole,
		ctx:        ctx,
	}
}

// ── Accessors ─────────────────────────────────────────────────────────────────

// ID returns the Cognito Identity Pool ID.
func (c *CognitoIdentityPool) ID() pulumi.StringOutput { return c.pool.ID().ToStringOutput() }

// Pool returns the underlying Cognito Identity Pool resource.
func (c *CognitoIdentityPool) Pool() *cognito.IdentityPool { return c.pool }

// AuthenticatedRole returns the IAM role assumed by authenticated users.
func (c *CognitoIdentityPool) AuthenticatedRole() *iam.Role { return c.authRole }

// UnauthenticatedRole returns the IAM role assumed by unauthenticated (guest) users,
// or nil if AllowUnauthenticated was false.
func (c *CognitoIdentityPool) UnauthenticatedRole() *iam.Role { return c.unauthRole }

// ── IAM grant ─────────────────────────────────────────────────────────────────

// Grant attaches an inline IAM policy giving the role cognito-identity:* on this pool.
func (c *CognitoIdentityPool) Grant(role *iam.Role) {
	pctx := c.ctx.Pulumi()
	policy := c.pool.Arn.ApplyT(func(arn string) (string, error) {
		doc := map[string]interface{}{
			"Version": "2012-10-17",
			"Statement": []map[string]interface{}{{
				"Effect":   "Allow",
				"Action":   "cognito-identity:*",
				"Resource": arn,
			}},
		}
		b, err := json.Marshal(doc)
		return string(b), err
	}).(pulumi.StringOutput)

	_, err := iam.NewRolePolicy(pctx, fmt.Sprintf("%s-identity-grant", c.name), &iam.RolePolicyArgs{
		Role:   role.Name,
		Policy: policy,
	})
	panicOnErr(err, c.name+": cognito-identity grant")
}

// ── Linkable ──────────────────────────────────────────────────────────────────

// LinkEnv implements forge.Linkable.
func (c *CognitoIdentityPool) LinkEnv() pulumi.StringMap {
	k := envKey(c.name)
	return pulumi.StringMap{
		"SST_IDENTITY_POOL_" + k + "_ID": c.ID(),
	}
}

// LinkName implements forge.Linkable.
func (c *CognitoIdentityPool) LinkName() string { return c.name }
