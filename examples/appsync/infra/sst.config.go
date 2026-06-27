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
			Name: "appsync-notes",
			Home: "aws",
		},
		Run: func(ctx *forge.RunContext) error {
			// ── Storage ───────────────────────────────────────────────────────

			table := constructs.NewDynamoDB(ctx, "Notes", &constructs.DynamoDBArgs{
				Fields: map[string]constructs.FieldType{
					"id": constructs.FieldTypeString,
				},
				PrimaryIndex: &constructs.PrimaryIndex{
					HashKey: "id",
				},
			})

			// ── Lambda resolver ───────────────────────────────────────────────
			// Build the binary before deploying: make build
			// Produces functions/resolver.zip containing the bootstrap binary.

			fn := constructs.NewFunction(ctx, "Resolver", &constructs.FunctionArgs{
				Handler: "bootstrap",
				Code:    "../functions/resolver.zip",
				Link:    []forge.Linkable{table},
			})

			// Link injects SST_TABLE_NOTES_NAME but does not grant IAM permissions.
			_, err := iam.NewRolePolicy(ctx.Pulumi(), "ResolverDynamo", &iam.RolePolicyArgs{
				Role: fn.Role().Name,
				Policy: table.ARN().ApplyT(func(arn string) (string, error) {
					return fmt.Sprintf(`{
						"Version": "2012-10-17",
						"Statement": [{
							"Effect": "Allow",
							"Action": [
								"dynamodb:GetItem",
								"dynamodb:PutItem",
								"dynamodb:DeleteItem",
								"dynamodb:Scan"
							],
							"Resource": "%s"
						}]
					}`, arn), nil
				}).(pulumi.StringOutput),
			})
			if err != nil {
				return err
			}

			// ── AppSync GraphQL API ───────────────────────────────────────────
			// Each resolver uses a custom VTL template that wraps the field name
			// and arguments into a single {"field":"...","args":{...}} envelope
			// so the single Resolver Lambda can dispatch based on the field name.

			vtl := func(field string) string {
				return fmt.Sprintf(
					`{"version":"2017-02-28","operation":"Invoke","payload":{"field":"%s","args":$util.toJson($context.arguments)}}`,
					field,
				)
			}

			gql := constructs.NewAppSync(ctx, "Graph", &constructs.AppSyncArgs{
				Schema: `
schema { query: Query  mutation: Mutation }

type Note {
  id: ID!
  content: String!
  createdAt: String!
}

type Query {
  getNote(id: ID!): Note
  listNotes: [Note!]!
}

type Mutation {
  createNote(id: ID!, content: String!): Note
  deleteNote(id: ID!): Boolean
}
`,
				ApiKeyExpiry: "2027-01-01T00:00:00Z",
				DataSources: []constructs.AppSyncDataSource{
					{Name: "NotesDS", Type: constructs.AppSyncDataSourceLambda, Function: fn},
				},
				Resolvers: []constructs.AppSyncResolver{
					{TypeName: "Query", FieldName: "getNote", DataSource: "NotesDS",
						RequestTemplate: vtl("getNote")},
					{TypeName: "Query", FieldName: "listNotes", DataSource: "NotesDS",
						RequestTemplate: vtl("listNotes")},
					{TypeName: "Mutation", FieldName: "createNote", DataSource: "NotesDS",
						RequestTemplate: vtl("createNote")},
					{TypeName: "Mutation", FieldName: "deleteNote", DataSource: "NotesDS",
						RequestTemplate: vtl("deleteNote")},
				},
			})

			// ── Outputs ───────────────────────────────────────────────────────

			ctx.Export("apiUrl", gql.URL())
			ctx.Export("apiKey", gql.APIKey())
			return nil
		},
	})
}
