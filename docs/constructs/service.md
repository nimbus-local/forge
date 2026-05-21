# Service

An ECS Fargate service construct. Runs a Docker container as a long-lived service with automatic task restarts, CloudWatch logging, and an optional Application Load Balancer.

## Minimal usage (no public endpoint)

```go
svc := constructs.NewService(ctx, "Worker", &constructs.ServiceArgs{
    Image: "123456789.dkr.ecr.us-east-1.amazonaws.com/worker:latest",
    VPC: &constructs.ServiceVPCArgs{
        SubnetIDs:        []string{"subnet-aaa", "subnet-bbb"},
        SecurityGroupIDs: []string{"sg-tasks"},
    },
    Link: []forge.Linkable{queue},
})
```

## With Application Load Balancer

```go
svc := constructs.NewService(ctx, "Api", &constructs.ServiceArgs{
    Image:  "123456789.dkr.ecr.us-east-1.amazonaws.com/api:latest",
    CPU:    512,
    Memory: 1024,
    Port:   8080,
    VPC: &constructs.ServiceVPCArgs{
        VPCID:            "vpc-0abc123",
        SubnetIDs:        []string{"subnet-private-a", "subnet-private-b"},
        SecurityGroupIDs: []string{"sg-tasks"},
    },
    // Public subnets for the ALB, with its own security group.
    ALBSubnetIDs:        []string{"subnet-public-a", "subnet-public-b"},
    ALBSecurityGroupIDs: []string{"sg-alb"},
    Environment: map[string]string{
        "LOG_LEVEL": "info",
    },
    Link: []forge.Linkable{table, bucket},
})
ctx.Export("url", svc.URL())
```

## Reusing an existing cluster

```go
svc := constructs.NewService(ctx, "Api", &constructs.ServiceArgs{
    Image:      "nginx:latest",
    ClusterARN: "arn:aws:ecs:us-east-1:123:cluster/my-cluster",
    VPC: &constructs.ServiceVPCArgs{
        SubnetIDs:        []string{"subnet-aaa"},
        SecurityGroupIDs: []string{"sg-tasks"},
        AssignPublicIP:   pulumi.BoolRef(true),
    },
})
```

## Attaching IAM policies to the task role

The task role is the IAM identity your container code assumes when making AWS API calls. Attach extra policies via `TaskRole()`:

```go
svc := constructs.NewService(ctx, "Processor", &constructs.ServiceArgs{...})

iam.NewRolePolicy(ctx.Pulumi(), "Processor-s3-policy", &iam.RolePolicyArgs{
    Role:   svc.TaskRole().Name,
    Policy: pulumi.String(`{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":"s3:*","Resource":"*"}]}`),
})
```

## Args reference

| Field | Type | Default | Description |
|---|---|---|---|
| `Image` | `string` | **required** | Docker image URI |
| `CPU` | `int` | `256` | Fargate CPU units (256, 512, 1024, 2048, 4096) |
| `Memory` | `int` | `512` | Fargate memory in MB (must be compatible with CPU) |
| `Port` | `int` | `0` | Container port; creates an ALB on port 80 when set |
| `ALBSubnetIDs` | `[]string` | `VPC.SubnetIDs` | Subnets for the ALB (should be public subnets) |
| `ALBSecurityGroupIDs` | `[]string` | `VPC.SecurityGroupIDs` | Security groups for the ALB |
| `DesiredCount` | `int` | `1` | Number of running tasks |
| `Environment` | `map[string]string` | — | Container environment variables |
| `Link` | `[]forge.Linkable` | — | Injects env vars from linked constructs |
| `VPC` | `*ServiceVPCArgs` | **required** | VPC networking config |
| `ClusterARN` | `string` | _(creates new)_ | Reuse an existing ECS cluster |

### ServiceVPCArgs

| Field | Type | Default | Description |
|---|---|---|---|
| `VPCID` | `string` | — | VPC ID; required when `Port > 0` |
| `SubnetIDs` | `[]string` | **required** | Subnets for ECS tasks |
| `SecurityGroupIDs` | `[]string` | **required** | Security groups for ECS tasks |
| `AssignPublicIP` | `*bool` | `true` | Assign public IPs to tasks (required without NAT gateway) |

## Resources created

| Resource | Purpose |
|---|---|
| `aws:ecs/cluster` | ECS cluster (skipped when `ClusterARN` is provided) |
| `aws:cloudwatch/logGroup` | `/ecs/<app>-<stage>-<name>` with 14-day retention |
| `aws:iam/role` (exec) | ECS agent role — pulls images and writes logs |
| `aws:iam/role` (task) | Container's AWS identity — attach app-specific policies here |
| `aws:ecs/taskDefinition` | Fargate task with awsvpc networking and awslogs driver |
| `aws:ecs/service` | Maintains `DesiredCount` running tasks |
| `aws:lb/loadBalancer` | Application Load Balancer (when `Port > 0`) |
| `aws:lb/targetGroup` | IP-mode target group for Fargate (when `Port > 0`) |
| `aws:lb/listener` | HTTP listener on port 80 → target group (when `Port > 0`) |

## Linkable outputs

When linked to a `Function`, the following environment variables are injected:

| Variable | Value |
|---|---|
| `SST_SERVICE_<NAME>_URL` | `http://<alb-dns>` (only when `Port > 0`) |

## Security group setup

For a typical public-internet → ALB → Fargate architecture, you need two security groups created outside of this construct:

**ALB security group** (`sg-alb`):
- Inbound: TCP 80 from `0.0.0.0/0`
- Outbound: TCP `<Port>` to `sg-tasks`

**Task security group** (`sg-tasks`):
- Inbound: TCP `<Port>` from `sg-alb`
- Outbound: HTTPS 443 to `0.0.0.0/0` (for ECR image pulls, SSM, etc.)

## CPU / memory compatibility

Valid Fargate (CPU units, memory MB) combinations:

| CPU | Memory options |
|---|---|
| 256 | 512, 1024, 2048 |
| 512 | 1024–4096 (in 1024 increments) |
| 1024 | 2048–8192 (in 1024 increments) |
| 2048 | 4096–16384 (in 1024 increments) |
| 4096 | 8192–30720 (in 1024 increments) |
