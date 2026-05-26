package constructs

import (
	"testing"

	forge "github.com/nimbus-local/forge"
)

var testEfsSubnets = []string{"subnet-00000001", "subnet-00000002"}

func TestNewEfs_FileSystemCreated(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		NewEfs(ctx, "Data", &EfsArgs{SubnetIDs: testEfsSubnets})
	})

	if mocks.find("aws:efs/fileSystem:FileSystem") == nil {
		t.Error("EFS filesystem not created")
	}
}

func TestNewEfs_MountTargetsCreated(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		NewEfs(ctx, "Data", &EfsArgs{SubnetIDs: testEfsSubnets})
	})

	mts := mocks.findAll("aws:efs/mountTarget:MountTarget")
	if len(mts) != len(testEfsSubnets) {
		t.Errorf("expected %d mount targets, got %d", len(testEfsSubnets), len(mts))
	}
}

func TestNewEfs_AccessPointCreated(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		NewEfs(ctx, "Data", &EfsArgs{SubnetIDs: testEfsSubnets})
	})

	if mocks.find("aws:efs/accessPoint:AccessPoint") == nil {
		t.Error("EFS access point not created")
	}
}

func TestNewEfs_EncryptedByDefault(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		NewEfs(ctx, "Data", &EfsArgs{SubnetIDs: testEfsSubnets})
	})

	r := mocks.find("aws:efs/fileSystem:FileSystem")
	if r == nil {
		t.Fatal("EFS filesystem not registered")
	}
	if !r.inputs["encrypted"].IsBool() || !r.inputs["encrypted"].BoolValue() {
		t.Error("encrypted should be true")
	}
}

func TestNewEfs_DefaultMountPath(t *testing.T) {
	t.Parallel()
	runTest(t, func(ctx *forge.RunContext) {
		e := NewEfs(ctx, "Data", &EfsArgs{SubnetIDs: testEfsSubnets})
		if e.MountPath() != "/mnt/efs" {
			t.Errorf("MountPath = %q, want /mnt/efs", e.MountPath())
		}
	})
}

func TestNewEfs_CustomMountPath(t *testing.T) {
	t.Parallel()
	runTest(t, func(ctx *forge.RunContext) {
		e := NewEfs(ctx, "Data", &EfsArgs{
			SubnetIDs: testEfsSubnets,
			MountPath: "/mnt/data",
		})
		if e.MountPath() != "/mnt/data" {
			t.Errorf("MountPath = %q, want /mnt/data", e.MountPath())
		}
	})
}

func TestNewEfs_TagsApplied(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		NewEfs(ctx, "Data", &EfsArgs{SubnetIDs: testEfsSubnets})
	})

	r := mocks.find("aws:efs/fileSystem:FileSystem")
	if r == nil {
		t.Fatal("EFS filesystem not registered")
	}
	for _, tag := range []string{"forge:app", "forge:stage", "forge:name"} {
		assertTag(t, r.inputs, tag)
	}
}

func TestNewEfs_AccessPointTagsApplied(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		NewEfs(ctx, "Data", &EfsArgs{SubnetIDs: testEfsSubnets})
	})

	r := mocks.find("aws:efs/accessPoint:AccessPoint")
	if r == nil {
		t.Fatal("EFS access point not registered")
	}
	for _, tag := range []string{"forge:app", "forge:stage", "forge:name"} {
		assertTag(t, r.inputs, tag)
	}
}

func TestNewEfs_AccessPointRootDirectoryPath(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		NewEfs(ctx, "Data", &EfsArgs{SubnetIDs: testEfsSubnets})
	})

	r := mocks.find("aws:efs/accessPoint:AccessPoint")
	if r == nil {
		t.Fatal("EFS access point not registered")
	}
	rd, ok := r.inputs["rootDirectory"]
	if !ok || !rd.IsObject() {
		t.Fatal("rootDirectory missing or not an object")
	}
	path, ok := rd.ObjectValue()["path"]
	if !ok || path.StringValue() != "/lambda" {
		t.Errorf("rootDirectory.path = %q, want /lambda", path.StringValue())
	}
}

func TestNewEfs_NoSubnetIDsPanics(t *testing.T) {
	t.Parallel()
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for missing SubnetIDs")
		}
	}()
	runTest(t, func(ctx *forge.RunContext) {
		NewEfs(ctx, "Data", &EfsArgs{})
	})
}

func TestNewEfs_NilArgsPanics(t *testing.T) {
	t.Parallel()
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for nil args (missing SubnetIDs)")
		}
	}()
	runTest(t, func(ctx *forge.RunContext) {
		NewEfs(ctx, "Data", nil)
	})
}

func TestNewEfs_LinkEnvKeys(t *testing.T) {
	t.Parallel()
	runTest(t, func(ctx *forge.RunContext) {
		e := NewEfs(ctx, "Data", &EfsArgs{SubnetIDs: testEfsSubnets})
		env := e.LinkEnv()
		for _, key := range []string{
			"SST_EFS_DATA_ACCESS_POINT_ARN",
			"SST_EFS_DATA_MOUNT_PATH",
		} {
			if _, ok := env[key]; !ok {
				t.Errorf("LinkEnv missing %s", key)
			}
		}
		if len(env) != 2 {
			t.Errorf("LinkEnv has %d keys, want 2", len(env))
		}
	})
}

