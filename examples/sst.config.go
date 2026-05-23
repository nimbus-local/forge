// Example infra/sst.config.go
// This is what a typical forge project looks like.
// Run: cd infra && go mod tidy  then  forge deploy
package main

import (
	"github.com/nimbus-local/forge"
	"github.com/nimbus-local/forge/constructs"
)

func main() {
	forge.Run(&forge.Config{
		App: &forge.AppConfig{
			Name: "todo-app",
			Home: "aws",
			// Retain resources in production, destroy in all other stages.
			// This is equivalent to SST's: removal: input?.stage === "production" ? "retain" : "remove"
			Removal: func() forge.RemovalPolicy {
				// Evaluated at deploy time; ctx.Stage is set by the CLI.
				return forge.RemovalDestroy // override per stage in your own logic
			}(),
		},

		Run: func(ctx *forge.RunContext) error {
			// ── Storage ───────────────────────────────────────────────────────

			table := constructs.NewDynamoDB(ctx, "TodoTable", &constructs.DynamoDBArgs{
				Fields: map[string]constructs.FieldType{
					"pk": constructs.FieldTypeString,
					"sk": constructs.FieldTypeString,
				},
				PrimaryIndex: &constructs.PrimaryIndex{
					HashKey:  "pk",
					RangeKey: "sk",
				},
				PointInTimeRecovery: ctx.Stage == "production",
				DeletionProtection:  ctx.Stage == "production",
			})

			uploads := constructs.NewBucket(ctx, "UploadsBucket", &constructs.BucketArgs{
				CORS: true,
			})

			// ── API ───────────────────────────────────────────────────────────

			api := constructs.NewApiGatewayV2(ctx, "TodoApi", nil)

			api.Route("GET /todos", &constructs.RouteArgs{
				Handler: "functions/list/main.handler",
				Link:    []forge.Linkable{table},
			})

			api.Route("POST /todos", &constructs.RouteArgs{
				Handler: "functions/create/main.handler",
				Link:    []forge.Linkable{table},
			})

			api.Route("DELETE /todos/{id}", &constructs.RouteArgs{
				Handler: "functions/delete/main.handler",
				Link:    []forge.Linkable{table},
			})

			api.Route("POST /upload", &constructs.RouteArgs{
				Handler: "functions/upload/main.handler",
				Link:    []forge.Linkable{table, uploads},
				Timeout: 30,
			})

			// ── Exports ───────────────────────────────────────────────────────
			// These appear in `forge deploy` output and can be read by your app.

			ctx.Export("apiUrl", api.URL())
			ctx.Export("tableName", table.TableName())
			ctx.Export("uploadsBucket", uploads.Name())

			return nil
		},
	})
}
