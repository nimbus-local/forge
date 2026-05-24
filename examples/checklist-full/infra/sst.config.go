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
			Name: "checklist-full",
			Home: "aws",
		},
		Stages: map[string]*forge.StageConfig{
			"production": {
				Protected: true,
			},
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
				PointInTimeRecovery: ctx.Stage == "production",
				DeletionProtection:  ctx.Stage == "production",
			})

			// ── Secrets ───────────────────────────────────────────────────────
			// Set these before the first deploy:
			//   forge secret set GithubId       <github-oauth-client-id>
			//   forge secret set GithubSecret   <github-oauth-client-secret>
			//   forge secret set NextauthSecret <random-32+-char-string>
			//   forge secret set InternalKey    <random-32+-char-string>
			//
			// GitHub OAuth app: set the Authorization callback URL to
			//   <url output from forge deploy>/api/auth/callback/github

			githubId := constructs.NewSecret(ctx, "GithubId", nil)
			githubSecret := constructs.NewSecret(ctx, "GithubSecret", nil)
			nextauthSecret := constructs.NewSecret(ctx, "NextauthSecret", nil)
			internalKey := constructs.NewSecret(ctx, "InternalKey", nil)

			// ── Go API Lambda ─────────────────────────────────────────────────
			// Build the binary before deploying: make build
			// The Makefile produces functions/api.zip containing the bootstrap binary.

			fn := constructs.NewFunction(ctx, "Handler", &constructs.FunctionArgs{
				Handler: "bootstrap",
				Code:    "../functions/api.zip",
				Link:    []forge.Linkable{table, internalKey},
			})

			// Grant the Lambda DynamoDB read/write access.
			// forge Link injects env vars but does not automatically grant IAM permissions.
			_, err := iam.NewRolePolicy(ctx.Pulumi(), "HandlerDynamo", &iam.RolePolicyArgs{
				Role: fn.Role().Name,
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

			// ── API Gateway ───────────────────────────────────────────────────

			api := constructs.NewApiGatewayV2(ctx, "Gateway", nil)
			api.Route("GET /items", &constructs.RouteArgs{Function: fn})
			api.Route("POST /items", &constructs.RouteArgs{Function: fn})
			api.Route("PATCH /items/{id}", &constructs.RouteArgs{Function: fn})
			api.Route("DELETE /items/{id}", &constructs.RouteArgs{Function: fn})

			// ── Next.js site ──────────────────────────────────────────────────
			// Link injects:
			//   SST_API_GATEWAY_URL       — Go API base URL
			//   SST_SECRET_INTERNAL_KEY   — shared secret for server-to-Lambda auth
			//   SST_SECRET_NEXTAUTH_SECRET — Auth.js signing secret
			//   SST_SECRET_GITHUB_ID      — GitHub OAuth client ID
			//   SST_SECRET_GITHUB_SECRET  — GitHub OAuth client secret

			// NewNextjsSite automatically copies Host → x-forwarded-host via a CloudFront
			// viewer-request function, so next-auth derives the correct public URL from
			// the request without a hardcoded NEXTAUTH_URL.
			//
			// GitHub OAuth app setup:
			//   Homepage URL:              <url output from forge deploy>
			//   Authorization callback URL: <url output>/api/auth/callback/github
			site := constructs.NewNextjsSite(ctx, "Web", &constructs.NextjsSiteArgs{
				Path: "../web",
				Link: []forge.Linkable{api, internalKey, nextauthSecret, githubId, githubSecret},
			})

			// ── Outputs ───────────────────────────────────────────────────────

			ctx.Export("url", site.URL())
			ctx.Export("apiUrl", api.URL())
			return nil
		},
	})
}
