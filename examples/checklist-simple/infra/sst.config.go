package main

import (
	"fmt"

	forge "github.com/nimbus-local/forge"
	"github.com/nimbus-local/forge/constructs"
	"github.com/pulumi/pulumi-aws/sdk/v7/go/aws/iam"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

func main() {
	forge.Run(&forge.Config{
		App: &forge.AppConfig{
			Name: "checklist-simple",
			Home: "aws",
		},
		Run: func(ctx *forge.RunContext) error {
			// ── Storage ───────────────────────────────────────────────────────

			table := constructs.NewDynamoDB(ctx, "Items", &constructs.DynamoDBArgs{
				Fields: map[string]constructs.FieldType{
					"userId": constructs.FieldTypeString,
					"itemId": constructs.FieldTypeString,
				},
				PrimaryIndex: &constructs.PrimaryIndex{
					HashKey:  "userId",
					RangeKey: "itemId",
				},
				// Enable point-in-time recovery on protected stages.
				PointInTimeRecovery: ctx.Stage == "production",
			})

			// ── Site ─────────────────────────────────────────────────────────

			// Link injects SST_TABLE_ITEMS_NAME and SST_TABLE_ITEMS_ARN as
			// environment variables into the Next.js server Lambda.
			site := constructs.NewNextjsSite(ctx, "Web", &constructs.NextjsSiteArgs{
				Path: "../web",
				Link: []forge.Linkable{table},
			})

			// forge Link injects env vars but does not automatically grant IAM
			// permissions — attach a scoped policy to the SSR Lambda role.
			_, err := iam.NewRolePolicy(ctx.Pulumi(), "WebDynamo", &iam.RolePolicyArgs{
				Role: site.Role().Name,
				Policy: table.ARN().ApplyT(func(arn string) (string, error) {
					return fmt.Sprintf(`{
						"Version": "2012-10-17",
						"Statement": [{
							"Effect": "Allow",
							"Action": [
								"dynamodb:GetItem",
								"dynamodb:PutItem",
								"dynamodb:UpdateItem",
								"dynamodb:DeleteItem",
								"dynamodb:Query"
							],
							"Resource": ["%s", "%s/index/*"]
						}]
					}`, arn, arn), nil
				}).(pulumi.StringOutput),
			})
			if err != nil {
				return err
			}

			// ── Outputs ───────────────────────────────────────────────────────

			ctx.Export("url", site.URL())
			return nil
		},
	})
}
