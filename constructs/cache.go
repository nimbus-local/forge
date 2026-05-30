package constructs

import (
	"fmt"
	"strconv"
	"strings"

	forge "github.com/nimbus-local/forge"
	"github.com/pulumi/pulumi-aws/sdk/v7/go/aws/elasticache"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

// Cache creates an ElastiCache ReplicationGroup (Redis or Valkey). It mirrors sst.aws.Redis.
//
// When linked to a Function the handler receives SST_CACHE_<NAME>_HOST, PORT, TLS, and
// AUTH_TOKEN. The cluster always enables at-rest and in-transit encryption.
type Cache struct {
	name      string
	cluster   *elasticache.ReplicationGroup
	authToken string
}

// CacheArgs configures the ElastiCache ReplicationGroup.
type CacheArgs struct {
	// Engine is the cache engine. Supported values: "redis" (default), "valkey".
	Engine string
	// EngineVersion is the cache engine version (e.g. "7.1"). Omit to let AWS select.
	// For Redis: default "7.1"; for Valkey: default "7.2".
	EngineVersion string
	// NodeType is the ElastiCache instance class without the "cache." prefix
	// (default: "t4g.micro"). Example: "m7g.xlarge".
	NodeType string
	// NumCacheClusters is the number of cache nodes (default: 1).
	NumCacheClusters int
	// AuthToken is the password for transit-encrypted connections (16–128 chars).
	// Required when TransitEncryptionMode is "required". Leave empty to allow
	// unauthenticated TLS connections (TransitEncryptionMode: "preferred").
	AuthToken string
	// SubnetIDs are the VPC subnet IDs for the cache subnet group. Required.
	SubnetIDs []string
	// VpcSecurityGroupIDs optionally restricts cluster access.
	VpcSecurityGroupIDs []string
	// Tags are merged with the default forge tags.
	Tags map[string]string
}

// NewCache creates an ElastiCache ReplicationGroup construct.
// args.SubnetIDs is required.
func NewCache(ctx *forge.RunContext, name string, args *CacheArgs) *Cache {
	if args == nil {
		args = &CacheArgs{}
	}
	if len(args.SubnetIDs) == 0 {
		panic(fmt.Sprintf("NewCache %q: SubnetIDs is required", name))
	}

	engine := args.Engine
	if engine == "" {
		engine = "redis"
	}
	engineVersion := args.EngineVersion
	if engineVersion == "" {
		if engine == "valkey" {
			engineVersion = "7.2"
		} else {
			engineVersion = "7.1"
		}
	}
	nodeType := args.NodeType
	if nodeType == "" {
		nodeType = "t4g.micro"
	}
	numClusters := args.NumCacheClusters
	if numClusters == 0 {
		numClusters = 1
	}

	tags := mergedTags(defaultTags(ctx, name), args.Tags)
	pctx := ctx.Pulumi()
	qname := strings.ToLower(qualifiedName(ctx, name))

	// Subnet group — required for VPC placement.
	subnetIDs := make(pulumi.StringArray, len(args.SubnetIDs))
	for i, id := range args.SubnetIDs {
		subnetIDs[i] = pulumi.String(id)
	}
	subnetGroup, err := elasticache.NewSubnetGroup(pctx, name+"-subnets", &elasticache.SubnetGroupArgs{
		Name:      pulumi.String(qname + "-subnets"),
		SubnetIds: subnetIDs,
		Tags:      tags,
	})
	panicOnErr(err, name+": cache subnet group")

	// Parameter group — sets cluster-enabled=no (non-sharded mode).
	family := cacheFamily(engine, engineVersion)
	paramGroup, err := elasticache.NewParameterGroup(pctx, name+"-params", &elasticache.ParameterGroupArgs{
		Name:   pulumi.String(qname + "-params"),
		Family: pulumi.String(family),
		Tags:   tags,
		Parameters: elasticache.ParameterGroupParameterArray{
			&elasticache.ParameterGroupParameterArgs{
				Name:  pulumi.String("cluster-enabled"),
				Value: pulumi.String("no"),
			},
		},
	})
	panicOnErr(err, name+": cache parameter group")

	// Security group IDs (optional).
	var sgIDs pulumi.StringArray
	for _, id := range args.VpcSecurityGroupIDs {
		sgIDs = append(sgIDs, pulumi.String(id))
	}

	clusterArgs := &elasticache.ReplicationGroupArgs{
		ReplicationGroupId:       pulumi.String(qname),
		Description:              pulumi.String("Managed by forge"),
		Engine:                   pulumi.String(engine),
		EngineVersion:            pulumi.String(engineVersion),
		NodeType:                 pulumi.String("cache." + nodeType),
		NumCacheClusters:         pulumi.Int(numClusters),
		Port:                     pulumi.Int(6379),
		SubnetGroupName:          subnetGroup.Name,
		ParameterGroupName:       paramGroup.Name,
		AtRestEncryptionEnabled:  pulumi.Bool(true),
		TransitEncryptionEnabled: pulumi.Bool(true),
		ApplyImmediately:         pulumi.Bool(true),
		AutoMinorVersionUpgrade:  pulumi.Bool(false),
		Tags:                     tags,
	}
	if len(sgIDs) > 0 {
		clusterArgs.SecurityGroupIds = sgIDs
	}
	if args.AuthToken != "" {
		clusterArgs.AuthToken = pulumi.String(args.AuthToken)
		clusterArgs.AuthTokenUpdateStrategy = pulumi.String("ROTATE")
		clusterArgs.TransitEncryptionMode = pulumi.String("required")
	} else {
		clusterArgs.TransitEncryptionMode = pulumi.String("preferred")
	}

	cluster, err := elasticache.NewReplicationGroup(pctx, name, clusterArgs)
	panicOnErr(err, name+": elasticache replication group")

	return &Cache{name: name, cluster: cluster, authToken: args.AuthToken}
}

// PrimaryEndpoint returns the primary endpoint address as a Pulumi output.
func (c *Cache) PrimaryEndpoint() pulumi.StringOutput { return c.cluster.PrimaryEndpointAddress }

// Port returns the cluster port as a Pulumi output.
func (c *Cache) Port() pulumi.IntPtrOutput { return c.cluster.Port }

// LinkEnv implements Linkable.
func (c *Cache) LinkEnv() pulumi.StringMap {
	key := envKey(c.name)
	portStr := c.cluster.Port.ApplyT(func(p *int) string {
		if p == nil {
			return "6379"
		}
		return strconv.Itoa(*p)
	}).(pulumi.StringOutput)

	return pulumi.StringMap{
		fmt.Sprintf("SST_CACHE_%s_HOST", key):       c.cluster.PrimaryEndpointAddress,
		fmt.Sprintf("SST_CACHE_%s_PORT", key):       portStr,
		fmt.Sprintf("SST_CACHE_%s_TLS", key):        pulumi.String("true"),
		fmt.Sprintf("SST_CACHE_%s_AUTH_TOKEN", key): pulumi.String(c.authToken),
	}
}

// LinkName implements Linkable.
func (c *Cache) LinkName() string { return c.name }

// cacheFamily derives the ElastiCache parameter group family from engine and version.
func cacheFamily(engine, version string) string {
	parts := strings.SplitN(version, ".", 2)
	major := parts[0]
	switch {
	case engine == "redis" && major == "6":
		return "redis6.x"
	case engine == "redis" && major == "5":
		return "redis5.0"
	case engine == "redis" && major == "4":
		return "redis4.0"
	default:
		return engine + major
	}
}
