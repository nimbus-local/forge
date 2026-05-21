package constructs

import (
	"fmt"

	forge "github.com/sst-go/forge"
	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/dynamodb"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

// FieldType maps to DynamoDB attribute types.
type FieldType string

const (
	FieldTypeString FieldType = "S"
	FieldTypeNumber FieldType = "N"
	FieldTypeBinary FieldType = "B"
)

// PrimaryIndex defines the table's main partition + optional sort key.
type PrimaryIndex struct {
	HashKey  string
	RangeKey string // optional
}

// GlobalIndex defines a GSI.
type GlobalIndex struct {
	Name       string
	HashKey    string
	RangeKey   string // optional
	Projection string // "ALL" | "KEYS_ONLY" — defaults to "ALL"
}

// DynamoDBArgs mirrors sst.aws.DynamoDB args.
type DynamoDBArgs struct {
	// Fields defines every attribute used in keys or indexes.
	Fields map[string]FieldType
	// PrimaryIndex is the table's partition + sort key.
	PrimaryIndex *PrimaryIndex
	// GlobalIndexes is an optional list of GSIs.
	GlobalIndexes []GlobalIndex
	// BillingMode defaults to PAY_PER_REQUEST.
	BillingMode string
	// PointInTimeRecovery enables PITR (recommended for production).
	PointInTimeRecovery bool
	// DeletionProtection prevents accidental table deletion.
	DeletionProtection bool
	// StreamEnabled enables DynamoDB Streams.
	StreamEnabled bool
	// StreamViewType defaults to "NEW_AND_OLD_IMAGES".
	StreamViewType string
}

// DynamoDB is a DynamoDB table construct.
type DynamoDB struct {
	name     string
	resource *dynamodb.Table
	ctx      *forge.RunContext
}

// NewDynamoDB creates a DynamoDB table construct.
func NewDynamoDB(ctx *forge.RunContext, name string, args *DynamoDBArgs) *DynamoDB {
	if args == nil {
		args = &DynamoDBArgs{}
	}
	if args.BillingMode == "" {
		args.BillingMode = "PAY_PER_REQUEST"
	}
	if args.PrimaryIndex == nil {
		panic("forge: DynamoDBArgs.PrimaryIndex must not be nil for " + name)
	}

	pctx := ctx.Pulumi()

	// Build attribute definitions from Fields map.
	attrs := dynamodb.TableAttributeArray{}
	for fieldName, fieldType := range args.Fields {
		attrs = append(attrs, &dynamodb.TableAttributeArgs{
			Name: pulumi.String(fieldName),
			Type: pulumi.String(string(fieldType)),
		})
	}

	tableArgs := &dynamodb.TableArgs{
		Name:           pulumi.String(qualifiedName(ctx, name)),
		BillingMode:    pulumi.String(args.BillingMode),
		Attributes:     attrs,
		HashKey:        pulumi.String(args.PrimaryIndex.HashKey),
		DeletionProtectionEnabled: pulumi.Bool(args.DeletionProtection),
		Tags:           defaultTags(ctx, name),
	}

	if args.PrimaryIndex.RangeKey != "" {
		tableArgs.RangeKey = pulumi.String(args.PrimaryIndex.RangeKey)
	}

	if args.PointInTimeRecovery {
		tableArgs.PointInTimeRecovery = &dynamodb.TablePointInTimeRecoveryArgs{
			Enabled: pulumi.Bool(true),
		}
	}

	if args.StreamEnabled {
		viewType := args.StreamViewType
		if viewType == "" {
			viewType = "NEW_AND_OLD_IMAGES"
		}
		tableArgs.StreamEnabled = pulumi.Bool(true)
		tableArgs.StreamViewType = pulumi.String(viewType)
	}

	// GSIs
	gsis := dynamodb.TableGlobalSecondaryIndexArray{}
	for _, gi := range args.GlobalIndexes {
		proj := gi.Projection
		if proj == "" {
			proj = "ALL"
		}
		gsi := &dynamodb.TableGlobalSecondaryIndexArgs{
			Name:           pulumi.String(gi.Name),
			HashKey:        pulumi.String(gi.HashKey),
			ProjectionType: pulumi.String(proj),
		}
		if gi.RangeKey != "" {
			gsi.RangeKey = pulumi.String(gi.RangeKey)
		}
		gsis = append(gsis, gsi)
	}
	if len(gsis) > 0 {
		tableArgs.GlobalSecondaryIndexes = gsis
	}

	table, err := dynamodb.NewTable(pctx, name, tableArgs)
	panicOnErr(err, name+": dynamodb table")

	return &DynamoDB{name: name, resource: table, ctx: ctx}
}

// TableName returns the physical table name as a Pulumi output.
func (d *DynamoDB) TableName() pulumi.StringOutput { return d.resource.Name }

// ARN returns the table ARN as a Pulumi output.
func (d *DynamoDB) ARN() pulumi.StringOutput { return d.resource.Arn }

// StreamARN returns the DynamoDB Stream ARN (empty if streams not enabled).
func (d *DynamoDB) StreamARN() pulumi.StringOutput { return d.resource.StreamArn }

// linkEnv implements Linkable — injects the table name and ARN into linked Lambdas.
func (d *DynamoDB) linkEnv() pulumi.StringMap {
	key := envKey(d.name)
	return pulumi.StringMap{
		fmt.Sprintf("SST_TABLE_%s_NAME", key): d.resource.Name,
		fmt.Sprintf("SST_TABLE_%s_ARN", key):  d.resource.Arn,
	}
}
func (d *DynamoDB) linkName() string { return d.name }
