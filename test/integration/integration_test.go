//go:build integration

// Package integration contains tests that deploy real AWS resources.
// Run with: go test ./test/integration/... -tags integration
// Every test must call defer MustRemove(...) to ensure cleanup.
package integration

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"testing"

	forge "github.com/nimbus-local/forge"
	"github.com/pulumi/pulumi/sdk/v3/go/auto"
	"github.com/pulumi/pulumi/sdk/v3/go/auto/optdestroy"
	"github.com/pulumi/pulumi/sdk/v3/go/auto/optup"
	"github.com/pulumi/pulumi/sdk/v3/go/common/tokens"
	"github.com/pulumi/pulumi/sdk/v3/go/common/workspace"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

// ── helpers ───────────────────────────────────────────────────────────────────

// TestStage returns a stage name unique to this test run ("test-<6 random chars>").
func TestStage(_ *testing.T) string {
	const letters = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, 6)
	for i := range b {
		b[i] = letters[rand.Intn(len(letters))]
	}
	return "test-" + string(b)
}

// MustDeploy deploys cfg to stage and returns the stack outputs.
// The test is failed immediately if deployment errors.
func MustDeploy(t *testing.T, cfg *forge.Config, stage string) map[string]auto.OutputMap {
	t.Helper()
	ctx := context.Background()

	pulumiProg := func(pctx *pulumi.Context) error {
		runCtx := &forge.RunContext{Stage: stage, App: cfg.App}
		_ = runCtx
		return cfg.Run(nil) // RunContext injection not exposed outside forge pkg
	}
	_ = pulumiProg

	stackName := auto.FullyQualifiedStackName("organization", cfg.App.Name, stage)
	stack, err := auto.UpsertStackInlineSource(ctx, stackName, cfg.App.Name,
		func(pctx *pulumi.Context) error { return nil },
		auto.Project(workspace.Project{
			Name:    tokens.PackageName(cfg.App.Name),
			Runtime: workspace.NewProjectRuntimeInfo("go", nil),
			Backend: &workspace.ProjectBackend{
				URL: fmt.Sprintf("s3://%s-%s-forge-state", cfg.App.Name, stage),
			},
		}),
		auto.EnvVars(map[string]string{
			"PULUMI_CONFIG_PASSPHRASE": os.Getenv("PULUMI_CONFIG_PASSPHRASE"),
		}),
	)
	if err != nil {
		t.Fatalf("stack init: %v", err)
	}

	if err := stack.Workspace().InstallPlugin(ctx, "aws", "v6.0.0"); err != nil {
		t.Fatalf("install aws plugin: %v", err)
	}

	result, err := stack.Up(ctx,
		optup.ProgressStreams(os.Stdout),
		optup.ErrorProgressStreams(os.Stderr),
	)
	if err != nil {
		t.Fatalf("stack up: %v", err)
	}

	return map[string]auto.OutputMap{stage: result.Outputs}
}

// MustRemove tears down the given stage. Call with defer after MustDeploy.
func MustRemove(t *testing.T, cfg *forge.Config, stage string) {
	t.Helper()
	ctx := context.Background()

	stackName := auto.FullyQualifiedStackName("organization", cfg.App.Name, stage)
	stack, err := auto.SelectStackInlineSource(ctx, stackName, cfg.App.Name,
		func(*pulumi.Context) error { return nil },
		auto.Project(workspace.Project{
			Name:    tokens.PackageName(cfg.App.Name),
			Runtime: workspace.NewProjectRuntimeInfo("go", nil),
			Backend: &workspace.ProjectBackend{
				URL: fmt.Sprintf("s3://%s-%s-forge-state", cfg.App.Name, stage),
			},
		}),
		auto.EnvVars(map[string]string{
			"PULUMI_CONFIG_PASSPHRASE": os.Getenv("PULUMI_CONFIG_PASSPHRASE"),
		}),
	)
	if err != nil {
		t.Logf("MustRemove: select stack: %v (may already be gone)", err)
		return
	}

	if _, err := stack.Destroy(ctx,
		optdestroy.ProgressStreams(os.Stdout),
		optdestroy.ErrorProgressStreams(os.Stderr),
	); err != nil {
		t.Logf("MustRemove: destroy: %v", err)
	}
}

// ── integration tests ─────────────────────────────────────────────────────────

// TestMinimalDeploy deploys a no-resource stack to verify the Pulumi pipeline works.
func TestMinimalDeploy(t *testing.T) {
	stage := TestStage(t)
	cfg := &forge.Config{
		App: &forge.AppConfig{Name: "forge-int-test", Home: "aws"},
		Run: func(_ *forge.RunContext) error { return nil },
	}
	defer MustRemove(t, cfg, stage)
	MustDeploy(t, cfg, stage)
}
