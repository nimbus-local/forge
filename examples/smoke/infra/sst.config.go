// Smoke-test stack that exercises every forge construct.
//
// Build the handler binary before deploying:
//
//	make build
//
// Deploy:
//
//	make deploy
//
// After deploying, hit the API Gateway URL to verify link injection:
//
//	curl $(forge diff --stage <stage> | grep url) /
//
// Expected response: 200 JSON with all SST_* env vars populated.
package main

import (
	forge "github.com/nimbus-local/forge"
	"github.com/nimbus-local/forge/constructs"
)

func main() {
	forge.Run(&forge.Config{
		App: &forge.AppConfig{
			Name: "forge-smoke",
			Home: "aws",
		},
		Run: func(ctx *forge.RunContext) error {
			// ── KMS key (encrypts all storage + the handler function) ─────────

			key := constructs.NewKMSKey(ctx, "SmokeKey", nil)

			// ── Storage ───────────────────────────────────────────────────────

			table := constructs.NewDynamoDB(ctx, "Records", &constructs.DynamoDBArgs{
				Fields: map[string]constructs.FieldType{
					"pk": constructs.FieldTypeString,
					"sk": constructs.FieldTypeString,
				},
				PrimaryIndex: &constructs.PrimaryIndex{
					HashKey:  "pk",
					RangeKey: "sk",
				},
				KMSKeyArn: key.ARN(),
			})

			bucket := constructs.NewBucket(ctx, "Assets", &constructs.BucketArgs{
				KMSKeyArn:     key.ARN(),
				LifecycleDays: 90,
			})

			// ── Kinesis stream (created before handlerArgs so it can be linked) ─

			stream := constructs.NewKinesisStream(ctx, "Stream", nil)

			// ── Secrets ───────────────────────────────────────────────────────
			// Override with a real value for production:
			//   forge secret set AppSecret <value>

			secret := constructs.NewSecret(ctx, "AppSecret", &constructs.SecretArgs{
				Default: "smoke-default",
			})

			// ── Handler function args (reused by Queue, Topic, and Cron) ──────
			// Each construct that receives handlerArgs creates its own Lambda function.

			handlerArgs := &constructs.FunctionArgs{
				Handler:          "bootstrap",
				Code:             "../functions/handler.zip",
				DevHandler:       "./functions/handler",
				Link:             []forge.Linkable{table, bucket, secret, key, stream},
				KMSKeyArn:        key.ARN(),
				LogRetentionDays: 30,
			}

			// ── API function ──────────────────────────────────────────────────

			fn := constructs.NewFunction(ctx, "Handler", handlerArgs)

			api := constructs.NewApiGatewayV2(ctx, "Api", nil)
			api.Route("GET /", &constructs.RouteArgs{Function: fn})
			api.Route("GET /health", &constructs.RouteArgs{Function: fn})

			// ── Queue with consumer ───────────────────────────────────────────

			queue := constructs.NewQueue(ctx, "Events", &constructs.QueueArgs{
				Consumer:          handlerArgs,
				VisibilityTimeout: 30,
				DeadLetterQueue:   true,
				KMSKeyArn:         key.ARN(),
			})

			// ── Topic with subscriber ─────────────────────────────────────────

			topic := constructs.NewTopic(ctx, "Alerts", &constructs.TopicArgs{
				Subscribers: []*constructs.FunctionArgs{handlerArgs},
				KMSKeyArn:   key.ARN(),
			})

			// ── Step Functions state machine ──────────────────────────────────

			sfn := constructs.NewStepFunctions(ctx, "Workflow", &constructs.StepFunctionsArgs{
				Definition: `{
					"StartAt": "Done",
					"States": {
						"Done": {"Type": "Succeed"}
					}
				}`,
			})

			// ── Cron job (every 5 minutes) ────────────────────────────────────

			constructs.NewCron(ctx, "Heartbeat", &constructs.CronArgs{
				Schedule: "rate(5 minutes)",
				Job:      handlerArgs,
			})

			// ── Service (ECS Fargate) ─────────────────────────────────────────
			// Uncomment and fill in your VPC details to exercise NewService.
			// Requires an existing VPC with at least one subnet.
			//
			// constructs.NewService(ctx, "Web", &constructs.ServiceArgs{
			// 	Image:  "nginx:1.27-alpine",
			// 	CPU:    256,
			// 	Memory: 512,
			// 	Port:   80,
			// 	VPC: &constructs.ServiceVPCArgs{
			// 		VPCID:     "vpc-xxxxxxxx",
			// 		SubnetIDs: []string{"subnet-xxxxxxxx"},
			// 	},
			// })

			// ── Outputs ───────────────────────────────────────────────────────

			ctx.Export("apiUrl", api.URL())
			ctx.Export("queueUrl", queue.URL())
			ctx.Export("topicArn", topic.ARN())
			ctx.Export("bucketName", bucket.Name())
			ctx.Export("tableName", table.TableName())
			ctx.Export("kmsKeyArn", key.ARN())
			ctx.Export("streamName", stream.Name())
			ctx.Export("sfnArn", sfn.ARN())
			return nil
		},
	})
}
