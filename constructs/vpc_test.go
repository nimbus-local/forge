package constructs

import (
	"testing"

	forge "github.com/nimbus-local/forge"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

func TestNewVpc_DefaultsCreateTwoAZs(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		NewVpc(ctx, "MyVpc", nil)
	})

	// 2 AZs × 1 public + 1 private = 4 subnets
	subnets := mocks.findAll("aws:ec2/subnet:Subnet")
	if len(subnets) != 4 {
		t.Errorf("expected 4 subnets (2 pub + 2 priv), got %d", len(subnets))
	}
}

func TestNewVpc_VpcResourceCreated(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		NewVpc(ctx, "Net", nil)
	})

	r := mocks.find("aws:ec2/vpc:Vpc")
	if r == nil {
		t.Fatal("VPC resource not created")
	}
	if r.inputs["cidrBlock"].StringValue() != "10.0.0.0/16" {
		t.Errorf("cidrBlock = %q, want 10.0.0.0/16", r.inputs["cidrBlock"].StringValue())
	}
	if !r.inputs["enableDnsSupport"].IsBool() || !r.inputs["enableDnsSupport"].BoolValue() {
		t.Error("enableDnsSupport should be true")
	}
	if !r.inputs["enableDnsHostnames"].IsBool() || !r.inputs["enableDnsHostnames"].BoolValue() {
		t.Error("enableDnsHostnames should be true")
	}
}

func TestNewVpc_InternetGatewayCreated(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		NewVpc(ctx, "Net", nil)
	})

	if mocks.find("aws:ec2/internetGateway:InternetGateway") == nil {
		t.Error("Internet Gateway not created")
	}
}

func TestNewVpc_DefaultSecurityGroupCreated(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		NewVpc(ctx, "Net", nil)
	})

	if mocks.find("aws:ec2/defaultSecurityGroup:DefaultSecurityGroup") == nil {
		t.Error("default security group not created")
	}
}

func TestNewVpc_RouteTablesCreated(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		NewVpc(ctx, "Net", nil)
	})

	// 2 AZs × (1 public RT + 1 private RT) = 4 route tables
	rts := mocks.findAll("aws:ec2/routeTable:RouteTable")
	if len(rts) != 4 {
		t.Errorf("expected 4 route tables (2 pub + 2 priv), got %d", len(rts))
	}
	// 4 route table associations
	rtas := mocks.findAll("aws:ec2/routeTableAssociation:RouteTableAssociation")
	if len(rtas) != 4 {
		t.Errorf("expected 4 route table associations, got %d", len(rtas))
	}
}

func TestNewVpc_NoNATByDefault(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		NewVpc(ctx, "Net", nil)
	})

	if mocks.find("aws:ec2/natGateway:NatGateway") != nil {
		t.Error("NAT Gateway should not be created when NAT is \"none\"")
	}
	if mocks.find("aws:ec2/eip:Eip") != nil {
		t.Error("Elastic IP should not be created when NAT is \"none\"")
	}
}

func TestNewVpc_ManagedNATCreatesGatewayPerAZ(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		NewVpc(ctx, "Net", &VpcArgs{NAT: "managed"})
	})

	nats := mocks.findAll("aws:ec2/natGateway:NatGateway")
	if len(nats) != 2 {
		t.Errorf("expected 2 NAT Gateways (one per AZ), got %d", len(nats))
	}
	eips := mocks.findAll("aws:ec2/eip:Eip")
	if len(eips) != 2 {
		t.Errorf("expected 2 Elastic IPs (one per NAT), got %d", len(eips))
	}
}

func TestNewVpc_ThreeAZs(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		NewVpc(ctx, "Net", &VpcArgs{AvailabilityZones: 3})
	})

	subnets := mocks.findAll("aws:ec2/subnet:Subnet")
	if len(subnets) != 6 {
		t.Errorf("expected 6 subnets (3 pub + 3 priv), got %d", len(subnets))
	}
}

func TestNewVpc_TagsApplied(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		NewVpc(ctx, "Net", nil)
	})

	r := mocks.find("aws:ec2/vpc:Vpc")
	if r == nil {
		t.Fatal("VPC not created")
	}
	for _, tag := range []string{"forge:app", "forge:stage", "forge:name"} {
		assertTag(t, r.inputs, tag)
	}
}

func TestNewVpc_EC2NATpanics(t *testing.T) {
	t.Parallel()
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for NAT=ec2 (not yet implemented)")
		}
	}()

	mocks := newMocks()
	_ = pulumi.RunErr(func(pctx *pulumi.Context) error {
		ctx := forge.NewRunContext(pctx, testApp, "test", "123456789012")
		NewVpc(ctx, "Net", &VpcArgs{NAT: "ec2"})
		return nil
	}, pulumi.WithMocks("myapp", "test", mocks))
}

func TestNewVpc_InvalidNATpanics(t *testing.T) {
	t.Parallel()
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for invalid NAT value")
		}
	}()

	mocks := newMocks()
	_ = pulumi.RunErr(func(pctx *pulumi.Context) error {
		ctx := forge.NewRunContext(pctx, testApp, "test", "123456789012")
		NewVpc(ctx, "Net", &VpcArgs{NAT: "bogus"})
		return nil
	}, pulumi.WithMocks("myapp", "test", mocks))
}
