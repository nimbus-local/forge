package constructs

import (
	"fmt"

	forge "github.com/nimbus-local/forge"
	"github.com/pulumi/pulumi-aws/sdk/v7/go/aws/kms"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

// KMSKeyArgs configures a KMS key construct.
type KMSKeyArgs struct {
	// Description is a human-readable description of the key's purpose.
	Description string
	// DisableRotation disables automatic annual key rotation. Rotation is enabled by default.
	DisableRotation bool
	// DeletionWindowInDays is the waiting period before key deletion (7–30). Defaults to 30.
	DeletionWindowInDays int
}

// KMSKey is a managed KMS symmetric key that can be attached to any construct
// requiring encryption at rest.
type KMSKey struct {
	name     string
	resource *kms.Key
	ctx      *forge.RunContext
}

// NewKMSKey creates a KMS symmetric key with automatic annual rotation enabled by default.
func NewKMSKey(ctx *forge.RunContext, name string, args *KMSKeyArgs) *KMSKey {
	if args == nil {
		args = &KMSKeyArgs{}
	}
	if args.DeletionWindowInDays == 0 {
		args.DeletionWindowInDays = 30
	}

	pctx := ctx.Pulumi()

	desc := args.Description
	if desc == "" {
		desc = fmt.Sprintf("forge %s/%s %s", ctx.App.Name, ctx.Stage, name)
	}

	key, err := kms.NewKey(pctx, name, &kms.KeyArgs{
		Description:          pulumi.String(desc),
		EnableKeyRotation:    pulumi.Bool(!args.DisableRotation),
		DeletionWindowInDays: pulumi.Int(args.DeletionWindowInDays),
		Tags:                 defaultTags(ctx, name),
	})
	panicOnErr(err, name+": kms key")

	_, err = kms.NewAlias(pctx, name+"-alias", &kms.AliasArgs{
		Name:        pulumi.Sprintf("alias/%s", qualifiedName(ctx, name)),
		TargetKeyId: key.ID(),
	})
	panicOnErr(err, name+": kms alias")

	return &KMSKey{name: name, resource: key, ctx: ctx}
}

// ARN returns the KMS key ARN as a Pulumi output.
func (k *KMSKey) ARN() pulumi.StringOutput { return k.resource.Arn }

// ID returns the KMS key ID as a Pulumi output.
func (k *KMSKey) ID() pulumi.StringOutput { return k.resource.KeyId }

// LinkEnv implements Linkable — injects the key ARN and ID into linked Lambdas.
func (k *KMSKey) LinkEnv() pulumi.StringMap {
	key := envKey(k.name)
	return pulumi.StringMap{
		fmt.Sprintf("SST_KMS_%s_ARN", key): k.resource.Arn,
		fmt.Sprintf("SST_KMS_%s_ID", key):  k.resource.KeyId,
	}
}

// LinkName implements Linkable.
func (k *KMSKey) LinkName() string { return k.name }

// kmsGrant creates a KMS grant giving an IAM principal the permissions needed to
// use a customer-managed key for encryption/decryption.
func kmsGrant(pctx *pulumi.Context, resourceName string, keyArn pulumi.StringInput, principalArn pulumi.StringInput) {
	_, err := kms.NewGrant(pctx, resourceName+"-kms-grant", &kms.GrantArgs{
		KeyId:            keyArn,
		GranteePrincipal: principalArn,
		Operations: pulumi.StringArray{
			pulumi.String("GenerateDataKey"),
			pulumi.String("GenerateDataKeyWithoutPlaintext"),
			pulumi.String("Decrypt"),
			pulumi.String("DescribeKey"),
		},
	})
	panicOnErr(err, resourceName+": kms grant")
}
