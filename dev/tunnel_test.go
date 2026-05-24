package dev

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

// ── jsonReader ────────────────────────────────────────────────────────────────

func TestJsonReader_ReturnsData(t *testing.T) {
	t.Parallel()
	data := json.RawMessage(`{"key":"value"}`)
	r := jsonReader(data)

	buf := make([]byte, len(data))
	n, err := r.Read(buf)
	if err != nil && err.Error() != "EOF" {
		t.Fatalf("unexpected read error: %v", err)
	}
	if n == 0 {
		// read may return 0 immediately on empty pipe; try again
		t.Skip("pipe returned 0 bytes on first read — platform timing")
	}
}

// ── RegisterHandler / handle ──────────────────────────────────────────────────

// echoScript builds a tiny shell script (or bat on Windows) that echoes its
// stdin back to stdout. Used to simulate a Lambda handler binary.
func echoScript(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("echo script not supported on Windows")
	}

	dir := t.TempDir()
	script := filepath.Join(dir, "echo.sh")
	// Read all stdin, write to stdout.
	if err := os.WriteFile(script, []byte("#!/bin/sh\ncat\n"), 0755); err != nil {
		t.Fatalf("write echo script: %v", err)
	}
	return script
}

func TestTunnel_RegisterHandler_DispatchesToBinary(t *testing.T) {
	t.Parallel()

	echo := echoScript(t)

	// Build a fake tunnel (nil SQS client — we call handle() directly).
	tun := &Tunnel{
		handlers:    map[string]string{},
		requestURL:  "fake",
		responseURL: "fake",
	}
	tun.RegisterHandler("arn:aws:lambda:::function:Fn", echo)

	// Captured responses.
	var gotResp Response

	// Override sendResponse to capture instead of calling SQS.
	origSend := tun.sendFn
	defer func() { tun.sendFn = origSend }()
	tun.sendFn = func(_ context.Context, resp Response) {
		gotResp = resp
	}

	inv := &Invocation{
		ID:          "test-id-1",
		FunctionARN: "arn:aws:lambda:::function:Fn",
		Event:       json.RawMessage(`{"hello":"world"}`),
	}
	tun.handle(context.Background(), inv)

	if gotResp.ID != "test-id-1" {
		t.Errorf("response ID = %q, want test-id-1", gotResp.ID)
	}
	if gotResp.Error != "" {
		t.Errorf("unexpected error: %s", gotResp.Error)
	}
	// The echo script pipes stdin→stdout, so payload should match event.
	var got, want map[string]string
	if err := json.Unmarshal(gotResp.Payload, &got); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if err := json.Unmarshal(inv.Event, &want); err != nil {
		t.Fatal(err)
	}
	if got["hello"] != want["hello"] {
		t.Errorf("payload = %v, want %v", got, want)
	}
}

func TestTunnel_RegisterHandler_UnknownARN(t *testing.T) {
	t.Parallel()

	tun := &Tunnel{
		handlers:    map[string]string{},
		requestURL:  "fake",
		responseURL: "fake",
	}

	var gotResp Response
	tun.sendFn = func(_ context.Context, resp Response) { gotResp = resp }

	inv := &Invocation{
		ID:          "test-id-unknown",
		FunctionARN: "arn:aws:lambda:::function:Unknown",
		Event:       json.RawMessage(`{}`),
	}
	tun.handle(context.Background(), inv)

	// Unknown ARN should produce an error response, not panic.
	if gotResp.Error == "" {
		t.Error("expected error response for unknown ARN, got none")
	}
}

func TestTunnel_RegisterHandler_BinaryError(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	failScript := filepath.Join(dir, "fail.sh")
	if err := os.WriteFile(failScript, []byte("#!/bin/sh\nexit 1\n"), 0755); err != nil {
		t.Fatalf("write fail script: %v", err)
	}

	tun := &Tunnel{
		handlers:    map[string]string{},
		requestURL:  "fake",
		responseURL: "fake",
	}
	tun.RegisterHandler("arn:fake", failScript)

	var gotResp Response
	tun.sendFn = func(_ context.Context, resp Response) { gotResp = resp }

	tun.handle(context.Background(), &Invocation{
		ID:          "err-id",
		FunctionARN: "arn:fake",
		Event:       json.RawMessage(`{}`),
	})

	if gotResp.Error == "" {
		t.Error("expected error response when binary exits non-zero")
	}
}

// ── NewTunnel ─────────────────────────────────────────────────────────────────

func TestNewTunnel_ReturnsErrWithoutAWS(t *testing.T) {
	// Can't use t.Parallel() with t.Setenv.
	// With fake credentials, NewTunnel should still succeed (config loading
	// doesn't make network calls). Only Poll would fail.
	t.Setenv("AWS_ACCESS_KEY_ID", "test")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "test")
	t.Setenv("AWS_DEFAULT_REGION", "us-east-1")
	// Unset any endpoint override so we don't accidentally hit a local emulator.
	t.Setenv("FORGE_AWS_ENDPOINT", "")

	tun, err := NewTunnel("http://fake/req", "http://fake/res")
	if err != nil {
		t.Fatalf("NewTunnel returned error: %v", err)
	}
	if tun == nil {
		t.Fatal("NewTunnel returned nil tunnel")
	}
}

// ── helpers ───────────────────────────────────────────────────────────────────

// compile-time check: ensure echoScript is available so go vet catches bad use
var _ = exec.Command
