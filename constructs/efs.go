package constructs

import (
	"fmt"

	forge "github.com/nimbus-local/forge"
	"github.com/pulumi/pulumi-aws/sdk/v7/go/aws/efs"
	"github.com/pulumi/pulumi-aws/sdk/v7/go/aws/iam"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

// Efs creates an EFS filesystem with a POSIX access point, ready for Lambda mounts.
// It mirrors sst.aws.Efs.
//
// When linked to a Function the handler receives SST_EFS_<NAME>_ACCESS_POINT_ARN
// and SST_EFS_<NAME>_MOUNT_PATH. Call Grant on the Function's execution role to add
// elasticfilesystem:Client* permissions. The Function must also set VpcSubnetIDs to
// subnets that contain the EFS mount targets and EfsMount to wire the access point.
type Efs struct {
	name        string
	fileSystem  *efs.FileSystem
	accessPoint *efs.AccessPoint
	mountPath   string
	ctx         *forge.RunContext
}

// EfsArgs configures the EFS filesystem construct.
type EfsArgs struct {
	// SubnetIDs are the VPC subnet IDs where EFS mount targets are created. Required.
	SubnetIDs []string
	// VpcSecurityGroupIDs optionally restricts mount target access.
	VpcSecurityGroupIDs []string
	// MountPath is the Linux path where the filesystem is mounted inside Lambda.
	// Must start with /mnt/. Defaults to "/mnt/efs".
	MountPath string
	// Tags are merged with the default forge tags.
	Tags map[string]string
}

// NewEfs creates an EFS construct.
// args.SubnetIDs is required.
func NewEfs(ctx *forge.RunContext, name string, args *EfsArgs) *Efs {
	if args == nil {
		args = &EfsArgs{}
	}
	if len(args.SubnetIDs) == 0 {
		panic(fmt.Sprintf("NewEfs %q: SubnetIDs is required", name))
	}
	mountPath := args.MountPath
	if mountPath == "" {
		mountPath = "/mnt/efs"
	}

	tags := mergedTags(defaultTags(ctx, name), args.Tags)
	pctx := ctx.Pulumi()

	// ── EFS filesystem ─────────────────────────────────────────────────────────
	fs, err := efs.NewFileSystem(pctx, name, &efs.FileSystemArgs{
		Encrypted: pulumi.Bool(true),
		Tags:      tags,
	})
	panicOnErr(err, name+": efs filesystem")

	// ── Mount targets (one per subnet) ─────────────────────────────────────────
	var sgIDs pulumi.StringArray
	for _, id := range args.VpcSecurityGroupIDs {
		sgIDs = append(sgIDs, pulumi.String(id))
	}
	for i, subnetID := range args.SubnetIDs {
		mtArgs := &efs.MountTargetArgs{
			FileSystemId: fs.ID(),
			SubnetId:     pulumi.String(subnetID),
		}
		if len(sgIDs) > 0 {
			mtArgs.SecurityGroups = sgIDs
		}
		_, err = efs.NewMountTarget(pctx, fmt.Sprintf("%s-mt-%d", name, i+1), mtArgs)
		panicOnErr(err, fmt.Sprintf("%s: mount target %d", name, i+1))
	}

	// ── Access point ───────────────────────────────────────────────────────────
	// UID/GID 1000 is a safe non-root POSIX identity for Lambda workloads.
	ap, err := efs.NewAccessPoint(pctx, name+"-ap", &efs.AccessPointArgs{
		FileSystemId: fs.ID(),
		PosixUser: &efs.AccessPointPosixUserArgs{
			Uid: pulumi.Int(1000),
			Gid: pulumi.Int(1000),
		},
		RootDirectory: &efs.AccessPointRootDirectoryArgs{
			Path: pulumi.String("/lambda"),
			CreationInfo: &efs.AccessPointRootDirectoryCreationInfoArgs{
				OwnerUid:    pulumi.Int(1000),
				OwnerGid:    pulumi.Int(1000),
				Permissions: pulumi.String("755"),
			},
		},
		Tags: tags,
	})
	panicOnErr(err, name+": efs access point")

	return &Efs{name: name, fileSystem: fs, accessPoint: ap, mountPath: mountPath, ctx: ctx}
}

// AccessPointARN returns the EFS access point ARN as a Pulumi output.
func (e *Efs) AccessPointARN() pulumi.StringOutput { return e.accessPoint.Arn }

// FileSystemID returns the EFS filesystem ID as a Pulumi output.
func (e *Efs) FileSystemID() pulumi.StringOutput { return e.fileSystem.ID().ToStringOutput() }

// MountPath returns the configured Lambda local mount path.
func (e *Efs) MountPath() string { return e.mountPath }

// Grant adds elasticfilesystem:ClientMount, ClientWrite, and ClientRootAccess
// permissions to role, scoped to this access point. Call this on the execution
// role of every Lambda that mounts this filesystem.
func (e *Efs) Grant(role *iam.Role) {
	pctx := e.ctx.Pulumi()

	policy := e.accessPoint.Arn.ApplyT(func(arn string) (string, error) {
		return fmt.Sprintf(`{
			"Version": "2012-10-17",
			"Statement": [{
				"Effect": "Allow",
				"Action": [
					"elasticfilesystem:ClientMount",
					"elasticfilesystem:ClientWrite",
					"elasticfilesystem:ClientRootAccess"
				],
				"Resource": %q
			}]
		}`, arn), nil
	}).(pulumi.StringOutput)

	_, err := iam.NewRolePolicy(pctx, e.name+"-efs-grant", &iam.RolePolicyArgs{
		Role:   role.Name,
		Policy: policy,
	})
	panicOnErr(err, e.name+": efs grant policy")
}

// LinkEnv implements Linkable.
func (e *Efs) LinkEnv() pulumi.StringMap {
	key := envKey(e.name)
	return pulumi.StringMap{
		fmt.Sprintf("SST_EFS_%s_ACCESS_POINT_ARN", key): e.accessPoint.Arn,
		fmt.Sprintf("SST_EFS_%s_MOUNT_PATH", key):       pulumi.String(e.mountPath),
	}
}

// LinkName implements Linkable.
func (e *Efs) LinkName() string { return e.name }
