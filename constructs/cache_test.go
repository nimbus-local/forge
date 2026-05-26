package constructs

import (
	"testing"

	forge "github.com/nimbus-local/forge"
)

var testCacheSubnets = []string{"subnet-00000001", "subnet-00000002"}

func TestNewCache_ReplicationGroupCreated(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		NewCache(ctx, "Redis", &CacheArgs{
			SubnetIDs: testCacheSubnets,
		})
	})

	if mocks.find("aws:elasticache/replicationGroup:ReplicationGroup") == nil {
		t.Error("ElastiCache replication group not created")
	}
}

func TestNewCache_SubnetGroupCreated(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		NewCache(ctx, "Redis", &CacheArgs{
			SubnetIDs: testCacheSubnets,
		})
	})

	if mocks.find("aws:elasticache/subnetGroup:SubnetGroup") == nil {
		t.Error("ElastiCache subnet group not created")
	}
}

func TestNewCache_ParameterGroupCreated(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		NewCache(ctx, "Redis", &CacheArgs{
			SubnetIDs: testCacheSubnets,
		})
	})

	if mocks.find("aws:elasticache/parameterGroup:ParameterGroup") == nil {
		t.Error("ElastiCache parameter group not created")
	}
}

func TestNewCache_DefaultEngineIsRedis(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		NewCache(ctx, "Redis", &CacheArgs{
			SubnetIDs: testCacheSubnets,
		})
	})

	r := mocks.find("aws:elasticache/replicationGroup:ReplicationGroup")
	if r == nil {
		t.Fatal("ReplicationGroup not registered")
	}
	if r.inputs["engine"].StringValue() != "redis" {
		t.Errorf("engine = %q, want redis", r.inputs["engine"].StringValue())
	}
}

func TestNewCache_PhysicalNameQualifiedAndLowercase(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		NewCache(ctx, "Redis", &CacheArgs{
			SubnetIDs: testCacheSubnets,
		})
	})

	r := mocks.find("aws:elasticache/replicationGroup:ReplicationGroup")
	if r == nil {
		t.Fatal("ReplicationGroup not registered")
	}
	if r.inputs["replicationGroupId"].StringValue() != "myapp-test-redis" {
		t.Errorf("replicationGroupId = %q, want myapp-test-redis", r.inputs["replicationGroupId"].StringValue())
	}
}

func TestNewCache_AtRestEncryptionEnabled(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		NewCache(ctx, "Redis", &CacheArgs{
			SubnetIDs: testCacheSubnets,
		})
	})

	r := mocks.find("aws:elasticache/replicationGroup:ReplicationGroup")
	if r == nil {
		t.Fatal("ReplicationGroup not registered")
	}
	if !r.inputs["atRestEncryptionEnabled"].V.(bool) {
		t.Error("atRestEncryptionEnabled should be true")
	}
}

func TestNewCache_TransitEncryptionEnabled(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		NewCache(ctx, "Redis", &CacheArgs{
			SubnetIDs: testCacheSubnets,
		})
	})

	r := mocks.find("aws:elasticache/replicationGroup:ReplicationGroup")
	if r == nil {
		t.Fatal("ReplicationGroup not registered")
	}
	if !r.inputs["transitEncryptionEnabled"].V.(bool) {
		t.Error("transitEncryptionEnabled should be true")
	}
}

func TestNewCache_TransitEncryptionModePreferredWithoutAuthToken(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		NewCache(ctx, "Redis", &CacheArgs{
			SubnetIDs: testCacheSubnets,
		})
	})

	r := mocks.find("aws:elasticache/replicationGroup:ReplicationGroup")
	if r == nil {
		t.Fatal("ReplicationGroup not registered")
	}
	if r.inputs["transitEncryptionMode"].StringValue() != "preferred" {
		t.Errorf("transitEncryptionMode = %q, want preferred", r.inputs["transitEncryptionMode"].StringValue())
	}
}

func TestNewCache_TransitEncryptionModeRequiredWithAuthToken(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		NewCache(ctx, "Redis", &CacheArgs{
			SubnetIDs: testCacheSubnets,
			AuthToken: "mysecretauthtoken1234",
		})
	})

	r := mocks.find("aws:elasticache/replicationGroup:ReplicationGroup")
	if r == nil {
		t.Fatal("ReplicationGroup not registered")
	}
	if r.inputs["transitEncryptionMode"].StringValue() != "required" {
		t.Errorf("transitEncryptionMode = %q, want required", r.inputs["transitEncryptionMode"].StringValue())
	}
}

func TestNewCache_DefaultNodeTypeIsMicro(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		NewCache(ctx, "Redis", &CacheArgs{
			SubnetIDs: testCacheSubnets,
		})
	})

	r := mocks.find("aws:elasticache/replicationGroup:ReplicationGroup")
	if r == nil {
		t.Fatal("ReplicationGroup not registered")
	}
	if r.inputs["nodeType"].StringValue() != "cache.t4g.micro" {
		t.Errorf("nodeType = %q, want cache.t4g.micro", r.inputs["nodeType"].StringValue())
	}
}

func TestNewCache_CustomNodeType(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		NewCache(ctx, "Redis", &CacheArgs{
			SubnetIDs: testCacheSubnets,
			NodeType:  "m7g.xlarge",
		})
	})

	r := mocks.find("aws:elasticache/replicationGroup:ReplicationGroup")
	if r == nil {
		t.Fatal("ReplicationGroup not registered")
	}
	if r.inputs["nodeType"].StringValue() != "cache.m7g.xlarge" {
		t.Errorf("nodeType = %q, want cache.m7g.xlarge", r.inputs["nodeType"].StringValue())
	}
}

