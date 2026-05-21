// forge-stub is the thin proxy Lambda binary deployed by `forge dev`.
//
// On each invocation it publishes the event to the SQS request queue, then
// polls the response queue until it receives the matching reply or times out.
// The local `forge dev` tunnel receives events from the request queue, runs
// the real handler binary, and posts the result to the response queue.
//
// Required environment variables (set by forge dev at deploy time):
//
//	FORGE_REQUEST_QUEUE_URL   SQS URL for outbound invocation events
//	FORGE_RESPONSE_QUEUE_URL  SQS URL for inbound handler responses
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-lambda-go/lambdacontext"
	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/google/uuid"

	"github.com/sst-go/forge/dev"
)

const (
	// stubTimeout is the maximum time the stub waits for a local response.
	// Lambda's maximum timeout is 900s; we leave 1s headroom for overhead.
	stubTimeout = 29 * time.Second

	// responseVisibilityTimeout is the visibility duration claimed when polling
	// the response queue. Short so non-matching messages become available quickly.
	responseVisibilityTimeout = 5
)

var (
	sqsClient   *sqs.Client
	requestURL  string
	responseURL string
)

func main() {
	requestURL = os.Getenv("FORGE_REQUEST_QUEUE_URL")
	responseURL = os.Getenv("FORGE_RESPONSE_QUEUE_URL")
	if requestURL == "" || responseURL == "" {
		fmt.Fprintln(os.Stderr, "forge-stub: FORGE_REQUEST_QUEUE_URL and FORGE_RESPONSE_QUEUE_URL must be set")
		os.Exit(1)
	}

	cfg, err := awsconfig.LoadDefaultConfig(context.Background())
	if err != nil {
		fmt.Fprintf(os.Stderr, "forge-stub: load aws config: %v\n", err)
		os.Exit(1)
	}
	sqsClient = sqs.NewFromConfig(cfg)

	lambda.Start(handle)
}

// handle is the Lambda handler. It proxies each invocation through SQS to the
// local forge dev tunnel and returns the tunnel's response.
func handle(ctx context.Context, event json.RawMessage) (json.RawMessage, error) {
	id := uuid.New().String()

	// Capture invoked function ARN from the Lambda context.
	functionARN := ""
	if lc, ok := lambdacontext.FromContext(ctx); ok {
		functionARN = lc.InvokedFunctionArn
	}

	// Serialize the Lambda context so local handlers can inspect request ID etc.
	contextBytes, _ := json.Marshal(contextPayload(ctx))

	inv := dev.Invocation{
		ID:          id,
		FunctionARN: functionARN,
		Event:       event,
		Context:     json.RawMessage(contextBytes),
	}
	body, err := json.Marshal(inv)
	if err != nil {
		return nil, fmt.Errorf("forge-stub: marshal invocation: %w", err)
	}

	_, err = sqsClient.SendMessage(ctx, &sqs.SendMessageInput{
		QueueUrl:    aws.String(requestURL),
		MessageBody: aws.String(string(body)),
	})
	if err != nil {
		return nil, fmt.Errorf("forge-stub: send to request queue: %w", err)
	}

	return pollResponse(ctx, id)
}

// pollResponse polls the response queue until the reply for id arrives or the
// deadline expires. Non-matching messages are made immediately re-visible so
// other concurrent stub invocations can pick them up.
func pollResponse(ctx context.Context, id string) (json.RawMessage, error) {
	deadline := time.Now().Add(stubTimeout)

	for time.Now().Before(deadline) {
		remaining := time.Until(deadline)
		waitSeconds := int32(min(20, int(remaining.Seconds())))
		if waitSeconds <= 0 {
			break
		}

		msgs, err := sqsClient.ReceiveMessage(ctx, &sqs.ReceiveMessageInput{
			QueueUrl:            aws.String(responseURL),
			MaxNumberOfMessages: 1,
			WaitTimeSeconds:     waitSeconds,
			VisibilityTimeout:   responseVisibilityTimeout,
		})
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, fmt.Errorf("forge-stub: poll response queue: %w", err)
		}

		for _, msg := range msgs.Messages {
			var resp dev.Response
			if jsonErr := json.Unmarshal([]byte(*msg.Body), &resp); jsonErr != nil {
				// Unreadable message — leave it to expire.
				continue
			}

			if resp.ID != id {
				// Belongs to another concurrent invocation — release immediately.
				sqsClient.ChangeMessageVisibility(ctx, &sqs.ChangeMessageVisibilityInput{ //nolint:errcheck
					QueueUrl:          aws.String(responseURL),
					ReceiptHandle:     msg.ReceiptHandle,
					VisibilityTimeout: 0,
				})
				continue
			}

			// Found our response — delete and return.
			sqsClient.DeleteMessage(ctx, &sqs.DeleteMessageInput{ //nolint:errcheck
				QueueUrl:      aws.String(responseURL),
				ReceiptHandle: msg.ReceiptHandle,
			})

			if resp.Error != "" {
				return nil, fmt.Errorf("%s", resp.Error)
			}
			return resp.Payload, nil
		}
	}

	return nil, fmt.Errorf("forge-stub: timed out waiting for local response (id=%s)", id)
}

// contextPayload extracts serialisable fields from the Lambda context.
func contextPayload(ctx context.Context) any {
	p := struct {
		RequestID          string `json:"requestId"`
		InvokedFunctionARN string `json:"invokedFunctionArn"`
		FunctionName       string `json:"functionName"`
		FunctionVersion    string `json:"functionVersion"`
	}{
		FunctionName:    os.Getenv("AWS_LAMBDA_FUNCTION_NAME"),
		FunctionVersion: os.Getenv("AWS_LAMBDA_FUNCTION_VERSION"),
	}
	if lc, ok := lambdacontext.FromContext(ctx); ok {
		p.RequestID = lc.AwsRequestID
		p.InvokedFunctionARN = lc.InvokedFunctionArn
	}
	return p
}
