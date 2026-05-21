//go:build e2e

// Package e2e tests the forge CLI binary end-to-end.
// Build the binary first, then run with: go test ./test/e2e/... -tags e2e
// The tests expect the forge binary to be in PATH or at ./forge.
package e2e

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// ── helpers ───────────────────────────────────────────────────────────────────

// forgeBin returns the path to the forge binary to test.
// Looks for ./forge (built binary) first, then falls back to PATH.
func forgeBin(t *testing.T) string {
	t.Helper()

	// Look for binary relative to the repo root.
	_, thisFile, _, _ := runtime.Caller(0)
	repoRoot := filepath.Join(filepath.Dir(thisFile), "..", "..")
	candidate := filepath.Join(repoRoot, "forge")
	if runtime.GOOS == "windows" {
		candidate += ".exe"
	}
	if _, err := os.Stat(candidate); err == nil {
		return candidate
	}

	// Fall back to PATH.
	bin, err := exec.LookPath("forge")
	if err != nil {
		t.Skip("forge binary not found — build with `go build ./cmd/forge` first")
	}
	return bin
}

func runForge(t *testing.T, args ...string) (string, int) {
	t.Helper()
	cmd := exec.Command(forgeBin(t), args...)
	out, err := cmd.CombinedOutput()
	code := 0
	if exitErr, ok := err.(*exec.ExitError); ok {
		code = exitErr.ExitCode()
	}
	return string(out), code
}

// ── tests ─────────────────────────────────────────────────────────────────────

// TestForgeDeploy verifies that `forge deploy` prints a helpful error (not a
// panic) when no sst.config.go is present.
func TestForgeDeploy(t *testing.T) {
	t.Parallel()
	out, code := runForge(t, "deploy")
	if code == 0 {
		t.Error("forge deploy with no config should exit non-zero")
	}
	// Should print a human-readable message, not a raw panic.
	if strings.Contains(out, "panic:") {
		t.Errorf("forge deploy panicked instead of erroring cleanly:\n%s", out)
	}
}

// TestForgeDiff verifies that `forge diff` exits non-zero cleanly without config.
func TestForgeDiff(t *testing.T) {
	t.Parallel()
	out, code := runForge(t, "diff")
	if code == 0 {
		t.Error("forge diff with no config should exit non-zero")
	}
	if strings.Contains(out, "panic:") {
		t.Errorf("forge diff panicked:\n%s", out)
	}
}

// TestForgeMigrate verifies that `forge migrate` exits non-zero when no
// sst.config.ts is present.
func TestForgeMigrate(t *testing.T) {
	t.Parallel()
	out, code := runForge(t, "migrate")
	if code == 0 {
		t.Error("forge migrate with no config should exit non-zero")
	}
	if strings.Contains(out, "panic:") {
		t.Errorf("forge migrate panicked:\n%s", out)
	}
}

// TestForgeSecretSetGet verifies that `forge secret` shows usage without args.
func TestForgeSecretSetGet(t *testing.T) {
	t.Parallel()
	out, _ := runForge(t, "secret")
	if !strings.Contains(strings.ToLower(out), "secret") {
		t.Errorf("forge secret should print secret usage, got:\n%s", out)
	}
}
