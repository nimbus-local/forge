package constructs

import (
	"fmt"

	forge "github.com/nimbus-local/forge"
	awssdk "github.com/pulumi/pulumi-aws/sdk/v7/go/aws"
	"github.com/pulumi/pulumi-aws/sdk/v7/go/aws/ec2"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

// Vpc creates an AWS VPC with public and private subnets across multiple Availability Zones.
//
// Resources created per AZ:
//   - One public subnet  (10.0.{8*i}.0/22, IGW route)
//   - One private subnet (10.0.{8*i+4}.0/22, no internet by default)
//   - Route tables + associations for each subnet
//
// When NAT is "managed", one NAT Gateway + Elastic IP is added per AZ so private
// subnets can reach the internet.
//
// Vpc does not implement forge.Linkable — pass it directly via DatabaseArgs.Vpc,
// CacheArgs.Vpc, EfsArgs.Vpc, or ServiceArgs.VPC.
type Vpc struct {
	name        string
	vpc         *ec2.Vpc
	secGroup    *ec2.DefaultSecurityGroup
	pubSubnets  []*ec2.Subnet
	privSubnets []*ec2.Subnet
	ctx         *forge.RunContext
}

// VpcArgs configures a Vpc construct.
type VpcArgs struct {
	// AvailabilityZones is the number of AZs to span. Default: 2.
	// Must not exceed the number of AZs available in the region.
	AvailabilityZones int

	// NAT controls how private subnets reach the internet.
	//
	//  "none"    (default) — no outbound internet access from private subnets.
	//  "managed" — one NAT Gateway per AZ (~$32/mo per AZ in us-east-1).
	//  "ec2"     — TODO: fck-nat t4g.nano instance per AZ (~$3/mo) — not yet implemented.
	NAT string

	// Tags merged with stage-level tags on every resource.
	Tags map[string]string
}

// NewVpc creates a VPC construct.
func NewVpc(ctx *forge.RunContext, name string, args *VpcArgs) *Vpc {
	if args == nil {
		args = &VpcArgs{}
	}
	numAZs := args.AvailabilityZones
	if numAZs < 1 {
		numAZs = 2
	}
	nat := args.NAT
	if nat == "" {
		nat = "none"
	}

	switch nat {
	case "none", "managed":
		// supported
	case "ec2":
		// TODO: implement fck-nat AMI lookup + EC2 instance + source/dest check disable
		panic("forge: VpcArgs.NAT=\"ec2\" is not yet implemented — use \"managed\" or \"none\"")
	default:
		panic(fmt.Sprintf("forge: invalid VpcArgs.NAT %q — valid values: \"none\", \"managed\"", nat))
	}

	pctx := ctx.Pulumi()

	// ── AZ discovery (synchronous invoke — safe to call before resource creation) ──
	zones, err := awssdk.GetAvailabilityZones(pctx, &awssdk.GetAvailabilityZonesArgs{
		State: pulumi.StringRef("available"),
	})
	panicOnErr(err, name+": get availability zones")
	if len(zones.Names) < numAZs {
		panic(fmt.Sprintf("forge: NewVpc %q: AvailabilityZones=%d but region only has %d available AZs",
			name, numAZs, len(zones.Names)))
	}
	azNames := zones.Names[:numAZs]

	// ── VPC ───────────────────────────────────────────────────────────────────
	vpc, err := ec2.NewVpc(pctx, name, &ec2.VpcArgs{
		CidrBlock:          pulumi.String("10.0.0.0/16"),
		EnableDnsSupport:   pulumi.Bool(true),
		EnableDnsHostnames: pulumi.Bool(true),
		Tags:               mergedTags(defaultTags(ctx, name), args.Tags),
	})
	panicOnErr(err, name+": vpc")

	// ── Internet Gateway ──────────────────────────────────────────────────────
	igw, err := ec2.NewInternetGateway(pctx, name+"-igw", &ec2.InternetGatewayArgs{
		VpcId: vpc.ID(),
		Tags:  mergedTags(defaultTags(ctx, name), args.Tags),
	})
	panicOnErr(err, name+": internet gateway")

	// ── Default Security Group ────────────────────────────────────────────────
	// Egress: open to internet. Ingress: only from within the VPC CIDR.
	secGroup, err := ec2.NewDefaultSecurityGroup(pctx, name+"-sg", &ec2.DefaultSecurityGroupArgs{
		VpcId: vpc.ID(),
		Egress: ec2.DefaultSecurityGroupEgressArray{
			&ec2.DefaultSecurityGroupEgressArgs{
				FromPort:   pulumi.Int(0),
				ToPort:     pulumi.Int(0),
				Protocol:   pulumi.String("-1"),
				CidrBlocks: pulumi.StringArray{pulumi.String("0.0.0.0/0")},
			},
		},
		Ingress: ec2.DefaultSecurityGroupIngressArray{
			&ec2.DefaultSecurityGroupIngressArgs{
				FromPort:   pulumi.Int(0),
				ToPort:     pulumi.Int(0),
				Protocol:   pulumi.String("-1"),
				CidrBlocks: pulumi.StringArray{vpc.CidrBlock},
			},
		},
		Tags: mergedTags(defaultTags(ctx, name), args.Tags),
	})
	panicOnErr(err, name+": default security group")

	// ── Per-AZ subnets + route tables ─────────────────────────────────────────
	// CIDR layout (matches SST):
	//   public  AZ i: 10.0.{8*i}.0/22
	//   private AZ i: 10.0.{8*i+4}.0/22
	pubSubnets := make([]*ec2.Subnet, numAZs)
	privSubnets := make([]*ec2.Subnet, numAZs)

	for i, az := range azNames {
		idx := i + 1

		// ── Public subnet ─────────────────────────────────────────────────────
		pubSubnet, err := ec2.NewSubnet(pctx, fmt.Sprintf("%s-pub-%d", name, idx), &ec2.SubnetArgs{
			VpcId:               vpc.ID(),
			CidrBlock:           pulumi.Sprintf("10.0.%d.0/22", 8*i),
			AvailabilityZone:    pulumi.String(az),
			MapPublicIpOnLaunch: pulumi.Bool(true),
			Tags:                mergedTags(defaultTags(ctx, fmt.Sprintf("%s-pub-%d", name, idx)), args.Tags),
		})
		panicOnErr(err, fmt.Sprintf("%s: public subnet %d", name, idx))
		pubSubnets[i] = pubSubnet

		pubRT, err := ec2.NewRouteTable(pctx, fmt.Sprintf("%s-pub-rt-%d", name, idx), &ec2.RouteTableArgs{
			VpcId: vpc.ID(),
			Routes: ec2.RouteTableRouteArray{
				&ec2.RouteTableRouteArgs{
					CidrBlock: pulumi.String("0.0.0.0/0"),
					GatewayId: igw.ID(),
				},
			},
			Tags: mergedTags(defaultTags(ctx, fmt.Sprintf("%s-pub-rt-%d", name, idx)), args.Tags),
		})
		panicOnErr(err, fmt.Sprintf("%s: public route table %d", name, idx))

		_, err = ec2.NewRouteTableAssociation(pctx, fmt.Sprintf("%s-pub-rta-%d", name, idx), &ec2.RouteTableAssociationArgs{
			SubnetId:     pubSubnet.ID(),
			RouteTableId: pubRT.ID(),
		})
		panicOnErr(err, fmt.Sprintf("%s: public route table association %d", name, idx))

		// ── Optional NAT Gateway (managed) ────────────────────────────────────
		var natGWID pulumi.IDOutput
		if nat == "managed" {
			eip, err := ec2.NewEip(pctx, fmt.Sprintf("%s-nat-eip-%d", name, idx), &ec2.EipArgs{
				Domain: pulumi.String("vpc"),
				Tags:   mergedTags(defaultTags(ctx, fmt.Sprintf("%s-nat-eip-%d", name, idx)), args.Tags),
			})
			panicOnErr(err, fmt.Sprintf("%s: NAT EIP %d", name, idx))

			natGW, err := ec2.NewNatGateway(pctx, fmt.Sprintf("%s-nat-%d", name, idx), &ec2.NatGatewayArgs{
				SubnetId:     pubSubnet.ID(),
				AllocationId: eip.ID(),
				Tags:         mergedTags(defaultTags(ctx, fmt.Sprintf("%s-nat-%d", name, idx)), args.Tags),
			})
			panicOnErr(err, fmt.Sprintf("%s: NAT gateway %d", name, idx))
			natGWID = natGW.ID()
		}

		// ── Private subnet ────────────────────────────────────────────────────
		privSubnet, err := ec2.NewSubnet(pctx, fmt.Sprintf("%s-priv-%d", name, idx), &ec2.SubnetArgs{
			VpcId:            vpc.ID(),
			CidrBlock:        pulumi.Sprintf("10.0.%d.0/22", 8*i+4),
			AvailabilityZone: pulumi.String(az),
			Tags:             mergedTags(defaultTags(ctx, fmt.Sprintf("%s-priv-%d", name, idx)), args.Tags),
		})
		panicOnErr(err, fmt.Sprintf("%s: private subnet %d", name, idx))
		privSubnets[i] = privSubnet

		privRTArgs := &ec2.RouteTableArgs{
			VpcId: vpc.ID(),
			Tags:  mergedTags(defaultTags(ctx, fmt.Sprintf("%s-priv-rt-%d", name, idx)), args.Tags),
		}
		if nat == "managed" {
			privRTArgs.Routes = ec2.RouteTableRouteArray{
				&ec2.RouteTableRouteArgs{
					CidrBlock:    pulumi.String("0.0.0.0/0"),
					NatGatewayId: natGWID,
				},
			}
		}
		privRT, err := ec2.NewRouteTable(pctx, fmt.Sprintf("%s-priv-rt-%d", name, idx), privRTArgs)
		panicOnErr(err, fmt.Sprintf("%s: private route table %d", name, idx))

		_, err = ec2.NewRouteTableAssociation(pctx, fmt.Sprintf("%s-priv-rta-%d", name, idx), &ec2.RouteTableAssociationArgs{
			SubnetId:     privSubnet.ID(),
			RouteTableId: privRT.ID(),
		})
		panicOnErr(err, fmt.Sprintf("%s: private route table association %d", name, idx))
	}

	return &Vpc{
		name:        name,
		vpc:         vpc,
		secGroup:    secGroup,
		pubSubnets:  pubSubnets,
		privSubnets: privSubnets,
		ctx:         ctx,
	}
}

// ── Accessors ─────────────────────────────────────────────────────────────────

// ID returns the VPC ID.
func (v *Vpc) ID() pulumi.StringOutput { return v.vpc.ID().ToStringOutput() }

// PublicSubnetIDs returns the IDs of all public subnets.
func (v *Vpc) PublicSubnetIDs() pulumi.StringArrayOutput {
	ids := make(pulumi.StringArray, len(v.pubSubnets))
	for i, s := range v.pubSubnets {
		ids[i] = s.ID().ToStringOutput()
	}
	return ids.ToStringArrayOutput()
}

// PrivateSubnetIDs returns the IDs of all private subnets.
func (v *Vpc) PrivateSubnetIDs() pulumi.StringArrayOutput {
	ids := make(pulumi.StringArray, len(v.privSubnets))
	for i, s := range v.privSubnets {
		ids[i] = s.ID().ToStringOutput()
	}
	return ids.ToStringArrayOutput()
}

// SecurityGroupID returns the ID of the VPC's default security group.
func (v *Vpc) SecurityGroupID() pulumi.StringOutput {
	return v.secGroup.ID().ToStringOutput()
}

// ── helpers ───────────────────────────────────────────────────────────────────

// mergedTags merges extra tags into a base tag map.
// Returns base if extra is nil/empty.
func mergedTags(base pulumi.StringMap, extra map[string]string) pulumi.StringMap {
	if len(extra) == 0 {
		return base
	}
	out := pulumi.StringMap{}
	for k, v := range base {
		out[k] = v
	}
	for k, v := range extra {
		out[k] = pulumi.String(v)
	}
	return out
}
