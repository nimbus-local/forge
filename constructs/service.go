package constructs

import (
	"encoding/json"
	"fmt"

	awssdk "github.com/pulumi/pulumi-aws/sdk/v6/go/aws"
	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/cloudwatch"
	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/ecs"
	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/iam"
	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/lb"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	forge "github.com/nimbus-local/forge"
)

// ServiceVPCArgs specifies the VPC context for the ECS Fargate tasks.
type ServiceVPCArgs struct {
	// VPCID is the VPC where the service runs. Required when Port > 0 (ALB target group needs it).
	VPCID string
	// SubnetIDs are the subnets for the ECS tasks.
	SubnetIDs []string
	// SecurityGroupIDs are attached to the ECS tasks.
	SecurityGroupIDs []string
	// AssignPublicIP assigns a public IP to tasks. Nil defaults to true (required for public subnets without NAT).
	AssignPublicIP *bool
}

// ServiceArgs configures an ECS Fargate service construct.
type ServiceArgs struct {
	// Image is the Docker image URI (required). E.g. "nginx:latest" or "123.dkr.ecr.us-east-1.amazonaws.com/app:v1".
	Image string
	// CPU units for the Fargate task. Valid values: 256, 512, 1024, 2048, 4096. Defaults to 256.
	CPU int
	// Memory in MB for the Fargate task. Must be compatible with CPU. Defaults to 512.
	Memory int
	// Port exposed by the container. When set, an Application Load Balancer is created on port 80.
	Port int
	// ALBSubnetIDs are the subnets for the ALB. Defaults to VPC.SubnetIDs. Should be public subnets for internet-facing ALBs.
	ALBSubnetIDs []string
	// ALBSecurityGroupIDs are the security groups for the ALB. Defaults to VPC.SecurityGroupIDs.
	ALBSecurityGroupIDs []string
	// DesiredCount is the number of running tasks. Defaults to 1.
	DesiredCount int
	// Environment variables injected into the container. Merged with variables from linked resources.
	Environment map[string]string
	// Link injects ARNs / URLs from other constructs as container environment variables.
	Link []forge.Linkable
	// VPC specifies the networking configuration for ECS tasks (required).
	VPC *ServiceVPCArgs
	// ClusterARN optionally reuses an existing ECS cluster. A new cluster is created if empty.
	ClusterARN string
}

// Service is an ECS Fargate service construct.
type Service struct {
	name     string
	service  *ecs.Service
	taskRole *iam.Role
	alb      *lb.LoadBalancer
	ctx      *forge.RunContext
}

