package cloudflare

import (
	"fmt"

	cf "github.com/pulumi/pulumi-cloudflare/sdk/v5/go/cloudflare"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	forge "github.com/sst-go/forge"
)

// D1DatabaseArgs configures a Cloudflare D1 database construct.
type D1DatabaseArgs struct{}

// D1Database is a Cloudflare D1 SQLite database construct.
type D1Database struct {
	name     string
	resource *cf.D1Database
	ctx      *forge.RunContext
}

// NewD1Database creates a Cloudflare D1 database.
func NewD1Database(ctx *forge.RunContext, name string, args *D1DatabaseArgs) *D1Database {
	pctx := ctx.Pulumi()

	db, err := cf.NewD1Database(pctx, name, &cf.D1DatabaseArgs{
		AccountId: pulumi.String(accountID(ctx)),
		Name:      pulumi.String(qualifiedName(ctx, name)),
	})
	panicOnErr(err, name+": d1 database")

	return &D1Database{name: name, resource: db, ctx: ctx}
}

// ID returns the D1 database UUID as a Pulumi output.
func (d *D1Database) ID() pulumi.IDOutput { return d.resource.ID() }

// Name returns the physical D1 database name as a Pulumi output.
func (d *D1Database) Name() pulumi.StringOutput { return d.resource.Name }

// LinkEnv implements forge.Linkable — injects the database ID and name into linked Lambdas.
func (d *D1Database) LinkEnv() pulumi.StringMap {
	key := envKey(d.name)
	return pulumi.StringMap{
		fmt.Sprintf("SST_D1_%s_ID", key):   d.resource.ID().ToStringOutput(),
		fmt.Sprintf("SST_D1_%s_NAME", key): d.resource.Name,
	}
}
func (d *D1Database) LinkName() string { return d.name }
