// Lambda resolver for the appsync-notes example.
// Receives AppSync invocations via VTL that wraps $context.arguments into a
// {"field":"...","args":{...}} envelope so this single binary handles all
// four schema fields.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

var (
	ddb       *dynamodb.Client
	tableName = os.Getenv("SST_TABLE_NOTES_NAME")
)

func init() {
	cfg, err := awsconfig.LoadDefaultConfig(context.Background())
	if err != nil {
		panic(err)
	}
	ddb = dynamodb.NewFromConfig(cfg)
}

// Event is the payload forwarded by the AppSync VTL request template:
//
//	{"version":"2017-02-28","operation":"Invoke","payload":{"field":"...","args":{...}}}
type Event struct {
	Field string          `json:"field"`
	Args  json.RawMessage `json:"args"`
}

// Note is the GraphQL Note type backed by DynamoDB.
type Note struct {
	ID        string `json:"id"        dynamodbav:"id"`
	Content   string `json:"content"   dynamodbav:"content"`
	CreatedAt string `json:"createdAt" dynamodbav:"createdAt"`
}

func handler(ctx context.Context, event Event) (any, error) {
	switch event.Field {
	case "createNote":
		return createNote(ctx, event.Args)
	case "getNote":
		return getNote(ctx, event.Args)
	case "listNotes":
		return listNotes(ctx)
	case "deleteNote":
		return deleteNote(ctx, event.Args)
	default:
		return nil, fmt.Errorf("unknown field: %s", event.Field)
	}
}

func createNote(ctx context.Context, raw json.RawMessage) (*Note, error) {
	var args struct {
		ID      string `json:"id"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, fmt.Errorf("createNote: %w", err)
	}
	note := Note{
		ID:        args.ID,
		Content:   args.Content,
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	}
	av, err := attributevalue.MarshalMap(note)
	if err != nil {
		return nil, err
	}
	_, err = ddb.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: aws.String(tableName),
		Item:      av,
	})
	return &note, err
}

func getNote(ctx context.Context, raw json.RawMessage) (*Note, error) {
	var args struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, fmt.Errorf("getNote: %w", err)
	}
	out, err := ddb.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: aws.String(tableName),
		Key: map[string]types.AttributeValue{
			"id": &types.AttributeValueMemberS{Value: args.ID},
		},
	})
	if err != nil {
		return nil, err
	}
	if out.Item == nil {
		return nil, nil
	}
	var note Note
	if err := attributevalue.UnmarshalMap(out.Item, &note); err != nil {
		return nil, err
	}
	return &note, nil
}

func listNotes(ctx context.Context) ([]Note, error) {
	out, err := ddb.Scan(ctx, &dynamodb.ScanInput{
		TableName: aws.String(tableName),
	})
	if err != nil {
		return nil, err
	}
	var notes []Note
	if err := attributevalue.UnmarshalListOfMaps(out.Items, &notes); err != nil {
		return nil, err
	}
	if notes == nil {
		notes = []Note{}
	}
	return notes, nil
}

func deleteNote(ctx context.Context, raw json.RawMessage) (bool, error) {
	var args struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return false, fmt.Errorf("deleteNote: %w", err)
	}
	_, err := ddb.DeleteItem(ctx, &dynamodb.DeleteItemInput{
		TableName: aws.String(tableName),
		Key: map[string]types.AttributeValue{
			"id": &types.AttributeValueMemberS{Value: args.ID},
		},
	})
	return err == nil, err
}

func main() {
	lambda.Start(handler)
}