// NewService creates an ECS Fargate service construct.
//
// Creates an ECS cluster (unless ClusterARN is provided), CloudWatch log group, task execution role,
// task role, task definition, and ECS service. When Port > 0, also creates an Application Load
// Balancer with a target group and HTTP listener on port 80.
//
// Usage:
//
//	svc := constructs.NewService(ctx, "Api", &constructs.ServiceArgs{
//	    Image:        "123.dkr.ecr.us-east-1.amazonaws.com/api:latest",
//	    Port:         8080,
//	    VPC: &constructs.ServiceVPCArgs{
//	        VPCID:            "vpc-0abc123",
//	        SubnetIDs:        []string{"subnet-aaa", "subnet-bbb"},
//	        SecurityGroupIDs: []string{"sg-tasks"},
//	    },
//	    ALBSecurityGroupIDs: []string{"sg-alb"},
//	    Link: []forge.Linkable{table, bucket},
//	})
//	ctx.Export("url", svc.URL())
func NewService(ctx *forge.RunContext, name string, args *ServiceArgs) *Service {
	if args == nil {
		args = &ServiceArgs{}
	}
	if args.Image == "" {
		panic("forge: ServiceArgs.Image is required for " + name)
	}
	if args.VPC == nil {
		panic("forge: ServiceArgs.VPC is required for " + name)
	}
	if args.CPU == 0 {
		args.CPU = 256
	}
	if args.Memory == 0 {
		args.Memory = 512
	}
	if args.DesiredCount == 0 {
		args.DesiredCount = 1
	}

	pctx := ctx.Pulumi()

	region, err := awssdk.GetRegion(pctx, nil)
	panicOnErr(err, name+": get region")

	// ── CloudWatch log group ──────────────────────────────────────────────────
	logGroupName := fmt.Sprintf("/ecs/%s", qualifiedName(ctx, name))
	_, err = cloudwatch.NewLogGroup(pctx, name+"-logs", &cloudwatch.LogGroupArgs{
		Name:            pulumi.String(logGroupName),
		RetentionInDays: pulumi.Int(14),
		Tags:            defaultTags(ctx, name),
	})
	panicOnErr(err, name+": log group")

	// ── Task execution role (ECS agent pulls image and writes logs) ───────────
	execRole, err := iam.NewRole(pctx, name+"-exec-role", &iam.RoleArgs{
		AssumeRolePolicy: pulumi.String(`{
			"Version": "2012-10-17",
			"Statement": [{
				"Effect": "Allow",
				"Principal": { "Service": "ecs-tasks.amazonaws.com" },
				"Action": "sts:AssumeRole"
			}]
		}`),
		ManagedPolicyArns: pulumi.StringArray{
			pulumi.String("arn:aws:iam::aws:policy/service-role/AmazonECSTaskExecutionRolePolicy"),
		},
		Tags: defaultTags(ctx, name),
	})
	panicOnErr(err, name+": exec role")

	// ── Task role (the container's AWS identity — attach policies via TaskRole()) ──
	taskRole, err := iam.NewRole(pctx, name+"-task-role", &iam.RoleArgs{
		AssumeRolePolicy: pulumi.String(`{
			"Version": "2012-10-17",
			"Statement": [{
				"Effect": "Allow",
				"Principal": { "Service": "ecs-tasks.amazonaws.com" },
				"Action": "sts:AssumeRole"
			}]
		}`),
		Tags: defaultTags(ctx, name),
	})
	panicOnErr(err, name+": task role")

	// ── ECS cluster ───────────────────────────────────────────────────────────
	var clusterARN pulumi.StringInput
	if args.ClusterARN != "" {
		clusterARN = pulumi.String(args.ClusterARN)
	} else {
		cluster, cerr := ecs.NewCluster(pctx, name+"-cluster", &ecs.ClusterArgs{
			Name: pulumi.String(qualifiedName(ctx, name+"-cluster")),
			Tags: defaultTags(ctx, name),
		})
		panicOnErr(cerr, name+": ecs cluster")
		clusterARN = cluster.Arn
	}

	// ── Task definition ───────────────────────────────────────────────────────
	containerDefs := buildContainerDefs(ctx, name, args, logGroupName, region.Name)

	taskDef, err := ecs.NewTaskDefinition(pctx, name+"-taskdef", &ecs.TaskDefinitionArgs{
		Family:                  pulumi.String(qualifiedName(ctx, name)),
		Cpu:                     pulumi.String(fmt.Sprintf("%d", args.CPU)),
		Memory:                  pulumi.String(fmt.Sprintf("%d", args.Memory)),
		NetworkMode:             pulumi.String("awsvpc"),
		RequiresCompatibilities: pulumi.StringArray{pulumi.String("FARGATE")},
		ExecutionRoleArn:        execRole.Arn,
		TaskRoleArn:             taskRole.Arn,
		ContainerDefinitions:    containerDefs,
		Tags:                    defaultTags(ctx, name),
	})
	panicOnErr(err, name+": task definition")

	// ── ALB (only when Port is set) ───────────────────────────────────────────
	var alb *lb.LoadBalancer
	var tg *lb.TargetGroup

	if args.Port > 0 {
		if args.VPC.VPCID == "" {
			panic("forge: ServiceVPCArgs.VPCID is required when Port > 0 for " + name)
		}

		albSubnets := args.ALBSubnetIDs
		if len(albSubnets) == 0 {
			albSubnets = args.VPC.SubnetIDs
		}
		albSGs := args.ALBSecurityGroupIDs
		if len(albSGs) == 0 {
			albSGs = args.VPC.SecurityGroupIDs
		}

		alb, err = lb.NewLoadBalancer(pctx, name+"-alb", &lb.LoadBalancerArgs{
			Name:             pulumi.String(qualifiedName(ctx, name+"-alb")),
			LoadBalancerType: pulumi.String("application"),
			Internal:         pulumi.Bool(false),
			Subnets:          toStringArray(albSubnets),
			SecurityGroups:   toStringArray(albSGs),
			Tags:             defaultTags(ctx, name),
		})
		panicOnErr(err, name+": alb")

		tg, err = lb.NewTargetGroup(pctx, name+"-tg", &lb.TargetGroupArgs{
			Name:       pulumi.String(qualifiedName(ctx, name+"-tg")),
			Port:       pulumi.Int(args.Port),
			Protocol:   pulumi.String("HTTP"),
			TargetType: pulumi.String("ip"),
			VpcId:      pulumi.String(args.VPC.VPCID),
			HealthCheck: &lb.TargetGroupHealthCheckArgs{
				Path:               pulumi.String("/"),
				HealthyThreshold:   pulumi.Int(2),
				UnhealthyThreshold: pulumi.Int(3),
				Interval:           pulumi.Int(30),
			},
			Tags: defaultTags(ctx, name),
		})
		panicOnErr(err, name+": target group")

		_, err = lb.NewListener(pctx, name+"-listener", &lb.ListenerArgs{
			LoadBalancerArn: alb.Arn,
			Port:            pulumi.Int(80),
			Protocol:        pulumi.String("HTTP"),
			DefaultActions: lb.ListenerDefaultActionArray{
				&lb.ListenerDefaultActionArgs{
					Type:           pulumi.String("forward"),
					TargetGroupArn: tg.Arn,
				},
			},
		})
		panicOnErr(err, name+": listener")
	}

	// ── ECS service ───────────────────────────────────────────────────────────
	assignPublicIP := true
	if args.VPC.AssignPublicIP != nil {
		assignPublicIP = *args.VPC.AssignPublicIP
	}

	serviceArgs := &ecs.ServiceArgs{
		Name:           pulumi.String(qualifiedName(ctx, name)),
		Cluster:        clusterARN,
		TaskDefinition: taskDef.Arn,
		DesiredCount:   pulumi.Int(args.DesiredCount),
		LaunchType:     pulumi.String("FARGATE"),
		NetworkConfiguration: &ecs.ServiceNetworkConfigurationArgs{
			Subnets:        toStringArray(args.VPC.SubnetIDs),
			SecurityGroups: toStringArray(args.VPC.SecurityGroupIDs),
			AssignPublicIp: pulumi.Bool(assignPublicIP),
		},
		Tags: defaultTags(ctx, name),
	}

	if tg != nil {
		serviceArgs.LoadBalancers = ecs.ServiceLoadBalancerArray{
			&ecs.ServiceLoadBalancerArgs{
				TargetGroupArn: tg.Arn,
				ContainerName:  pulumi.String(qualifiedName(ctx, name)),
				ContainerPort:  pulumi.Int(args.Port),
			},
		}
	}

	svc, err := ecs.NewService(pctx, name, serviceArgs)
	panicOnErr(err, name+": ecs service")

	return &Service{name: name, service: svc, taskRole: taskRole, alb: alb, ctx: ctx}
}

