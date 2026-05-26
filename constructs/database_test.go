package constructs

import (
	"testing"

	forge "github.com/nimbus-local/forge"
)

var testSubnets = []string{"subnet-00000001", "subnet-00000002"}

func TestNewDatabase_ClusterCreated(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		NewDatabase(ctx, "Db", &DatabaseArgs{
			SubnetIDs:      testSubnets,
			MasterPassword: "testpass",
		})
	})

	if mocks.find("aws:rds/cluster:Cluster") == nil {
		t.Error("RDS cluster not created")
	}
}

func TestNewDatabase_SubnetGroupCreated(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		NewDatabase(ctx, "Db", &DatabaseArgs{
			SubnetIDs:      testSubnets,
			MasterPassword: "testpass",
		})
	})

	if mocks.find("aws:rds/subnetGroup:SubnetGroup") == nil {
		t.Error("DB subnet group not created")
	}
}

func TestNewDatabase_WriterInstanceCreated(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		NewDatabase(ctx, "Db", &DatabaseArgs{
			SubnetIDs:      testSubnets,
			MasterPassword: "testpass",
		})
	})

	if mocks.find("aws:rds/clusterInstance:ClusterInstance") == nil {
		t.Error("cluster writer instance not created")
	}
}

func TestNewDatabase_WriterInstanceClassIsServerless(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		NewDatabase(ctx, "Db", &DatabaseArgs{
			SubnetIDs:      testSubnets,
			MasterPassword: "testpass",
		})
	})

	r := mocks.find("aws:rds/clusterInstance:ClusterInstance")
	if r == nil {
		t.Fatal("ClusterInstance not registered")
	}
	if r.inputs["instanceClass"].StringValue() != "db.serverless" {
		t.Errorf("instanceClass = %q, want db.serverless", r.inputs["instanceClass"].StringValue())
	}
}

func TestNewDatabase_PhysicalNameQualified(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		NewDatabase(ctx, "Db", &DatabaseArgs{
			SubnetIDs:      testSubnets,
			MasterPassword: "testpass",
		})
	})

	r := mocks.find("aws:rds/cluster:Cluster")
	if r == nil {
		t.Fatal("Cluster not registered")
	}
	if r.inputs["clusterIdentifier"].StringValue() != "myapp-test-db" {
		t.Errorf("clusterIdentifier = %q, want myapp-test-db", r.inputs["clusterIdentifier"].StringValue())
	}
}

func TestNewDatabase_DefaultEngineIsAuroraPostgres(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		NewDatabase(ctx, "Db", &DatabaseArgs{
			SubnetIDs:      testSubnets,
			MasterPassword: "testpass",
		})
	})

	r := mocks.find("aws:rds/cluster:Cluster")
	if r == nil {
		t.Fatal("Cluster not registered")
	}
	if r.inputs["engine"].StringValue() != "aurora-postgresql" {
		t.Errorf("engine = %q, want aurora-postgresql", r.inputs["engine"].StringValue())
	}
}

func TestNewDatabase_DefaultDatabaseNameIsApp(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		NewDatabase(ctx, "Db", &DatabaseArgs{
			SubnetIDs:      testSubnets,
			MasterPassword: "testpass",
		})
	})

	r := mocks.find("aws:rds/cluster:Cluster")
	if r == nil {
		t.Fatal("Cluster not registered")
	}
	if r.inputs["databaseName"].StringValue() != "app" {
		t.Errorf("databaseName = %q, want app", r.inputs["databaseName"].StringValue())
	}
}

func TestNewDatabase_DefaultMasterUsernameIsAdmin(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		NewDatabase(ctx, "Db", &DatabaseArgs{
			SubnetIDs:      testSubnets,
			MasterPassword: "testpass",
		})
	})

	r := mocks.find("aws:rds/cluster:Cluster")
	if r == nil {
		t.Fatal("Cluster not registered")
	}
	if r.inputs["masterUsername"].StringValue() != "admin" {
		t.Errorf("masterUsername = %q, want admin", r.inputs["masterUsername"].StringValue())
	}
}

func TestNewDatabase_MasterPasswordSetDisablesManagedPassword(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		NewDatabase(ctx, "Db", &DatabaseArgs{
			SubnetIDs:      testSubnets,
			MasterPassword: "s3cr3t",
		})
	})

	r := mocks.find("aws:rds/cluster:Cluster")
	if r == nil {
		t.Fatal("Cluster not registered")
	}
	// When MasterPassword is set, manageMasterUserPassword must not be true.
	if v, ok := r.inputs["manageMasterUserPassword"]; ok && v.V.(bool) {
		t.Error("manageMasterUserPassword should not be true when MasterPassword is set")
	}
}

