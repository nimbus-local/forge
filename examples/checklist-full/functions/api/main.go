// Lambda handler for the checklist API.
// Called only by the Next.js server via API Gateway — not intended for direct
// browser access. Requests must include x-internal-key (matching SST_SECRET_INTERNAL_KEY)
// and x-user-id (set by the Next.js server after verifying the Auth.js session).
package main

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/google/uuid"
)

var (
	ddb         *dynamodb.Client
	tableName   = os.Getenv("SST_TABLE_ITEMS_NAME")
	internalKey = os.Getenv("SST_SECRET_INTERNAL_KEY")
)

func init() {
	cfg, err := awsconfig.LoadDefaultConfig(context.Background())
	if err != nil {
		panic(err)
	}
	ddb = dynamodb.NewFromConfig(cfg)
}

type Item struct {
	UserID    string `json:"userId"    dynamodbav:"userId"`
	ItemID    string `json:"itemId"    dynamodbav:"itemId"`
	Text      string `json:"text"      dynamodbav:"text"`
	Done      bool   `json:"done"      dynamodbav:"done"`
	CreatedAt string `json:"createdAt" dynamodbav:"createdAt"`
}

func handle(ctx context.Context, req events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
	if req.Headers["x-internal-key"] != internalKey {
		return jsonResp(http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
	}

	userId := req.Headers["x-user-id"]
	if userId == "" {
		return jsonResp(http.StatusBadRequest, map[string]string{"error": "x-user-id header required"})
	}

	method := req.RequestContext.HTTP.Method
	rawPath := req.RawPath

	switch {
	case method == http.MethodGet && rawPath == "/items":
		return listItems(ctx, userId)
	case method == http.MethodPost && rawPath == "/items":
		return createItem(ctx, userId, req.Body)
	default:
		id := req.PathParameters["id"]
		if id == "" {
			return jsonResp(http.StatusNotFound, map[string]string{"error": "not found"})
		}
		switch method {
		case http.MethodPatch:
			return updateItem(ctx, userId, id, req.Body)
		case http.MethodDelete:
			return deleteItem(ctx, userId, id)
		}
		return jsonResp(http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}

func listItems(ctx context.Context, userId string) (events.APIGatewayV2HTTPResponse, error) {
	out, err := ddb.Query(ctx, &dynamodb.QueryInput{
		TableName:              aws.String(tableName),
		KeyConditionExpression: aws.String("userId = :uid"),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":uid": &types.AttributeValueMemberS{Value: userId},
		},
	})
	if err != nil {
		return jsonResp(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}

	var items []Item
	if err := attributevalue.UnmarshalListOfMaps(out.Items, &items); err != nil {
		return jsonResp(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}

	sort.Slice(items, func(i, j int) bool {
		return items[i].CreatedAt > items[j].CreatedAt
	})

	return jsonResp(http.StatusOK, items)
}

func createItem(ctx context.Context, userId, body string) (events.APIGatewayV2HTTPResponse, error) {
	var payload struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal([]byte(body), &payload); err != nil || strings.TrimSpace(payload.Text) == "" {
		return jsonResp(http.StatusBadRequest, map[string]string{"error": "text is required"})
	}

	item := Item{
		UserID:    userId,
		ItemID:    uuid.New().String(),
		Text:      strings.TrimSpace(payload.Text),
		Done:      false,
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	}

	av, err := attributevalue.MarshalMap(item)
	if err != nil {
		return jsonResp(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}

	_, err = ddb.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: aws.String(tableName),
		Item:      av,
	})
	if err != nil {
		return jsonResp(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}

	return jsonResp(http.StatusCreated, item)
}

func updateItem(ctx context.Context, userId, itemId, body string) (events.APIGatewayV2HTTPResponse, error) {
	var payload struct {
		Done bool `json:"done"`
	}
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		return jsonResp(http.StatusBadRequest, map[string]string{"error": "invalid body"})
	}

	out, err := ddb.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName: aws.String(tableName),
		Key: map[string]types.AttributeValue{
			"userId": &types.AttributeValueMemberS{Value: userId},
			"itemId": &types.AttributeValueMemberS{Value: itemId},
		},
		UpdateExpression:         aws.String("SET #d = :done"),
		ExpressionAttributeNames: map[string]string{"#d": "done"},
		ConditionExpression:      aws.String("userId = :uid"),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":done": &types.AttributeValueMemberBOOL{Value: payload.Done},
			":uid":  &types.AttributeValueMemberS{Value: userId},
		},
		ReturnValues: types.ReturnValueAllNew,
	})
	if err != nil {
		return jsonResp(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}

	var item Item
	if err := attributevalue.UnmarshalMap(out.Attributes, &item); err != nil {
		return jsonResp(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}

	return jsonResp(http.StatusOK, item)
}

func deleteItem(ctx context.Context, userId, itemId string) (events.APIGatewayV2HTTPResponse, error) {
	_, err := ddb.DeleteItem(ctx, &dynamodb.DeleteItemInput{
		TableName: aws.String(tableName),
		Key: map[string]types.AttributeValue{
			"userId": &types.AttributeValueMemberS{Value: userId},
			"itemId": &types.AttributeValueMemberS{Value: itemId},
		},
		ConditionExpression: aws.String("userId = :uid"),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":uid": &types.AttributeValueMemberS{Value: userId},
		},
	})
	if err != nil {
		return jsonResp(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}

	return events.APIGatewayV2HTTPResponse{StatusCode: http.StatusNoContent}, nil
}

func jsonResp(statusCode int, body any) (events.APIGatewayV2HTTPResponse, error) {
	b, err := json.Marshal(body)
	if err != nil {
		return events.APIGatewayV2HTTPResponse{StatusCode: http.StatusInternalServerError}, err
	}
	return events.APIGatewayV2HTTPResponse{
		StatusCode: statusCode,
		Headers:    map[string]string{"Content-Type": "application/json"},
		Body:       string(b),
	}, nil
}

func main() {
	lambda.Start(handle)
}
