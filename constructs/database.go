package constructs

import (
	"encoding/json"
	"fmt"
	"strconv"

	forge "github.com/nimbus-local/forge"
	"github.com/pulumi/pulumi-aws/sdk/v7/go/aws/iam"
	"github.com/pulumi/pulumi-aws/sdk/v7/go/aws/rds"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

// Database creates an Aurora Serverless v2 cluster. It mirrors sst.aws.Postgres.
//
// The cluster receives a writer instance (db.serverless). When linked to a Function
// the handler can connect using SST_DATABASE_<NAME>_HOST, PORT, NAME, USERNAME,
// SECRET_ARN, and CLUSTER_ARN. Use Grant to allow the function's role to fetch the
// password from Secrets Manager and call the Aurora Data API.
type Database struct {
	name           string
	cluster        *rds.Cluster
	hasMgdPassword bool // true when ManageMasterUserPassword is used
	ctx            *forge.RunContext
}

// DatabaseArgs configures the Aurora Serverless v2 cluster.
type DatabaseArgs struct {
	// Engine is the database engine. Supported values: "aurora-postgresql" (default),
	// "aurora-mysql".
	Engine string
	// EngineVersion is the engine version (e.g. "16.6"). Omit to let the provider
	// select the latest supported version.
	EngineVersion string
	// DatabaseName is the initial database name. Defaults to "app".
	DatabaseName string
	// MasterUsername defaults to "admin".
	MasterUsername string
	// MasterPassword sets the master password directly. If empty, AWS manages the
	// password automatically via Secrets Manager (ManageMasterUserPassword: true),
	// and SST_DATABASE_<NAME>_SECRET_ARN is populated with the managed secret ARN.
	MasterPassword string
	// SubnetIDs are the VPC subnet IDs for the DB subnet group. Required.
	SubnetIDs []string
	// VpcSecurityGroupIDs optionally restricts cluster access. If empty, the default
	// VPC security group is used.
	VpcSecurityGroupIDs []string
	// MinCapacity is the Aurora Serverless v2 minimum ACU (default: 0.5).
	MinCapacity float64
	// MaxCapacity is the Aurora Serverless v2 maximum ACU (default: 10).
	MaxCapacity float64
	// DeletionProtection prevents accidental deletion. Defaults to false.
	DeletionProtection bool
	// SkipFinalSnapshot skips the final snapshot on cluster deletion. Defaults to true.
	// Set to false in production to preserve a final backup.
	SkipFinalSnapshot *bool
	// Tags are merged with the default forge tags.
	Tags map[string]string
}

// NewDatabase creates an Aurora Serverless v2 cluster construct.
// args.SubnetIDs is required.
func NewDatabase(ctx *forge.RunContext, name string, args *DatabaseArgs) *Database {
	if args == nil {
		args = &DatabaseArgs{}
	}
	if len(args.SubnetIDs) == 0 {
		panic(fmt.Sprintf("NewDatabase %q: SubnetIDs is required", name))
	}

	engine := args.Engine
	if engine == "" {
		engine = "aurora-postgresql"
	}
	dbName := args.DatabaseName
	if dbName == "" {
		dbName = "app"
	}
	masterUser := args.MasterUsername
	if masterUser == "" {
		masterUser = "admin"
	}
	minACU := args.MinCapacity
	if minACU == 0 {
		minACU = 0.5
	}
	maxACU := args.MaxCapacity
	if maxACU == 0 {
		maxACU = 10
	}
	skipSnapshot := true
	if args.SkipFinalSnapshot != nil {
		skipSnapshot = *args.SkipFinalSnapshot
	}

	tags := mergedTags(defaultTags(ctx, name), args.Tags)
	pctx := ctx.Pulumi()

	// Subnet group — required for cluster placement.
	subnetIDs := make(pulumi.StringArray, len(args.SubnetIDs))
	for i, id := range args.SubnetIDs {
		subnetIDs[i] = pulumi.String(id)
	}
	subnetGroup, err := rds.NewSubnetGroup(pctx, name+"-subnets", &rds.SubnetGroupArgs{
		Name:      pulumi.String(qualifiedName(ctx, name) + "-subnets"),
		SubnetIds: subnetIDs,
		Tags:      tags,
	})
	panicOnErr(err, name+": db subnet group")

	// Security group IDs (optional).
	var sgIDs pulumi.StringArray
	for _, id := range args.VpcSecurityGroupIDs {
		sgIDs = append(sgIDs, pulumi.String(id))
	}

	clusterArgs := &rds.ClusterArgs{
		ClusterIdentifier:  pulumi.String(qualifiedName(ctx, name)),
		Engine:             pulumi.String(engine),
		DatabaseName:       pulumi.String(dbName),
		MasterUsername:     pulumi.String(masterUser),
		DbSubnetGroupName:  subnetGroup.Name,
		DeletionProtection: pulumi.Bool(args.DeletionProtection),
		SkipFinalSnapshot:  pulumi.Bool(skipSnapshot),
		Serverlessv2ScalingConfiguration: &rds.ClusterServerlessv2ScalingConfigurationArgs{
			MinCapacity: pulumi.Float64(minACU),
			MaxCapacity: pulumi.Float64(maxACU),
		},
		Tags: tags,
	}
	if args.EngineVersion != "" {
		clusterArgs.EngineVersion = pulumi.String(args.EngineVersion)
	}
	if len(sgIDs) > 0 {
		clusterArgs.VpcSecurityGroupIds = sgIDs
	}

	hasMgd := args.MasterPassword == ""
	if hasMgd {
		clusterArgs.ManageMasterUserPassword = pulumi.Bool(true)
	} else {
		clusterArgs.MasterPassword = pulumi.String(args.MasterPassword)
	}

	cluster, err := rds.NewCluster(pctx, name, clusterArgs)
	panicOnErr(err, name+": rds cluster")

	// Writer instance — db.serverless is required for Aurora Serverless v2.
	_, err = rds.NewClusterInstance(pctx, name+"-writer", &rds.ClusterInstanceArgs{
		Identifier:        pulumi.String(qualifiedName(ctx, name) + "-writer"),
		ClusterIdentifier: cluster.ClusterIdentifier,
		Engine:            rds.EngineType(engine),
		InstanceClass:     pulumi.String("db.serverless"),
		DbSubnetGroupName: subnetGroup.Name,
	})
	panicOnErr(err, name+": cluster instance")

	return &Database{name: name, cluster: cluster, hasMgdPassword: hasMgd, ctx: ctx}
}

// Endpoint returns the cluster writer endpoint as a Pulumi output.
func (d *Database) Endpoint() pulumi.StringOutput { return d.cluster.Endpoint }

// Port returns the cluster port as a Pulumi output.
func (d *Database) Port() pulumi.IntOutput { return d.cluster.Port }

// ClusterARN returns the cluster ARN as a Pulumi output.
func (d *Database) ClusterARN() pulumi.StringOutput { return d.cluster.Arn }

// Grant adds IAM policies to role granting secretsmanager:GetSecretValue (to fetch
// the database password) and rds-data:* (for the Aurora Data API).
func (d *Database) Grant(role *iam.Role) {
	pctx := d.ctx.Pulumi()

	policy := d.cluster.Arn.ApplyT(func(arn string) (string, error) {
		doc := map[string]interface{}{
			"Version": "2012-10-17",
			"Statement": []map[string]interface{}{
				{
					"Effect":   "Allow",
					"Action":   "secretsmanager:GetSecretValue",
					"Resource": "*",
				},
				{
					"Effect":   "Allow",
					"Action":   "rds-data:*",
					"Resource": arn,
				},
			},
		}
		b, err := json.Marshal(doc)
		return string(b), err
	}).(pulumi.StringOutput)

	_, err := iam.NewRolePolicy(pctx, d.name+"-db-grant", &iam.RolePolicyArgs{
		Role:   role.Name,
		Policy: policy,
	})
	panicOnErr(err, d.name+": db grant policy")
}

// LinkEnv implements Linkable.
func (d *Database) LinkEnv() pulumi.StringMap {
	key := envKey(d.name)
	portStr := d.cluster.Port.ApplyT(func(p int) string {
		return strconv.Itoa(p)
	}).(pulumi.StringOutput)

	var secretArn pulumi.StringOutput
	if d.hasMgdPassword {
		secretArn = d.cluster.MasterUserSecrets.ApplyT(func(secrets []rds.ClusterMasterUserSecret) string {
			if len(secrets) == 0 || secrets[0].SecretArn == nil {
				return ""
			}
			return *secrets[0].SecretArn
		}).(pulumi.StringOutput)
	} else {
		secretArn = pulumi.String("").ToStringOutput()
	}

	return pulumi.StringMap{
		fmt.Sprintf("SST_DATABASE_%s_HOST", key):        d.cluster.Endpoint,
		fmt.Sprintf("SST_DATABASE_%s_PORT", key):        portStr,
		fmt.Sprintf("SST_DATABASE_%s_NAME", key):        d.cluster.DatabaseName,
		fmt.Sprintf("SST_DATABASE_%s_USERNAME", key):    d.cluster.MasterUsername,
		fmt.Sprintf("SST_DATABASE_%s_SECRET_ARN", key):  secretArn,
		fmt.Sprintf("SST_DATABASE_%s_CLUSTER_ARN", key): d.cluster.Arn,
	}
}

// LinkName implements Linkable.
func (d *Database) LinkName() string { return d.name }