// TaskRole returns the IAM task role so callers can attach additional policies.
func (s *Service) TaskRole() *iam.Role { return s.taskRole }

// ServiceARN returns the ECS service ARN.
func (s *Service) ServiceARN() pulumi.StringOutput { return s.service.ID().ToStringOutput() }

// URL returns the ALB DNS name as an http:// URL, or an empty string if no port was configured.
func (s *Service) URL() pulumi.StringOutput {
	if s.alb == nil {
		return pulumi.String("").ToStringOutput()
	}
	return s.alb.DnsName.ApplyT(func(dns string) string {
		return "http://" + dns
	}).(pulumi.StringOutput)
}

// LinkEnv implements Linkable — exposes the service URL to other constructs.
// SST_SERVICE_<NAME>_URL is set only when the service has an ALB (Port > 0).
func (s *Service) LinkEnv() pulumi.StringMap {
	if s.alb == nil {
		return pulumi.StringMap{}
	}
	return pulumi.StringMap{
		fmt.Sprintf("SST_SERVICE_%s_URL", envKey(s.name)): s.URL(),
	}
}

// LinkName implements Linkable.
func (s *Service) LinkName() string { return s.name }

// ── helpers ───────────────────────────────────────────────────────────────────

// buildContainerDefs builds the ECS container definitions JSON, resolving any Pulumi
// output values from linked constructs via pulumi.All.
func buildContainerDefs(ctx *forge.RunContext, name string, args *ServiceArgs, logGroupName, region string) pulumi.StringOutput {
	type pair struct {
		key string
		val pulumi.StringInput
	}

	var pairs []pair
	for k, v := range args.Environment {
		pairs = append(pairs, pair{k, pulumi.String(v)})
	}
	pairs = append(pairs, pair{"FORGE_STAGE", pulumi.String(ctx.Stage)})
	for _, link := range args.Link {
		for k, v := range link.LinkEnv() {
			pairs = append(pairs, pair{k, v})
		}
	}

	ifaces := make([]interface{}, len(pairs))
	for i, p := range pairs {
		ifaces[i] = p.val
	}

	containerName := qualifiedName(ctx, name)
	image := args.Image
	port := args.Port

	return pulumi.All(ifaces...).ApplyT(func(vals []interface{}) (string, error) {
		envList := make([]map[string]string, len(pairs))
		for i, p := range pairs {
			envList[i] = map[string]string{"name": p.key, "value": vals[i].(string)}
		}

		portMappings := []map[string]interface{}{}
		if port > 0 {
			portMappings = append(portMappings, map[string]interface{}{
				"containerPort": port,
				"protocol":      "tcp",
			})
		}

		def := []map[string]interface{}{{
			"name":         containerName,
			"image":        image,
			"essential":    true,
			"environment":  envList,
			"portMappings": portMappings,
			"logConfiguration": map[string]interface{}{
				"logDriver": "awslogs",
				"options": map[string]interface{}{
					"awslogs-group":         logGroupName,
					"awslogs-region":        region,
					"awslogs-stream-prefix": "ecs",
				},
			},
		}}

		b, err := json.Marshal(def)
		return string(b), err
	}).(pulumi.StringOutput)
}
