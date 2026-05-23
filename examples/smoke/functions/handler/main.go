// Smoke-test Lambda handler.
// Handles every trigger type that the smoke infra wires up:
//   - API Gateway v2 HTTP → returns 200 with all SST_* env vars as JSON
//   - SQS, SNS, EventBridge → logs the event payload and returns nil
//
// A single binary is deployed for all Lambda functions in the smoke stack
// (Handler, Queue consumer, Topic subscriber, Cron job).
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
)

func main() {
	lambda.Start(dispatch)
}

// dispatch routes to the correct handler based on what fields are present in the raw event.
func dispatch(ctx context.Context, raw json.RawMessage) (interface{}, error) {
	// Try API Gateway v2 first (has "requestContext" with "http" sub-key).
	var apigw events.APIGatewayV2HTTPRequest
	if err := json.Unmarshal(raw, &apigw); err == nil && apigw.RequestContext.HTTP.Method != "" {
		return handleHTTP(ctx, apigw)
	}

	// SQS (has "Records" with "eventSource": "aws:sqs").
	var sqsEvt events.SQSEvent
	if err := json.Unmarshal(raw, &sqsEvt); err == nil && len(sqsEvt.Records) > 0 && sqsEvt.Records[0].EventSource == "aws:sqs" {
		return nil, handleSQS(ctx, sqsEvt)
	}

	// SNS (has "Records" with "EventSource": "aws:sns").
	var snsEvt events.SNSEvent
	if err := json.Unmarshal(raw, &snsEvt); err == nil && len(snsEvt.Records) > 0 && snsEvt.Records[0].EventSource == "aws:sns" {
		return nil, handleSNS(ctx, snsEvt)
	}

	// EventBridge / CloudWatch Events (has "source" and "detail-type").
	var eb map[string]interface{}
	if err := json.Unmarshal(raw, &eb); err == nil {
		if _, hasSource := eb["source"]; hasSource {
			fmt.Printf("smoke: eventbridge event: %s\n", raw)
			return nil, nil
		}
	}

	// Unknown — log and succeed.
	fmt.Printf("smoke: unknown event: %s\n", raw)
	return nil, nil
}

func handleHTTP(_ context.Context, req events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
	// Collect all SST_* env vars to confirm link injection worked.
	envVars := map[string]string{}
	for _, env := range os.Environ() {
		if strings.HasPrefix(env, "SST_") {
			parts := strings.SplitN(env, "=", 2)
			if len(parts) == 2 {
				envVars[parts[0]] = parts[1]
			}
		}
	}

	body, _ := json.Marshal(map[string]interface{}{
		"ok":      true,
		"path":    req.RawPath,
		"method":  req.RequestContext.HTTP.Method,
		"sst_env": envVars,
	})

	return events.APIGatewayV2HTTPResponse{
		StatusCode: http.StatusOK,
		Headers:    map[string]string{"Content-Type": "application/json"},
		Body:       string(body),
	}, nil
}

func handleSQS(_ context.Context, evt events.SQSEvent) error {
	for _, record := range evt.Records {
		fmt.Printf("smoke: sqs message from %s: %s\n", record.EventSourceARN, record.Body)
	}
	return nil
}

func handleSNS(_ context.Context, evt events.SNSEvent) error {
	for _, record := range evt.Records {
		fmt.Printf("smoke: sns message subject=%q message=%s\n", record.SNS.Subject, record.SNS.Message)
	}
	return nil
}
