// Package dev implements the live Lambda development tunnel.
//
// Architecture (mirrors SST's live development model):
//
//	┌──────────────────────────────────────────────────────────────────┐
//	│  AWS                                                             │
//	│  ┌────────────────┐    invoke    ┌──────────────────────────┐   │
//	│  │ Real trigger   │ ──────────► │  Stub Lambda             │   │
//	│  │ (API GW, etc.) │             │  (forge-stub binary)      │   │
//	│  └────────────────┘             │  → sends event to SQS    │   │
//	│                                 │  → polls response SQS    │   │
//	│                                 └───────────┬──────────────┘   │
//	│                      SQS (request queue)    │                  │
//	│                      SQS (response queue)   │                  │
//	└─────────────────────────────────────────────┼──────────────────┘
//	                                              │
//	                        ┌─────────────────────▼──────────────────┐
//	                        │  Local machine (forge dev)              │
//	                        │  ┌────────────────────────────────┐    │
//	                        │  │  Tunnel.Poll()                 │    │
//	                        │  │  → receives event from SQS     │    │
//	                        │  │  → runs handler locally (go)   │    │
//	                        │  │  → sends response to SQS       │    │
//	                        │  └────────────────────────────────┘    │
//	                        └────────────────────────────────────────┘
package dev

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
)

// Invocation is the message passed over SQS between stub and local runner.
type Invocation struct {
	ID          string          `json:"id"`
	FunctionARN string          `json:"functionArn"`
	Event       json.RawMessage `json:"event"`
	Context     json.RawMessage `json:"context"`
}

// Response is sent back to the stub Lambda after local execution.
type Response struct {
	ID      string          `json:"id"`
	Payload json.RawMessage `json:"payload"`
	Error   string          `json:"error,omitempty"`
}

// Tunnel polls the request SQS queue, executes handlers locally,
// and sends responses back via the response SQS queue.
type Tunnel struct {
	sqs         *sqs.Client
	requestURL  string
	responseURL string
	handlers    map[string]string // functionARN → local handler binary path

	// sendFn is called to deliver a response. Defaults to sending via SQS.
	// Override in tests to capture responses without a real SQS connection.
	sendFn func(ctx context.Context, resp Response)
}

// NewTunnel creates a Tunnel from pre-provisioned SQS queue URLs.
func NewTunnel(requestQueueURL, responseQueueURL string) (*Tunnel, error) {
	cfg, err := awsconfig.LoadDefaultConfig(context.Background())
	if err != nil {
		return nil, fmt.Errorf("load aws config: %w", err)
	}
	var clientOpts []func(*sqs.Options)
	if endpoint := os.Getenv("FORGE_AWS_ENDPOINT"); endpoint != "" {
		clientOpts = append(clientOpts, func(o *sqs.Options) {
			o.BaseEndpoint = aws.String(endpoint)
		})
	}
	t := &Tunnel{
		sqs:         sqs.NewFromConfig(cfg, clientOpts...),
		requestURL:  requestQueueURL,
		responseURL: responseQueueURL,
		handlers:    map[string]string{},
	}
	t.sendFn = t.sendViaSQS
	return t, nil
}

// RegisterHandler maps a Lambda function ARN to a local executable path.
// When an invocation arrives for this ARN, the tunnel runs the binary with
// the event piped to stdin and reads the response from stdout.
func (t *Tunnel) RegisterHandler(functionARN, binaryPath string) {
	t.handlers[functionARN] = binaryPath
}

// Poll starts the main event loop. It blocks until ctx is cancelled.
// Run this in a goroutine alongside `forge dev`.
func (t *Tunnel) Poll(ctx context.Context) error {
	fmt.Fprintln(os.Stdout, "  ↳ dev tunnel active — polling for Lambda invocations")

	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		msgs, err := t.sqs.ReceiveMessage(ctx, &sqs.ReceiveMessageInput{
			QueueUrl:            aws.String(t.requestURL),
			MaxNumberOfMessages: 5,
			WaitTimeSeconds:     20, // long poll
			VisibilityTimeout:   30,
		})
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			fmt.Fprintf(os.Stderr, "forge dev: receive error: %v\n", err)
			time.Sleep(2 * time.Second)
			continue
		}

		for _, msg := range msgs.Messages {
			var inv Invocation
			if err := json.Unmarshal([]byte(*msg.Body), &inv); err != nil {
				fmt.Fprintf(os.Stderr, "forge dev: bad invocation payload: %v\n", err)
				continue
			}

			// Delete from queue before running (we handle exactly once locally).
			t.sqs.DeleteMessage(ctx, &sqs.DeleteMessageInput{
				QueueUrl:      aws.String(t.requestURL),
				ReceiptHandle: msg.ReceiptHandle,
			})

			go t.handle(ctx, &inv)
		}
	}
}

// handle runs a single invocation locally and sends the response.
func (t *Tunnel) handle(ctx context.Context, inv *Invocation) {
	binaryPath, ok := t.handlers[inv.FunctionARN]
	if !ok {
		fmt.Fprintf(os.Stderr, "forge dev: no handler registered for %s\n", inv.FunctionARN)
		t.sendFn(ctx, Response{ID: inv.ID, Error: "no handler registered for " + inv.FunctionARN})
		return
	}

	resp := Response{ID: inv.ID}

	cmd := exec.CommandContext(ctx, binaryPath)
	cmd.Stdin = jsonReader(inv.Event)
	out, err := cmd.Output()
	if err != nil {
		resp.Error = err.Error()
		fmt.Fprintf(os.Stderr, "  ✗ [%s] error: %v\n", inv.FunctionARN, err)
	} else {
		resp.Payload = json.RawMessage(out)
		fmt.Fprintf(os.Stdout, "  ✓ [%s] invocation handled\n", inv.FunctionARN)
	}

	t.sendFn(ctx, resp)
}

// sendViaSQS is the default sendFn — publishes the response to the SQS response queue.
func (t *Tunnel) sendViaSQS(ctx context.Context, resp Response) {
	payload, _ := json.Marshal(resp)
	_, err := t.sqs.SendMessage(ctx, &sqs.SendMessageInput{
		QueueUrl:    aws.String(t.responseURL),
		MessageBody: aws.String(string(payload)),
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "forge dev: send response error: %v\n", err)
	}
}

// jsonReader is a helper that returns an *os.File-like reader for a JSON value.
// We write to a temp file because exec.Cmd.Stdin requires an io.Reader.
func jsonReader(data json.RawMessage) *os.File {
	r, w, _ := os.Pipe()
	go func() {
		w.Write(data)
		w.Close()
	}()
	return r
}