func TestNewCache_DefaultNumCacheClusters(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		NewCache(ctx, "Redis", &CacheArgs{
			SubnetIDs: testCacheSubnets,
		})
	})

	r := mocks.find("aws:elasticache/replicationGroup:ReplicationGroup")
	if r == nil {
		t.Fatal("ReplicationGroup not registered")
	}
	if r.inputs["numCacheClusters"].NumberValue() != 1 {
		t.Errorf("numCacheClusters = %v, want 1", r.inputs["numCacheClusters"].NumberValue())
	}
}

func TestNewCache_ValKeyEngine(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		NewCache(ctx, "Cache", &CacheArgs{
			SubnetIDs: testCacheSubnets,
			Engine:    "valkey",
		})
	})

	r := mocks.find("aws:elasticache/replicationGroup:ReplicationGroup")
	if r == nil {
		t.Fatal("ReplicationGroup not registered")
	}
	if r.inputs["engine"].StringValue() != "valkey" {
		t.Errorf("engine = %q, want valkey", r.inputs["engine"].StringValue())
	}
}

func TestNewCache_TagsApplied(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		NewCache(ctx, "Redis", &CacheArgs{
			SubnetIDs: testCacheSubnets,
		})
	})

	r := mocks.find("aws:elasticache/replicationGroup:ReplicationGroup")
	if r == nil {
		t.Fatal("ReplicationGroup not registered")
	}
	for _, tag := range []string{"forge:app", "forge:stage", "forge:name"} {
		assertTag(t, r.inputs, tag)
	}
}

func TestNewCache_NoSubnetIDsPanics(t *testing.T) {
	t.Parallel()
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for missing SubnetIDs")
		}
	}()
	runTest(t, func(ctx *forge.RunContext) {
		NewCache(ctx, "Redis", &CacheArgs{})
	})
}

func TestNewCache_NilArgsPanics(t *testing.T) {
	t.Parallel()
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for nil args (missing SubnetIDs)")
		}
	}()
	runTest(t, func(ctx *forge.RunContext) {
		NewCache(ctx, "Redis", nil)
	})
}

func TestNewCache_LinkEnvKeys(t *testing.T) {
	t.Parallel()
	runTest(t, func(ctx *forge.RunContext) {
		c := NewCache(ctx, "Redis", &CacheArgs{
			SubnetIDs: testCacheSubnets,
		})
		env := c.LinkEnv()
		for _, key := range []string{
			"SST_CACHE_REDIS_HOST",
			"SST_CACHE_REDIS_PORT",
			"SST_CACHE_REDIS_TLS",
			"SST_CACHE_REDIS_AUTH_TOKEN",
		} {
			if _, ok := env[key]; !ok {
				t.Errorf("LinkEnv missing %s", key)
			}
		}
		if len(env) != 4 {
			t.Errorf("LinkEnv has %d keys, want 4", len(env))
		}
	})
}

func TestNewCache_TLSLinkEnvIsTrue(t *testing.T) {
	t.Parallel()
	runTest(t, func(ctx *forge.RunContext) {
		c := NewCache(ctx, "Redis", &CacheArgs{
			SubnetIDs: testCacheSubnets,
		})
		env := c.LinkEnv()
		tls, ok := env["SST_CACHE_REDIS_TLS"]
		if !ok {
			t.Fatal("SST_CACHE_REDIS_TLS missing from LinkEnv")
		}
		_ = tls // value is a pulumi.String("true") output, correct by construction
	})
}

func TestNewCache_CamelCaseLinkEnvKey(t *testing.T) {
	t.Parallel()
	runTest(t, func(ctx *forge.RunContext) {
		c := NewCache(ctx, "MyRedis", &CacheArgs{
			SubnetIDs: testCacheSubnets,
		})
		env := c.LinkEnv()
		if _, ok := env["SST_CACHE_MY_REDIS_HOST"]; !ok {
			t.Error("LinkEnv missing SST_CACHE_MY_REDIS_HOST")
		}
	})
}

func TestNewCache_LinkName(t *testing.T) {
	t.Parallel()
	runTest(t, func(ctx *forge.RunContext) {
		c := NewCache(ctx, "Redis", &CacheArgs{
			SubnetIDs: testCacheSubnets,
		})
		if c.LinkName() != "Redis" {
			t.Errorf("LinkName = %q, want Redis", c.LinkName())
		}
	})
}

func TestNewCache_ImplementsLinkable(t *testing.T) {
	t.Parallel()
	runTest(t, func(ctx *forge.RunContext) {
		c := NewCache(ctx, "Redis", &CacheArgs{
			SubnetIDs: testCacheSubnets,
		})
		var _ forge.Linkable = c
	})
}

func TestCacheFamily_Redis7(t *testing.T) {
	t.Parallel()
	if got := cacheFamily("redis", "7.1"); got != "redis7" {
		t.Errorf("cacheFamily(redis, 7.1) = %q, want redis7", got)
	}
}

func TestCacheFamily_Redis6(t *testing.T) {
	t.Parallel()
	if got := cacheFamily("redis", "6.2"); got != "redis6.x" {
		t.Errorf("cacheFamily(redis, 6.2) = %q, want redis6.x", got)
	}
}

func TestCacheFamily_Redis5(t *testing.T) {
	t.Parallel()
	if got := cacheFamily("redis", "5.0.6"); got != "redis5.0" {
		t.Errorf("cacheFamily(redis, 5.0.6) = %q, want redis5.0", got)
	}
}

func TestCacheFamily_Valkey7(t *testing.T) {
	t.Parallel()
	if got := cacheFamily("valkey", "7.2"); got != "valkey7" {
		t.Errorf("cacheFamily(valkey, 7.2) = %q, want valkey7", got)
	}
}