func TestNewEfs_CamelCaseLinkEnvKey(t *testing.T) {
	t.Parallel()
	runTest(t, func(ctx *forge.RunContext) {
		e := NewEfs(ctx, "SharedData", &EfsArgs{SubnetIDs: testEfsSubnets})
		env := e.LinkEnv()
		if _, ok := env["SST_EFS_SHARED_DATA_ACCESS_POINT_ARN"]; !ok {
			t.Error("LinkEnv missing SST_EFS_SHARED_DATA_ACCESS_POINT_ARN")
		}
	})
}

func TestNewEfs_LinkName(t *testing.T) {
	t.Parallel()
	runTest(t, func(ctx *forge.RunContext) {
		e := NewEfs(ctx, "Data", &EfsArgs{SubnetIDs: testEfsSubnets})
		if e.LinkName() != "Data" {
			t.Errorf("LinkName = %q, want Data", e.LinkName())
		}
	})
}

func TestNewEfs_ImplementsLinkable(t *testing.T) {
	t.Parallel()
	runTest(t, func(ctx *forge.RunContext) {
		e := NewEfs(ctx, "Data", &EfsArgs{SubnetIDs: testEfsSubnets})
		var _ forge.Linkable = e
	})
}

func TestNewEfs_GrantCreatesPolicy(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		e := NewEfs(ctx, "Data", &EfsArgs{SubnetIDs: testEfsSubnets})
		fn := NewFunction(ctx, "Worker", &FunctionArgs{
			Handler:      "bootstrap",
			VpcSubnetIDs: testEfsSubnets,
			EfsMount:     e,
		})
		e.Grant(fn.Role())
	})

	if mocks.find("aws:iam/rolePolicy:RolePolicy") == nil {
		t.Error("IAM role policy not created by Grant")
	}
}

// ── NewFunction VPC / EFS tests ───────────────────────────────────────────────

func TestNewFunction_VpcConfigApplied(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		NewFunction(ctx, "Worker", &FunctionArgs{
			Handler:             "bootstrap",
			VpcSubnetIDs:        testEfsSubnets,
			VpcSecurityGroupIDs: []string{"sg-00000001"},
		})
	})

	r := mocks.find("aws:lambda/function:Function")
	if r == nil {
		t.Fatal("Lambda function not registered")
	}
	if _, ok := r.inputs["vpcConfig"]; !ok {
		t.Error("Lambda missing vpcConfig")
	}
}

func TestNewFunction_VpcConfigAddsVpcManagedPolicy(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		NewFunction(ctx, "Worker", &FunctionArgs{
			Handler:      "bootstrap",
			VpcSubnetIDs: testEfsSubnets,
		})
	})

	r := mocks.find("aws:iam/role:Role")
	if r == nil {
		t.Fatal("IAM role not registered")
	}
	policies := r.inputs["managedPolicyArns"]
	if !policies.IsArray() {
		t.Fatal("managedPolicyArns is not an array")
	}
	found := false
	for _, p := range policies.ArrayValue() {
		if p.StringValue() == "arn:aws:iam::aws:policy/service-role/AWSLambdaVPCAccessExecutionRole" {
			found = true
			break
		}
	}
	if !found {
		t.Error("AWSLambdaVPCAccessExecutionRole not attached when VpcSubnetIDs is set")
	}
}

func TestNewFunction_EfsMountAddsFileSystemConfig(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		e := NewEfs(ctx, "Data", &EfsArgs{SubnetIDs: testEfsSubnets})
		NewFunction(ctx, "Worker", &FunctionArgs{
			Handler:      "bootstrap",
			VpcSubnetIDs: testEfsSubnets,
			EfsMount:     e,
		})
	})

	r := mocks.find("aws:lambda/function:Function")
	if r == nil {
		t.Fatal("Lambda function not registered")
	}
	if _, ok := r.inputs["fileSystemConfig"]; !ok {
		t.Error("Lambda missing fileSystemConfig when EfsMount is set")
	}
}

func TestNewFunction_EfsMountWithoutVpcPanics(t *testing.T) {
	t.Parallel()
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic when EfsMount is set without VpcSubnetIDs")
		}
	}()
	runTest(t, func(ctx *forge.RunContext) {
		e := NewEfs(ctx, "Data", &EfsArgs{SubnetIDs: testEfsSubnets})
		NewFunction(ctx, "Worker", &FunctionArgs{
			Handler:  "bootstrap",
			EfsMount: e, // VpcSubnetIDs missing — should panic
		})
	})
}

func TestNewFunction_NoVpcConfigWithoutSubnets(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		NewFunction(ctx, "Fn", nil)
	})

	r := mocks.find("aws:lambda/function:Function")
	if r == nil {
		t.Fatal("Lambda function not registered")
	}
	if _, ok := r.inputs["vpcConfig"]; ok {
		t.Error("Lambda should not have vpcConfig when VpcSubnetIDs is empty")
	}
}