func TestNewDatabase_ManageMasterUserPasswordWhenNoPassword(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		NewDatabase(ctx, "Db", &DatabaseArgs{
			SubnetIDs: testSubnets,
			// No MasterPassword → ManageMasterUserPassword: true
		})
	})

	r := mocks.find("aws:rds/cluster:Cluster")
	if r == nil {
		t.Fatal("Cluster not registered")
	}
	if !r.inputs["manageMasterUserPassword"].V.(bool) {
		t.Error("manageMasterUserPassword should be true when no MasterPassword is set")
	}
}

func TestNewDatabase_SkipFinalSnapshotDefaultsTrue(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		NewDatabase(ctx, "Db", &DatabaseArgs{
			SubnetIDs:      testSubnets,
			MasterPassword: "testpass",
		})
	})

	r := mocks.find("aws:rds/cluster:Cluster")
	if r == nil {
		t.Fatal("Cluster not registered")
	}
	if !r.inputs["skipFinalSnapshot"].V.(bool) {
		t.Error("skipFinalSnapshot should default to true")
	}
}

func TestNewDatabase_TagsApplied(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		NewDatabase(ctx, "Db", &DatabaseArgs{
			SubnetIDs:      testSubnets,
			MasterPassword: "testpass",
		})
	})

	r := mocks.find("aws:rds/cluster:Cluster")
	if r == nil {
		t.Fatal("Cluster not registered")
	}
	for _, tag := range []string{"forge:app", "forge:stage", "forge:name"} {
		assertTag(t, r.inputs, tag)
	}
}

func TestNewDatabase_NoSubnetIDsPanics(t *testing.T) {
	t.Parallel()
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for missing SubnetIDs")
		}
	}()
	runTest(t, func(ctx *forge.RunContext) {
		NewDatabase(ctx, "Db", &DatabaseArgs{MasterPassword: "testpass"})
	})
}

func TestNewDatabase_NilArgsPanics(t *testing.T) {
	t.Parallel()
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for nil args (missing SubnetIDs)")
		}
	}()
	runTest(t, func(ctx *forge.RunContext) {
		NewDatabase(ctx, "Db", nil)
	})
}

func TestNewDatabase_GrantCreatesPolicy(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		db := NewDatabase(ctx, "Db", &DatabaseArgs{
			SubnetIDs:      testSubnets,
			MasterPassword: "testpass",
		})
		fn := NewFunction(ctx, "Api", &FunctionArgs{Handler: "bootstrap"})
		db.Grant(fn.Role())
	})

	if mocks.find("aws:iam/rolePolicy:RolePolicy") == nil {
		t.Error("IAM role policy not created by Grant")
	}
}

func TestNewDatabase_LinkEnvKeys(t *testing.T) {
	t.Parallel()
	runTest(t, func(ctx *forge.RunContext) {
		db := NewDatabase(ctx, "Db", &DatabaseArgs{
			SubnetIDs:      testSubnets,
			MasterPassword: "testpass",
		})
		env := db.LinkEnv()
		for _, key := range []string{
			"SST_DATABASE_DB_HOST",
			"SST_DATABASE_DB_PORT",
			"SST_DATABASE_DB_NAME",
			"SST_DATABASE_DB_USERNAME",
			"SST_DATABASE_DB_SECRET_ARN",
			"SST_DATABASE_DB_CLUSTER_ARN",
		} {
			if _, ok := env[key]; !ok {
				t.Errorf("LinkEnv missing %s", key)
			}
		}
		if len(env) != 6 {
			t.Errorf("LinkEnv has %d keys, want 6", len(env))
		}
	})
}

func TestNewDatabase_CamelCaseLinkEnvKey(t *testing.T) {
	t.Parallel()
	runTest(t, func(ctx *forge.RunContext) {
		db := NewDatabase(ctx, "UserDb", &DatabaseArgs{
			SubnetIDs:      testSubnets,
			MasterPassword: "testpass",
		})
		env := db.LinkEnv()
		if _, ok := env["SST_DATABASE_USER_DB_HOST"]; !ok {
			t.Error("LinkEnv missing SST_DATABASE_USER_DB_HOST")
		}
	})
}

func TestNewDatabase_LinkName(t *testing.T) {
	t.Parallel()
	runTest(t, func(ctx *forge.RunContext) {
		db := NewDatabase(ctx, "Db", &DatabaseArgs{
			SubnetIDs:      testSubnets,
			MasterPassword: "testpass",
		})
		if db.LinkName() != "Db" {
			t.Errorf("LinkName = %q, want Db", db.LinkName())
		}
	})
}

func TestNewDatabase_ImplementsLinkable(t *testing.T) {
	t.Parallel()
	runTest(t, func(ctx *forge.RunContext) {
		db := NewDatabase(ctx, "Db", &DatabaseArgs{
			SubnetIDs:      testSubnets,
			MasterPassword: "testpass",
		})
		var _ forge.Linkable = db
	})
}
