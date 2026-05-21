package cloudflare

import (
	"fmt"

	cf "github.com/pulumi/pulumi-cloudflare/sdk/v5/go/cloudflare"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	forge "github.com/sst-go/forge"
)

// KVNamespaceArgs configures a Cloudflare Workers KV namespace construct.
type KVNamespaceArgs struct{}

// KVNamespace is a Cloudflare Workers KV namespace construct.
type KVNamespace struct {
	name     string
	resource *cf.WorkersKvNamespace
	ctx      *forge.RunContext
}

// NewKVNamespace creates a Cloudflare Workers KV namespace.
func NewKVNamespace(ctx *forge.RunContext, name string, args *KVNamespaceArgs) *KVNamespace {
	pctx := ctx.Pulumi()

	ns, err := cf.NewWorkersKvNamespace(pctx, name, &cf.WorkersKvNamespaceArgs{
		AccountId: pulumi.String(accountID(ctx)),
		Title:     pulumi.String(qualifiedName(ctx, name)),
	})
	panicOnErr(err, name+": kv namespace")

	return &KVNamespace{name: name, resource: ns, ctx: ctx}
}

// ID returns the KV namespace ID as a Pulumi output.
func (k *KVNamespace) ID() pulumi.IDOutput { return k.resource.ID() }

// Title returns the KV namespace title as a Pulumi output.
func (k *KVNamespace) Title() pulumi.StringOutput { return k.resource.Title }

// LinkEnv implements forge.Linkable — injects the namespace ID and title into linked Lambdas.
func (k *KVNamespace) LinkEnv() pulumi.StringMap {
	key := envKey(k.name)
	return pulumi.StringMap{
		fmt.Sprintf("SST_KV_%s_ID", key):   k.resource.ID().ToStringOutput(),
		fmt.Sprintf("SST_KV_%s_NAME", key): k.resource.Title,
	}
}
func (k *KVNamespace) LinkName() string { return k.name }
