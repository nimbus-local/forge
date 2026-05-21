// Package forge is the Go replacement for SST (Serverless Stack).
// Import this in your infra/sst.config.go and call forge.Run() from main().
// The forge CLI sets FORGE_MODE to control deploy/dev/remove behaviour.
package forge

import (
	"context"
	"fmt"
	"os"

	"github.com/pulumi/pulumi/sdk/v3/go/auto"
	"github.com/pulumi/pulumi/sdk/v3/go/auto/optdestroy"
	"github.com/pulumi/pulumi/sdk/v3/go/auto/optpreview"
	"github.com/pulumi/pulumi/sdk/v3/go/auto/optup"
	"github.com/pulumi/pulumi/sdk/v3/go/common/tokens"
	"github.com/pulumi/pulumi/sdk/v3/go/common/workspace"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

// ── Types ─────────────────────────────────────────────────────────────────────

// Config is the top-level definition of your infrastructure.
// Create one in infra/sst.config.go and pass it to forge.Run().
type Config struct {
	App *AppConfig
	Run func(ctx *RunContext) error
}

// AppConfig holds project-level metadata.
type AppConfig struct {
	Name    string
	Home    string        // "aws" (only supported value for now)
	Removal RemovalPolicy // default: RemovalDestroy
}

// RemovalPolicy controls what happens to resources when a stage is torn down.
type RemovalPolicy string

const (
	RemovalDestroy             RemovalPolicy = "destroy"
	RemovalRetain              RemovalPolicy = "retain"
	RemovalRetainOnProtection  RemovalPolicy = "retain-on-protection"
)

// RunContext is passed to your Config.Run function.
// Use it to create constructs and export stack outputs.
type RunContext struct {
	pulumiCtx *pulumi.Context
	Stage     string
	App       *AppConfig
	DevMode   bool // true when running `forge dev`
}

// Pulumi returns the underlying pulumi.Context for advanced use cases.
func (r *RunContext) Pulumi() *pulumi.Context { return r.pulumiCtx }

// Export exposes a stack output visible in `forge deploy` output and the
// SST Console. value must be a pulumi.Output or a plain string/int.
func (r *RunContext) Export(name string, value interface{}) {
	switch v := value.(type) {
	case pulumi.StringOutput:
		r.pulumiCtx.Export(name, v)
	case pulumi.Output:
		r.pulumiCtx.Export(name, v)
	default:
		r.pulumiCtx.Export(name, pulumi.Any(v))
	}
}

// Linkable is implemented by any construct that can be linked to a Function
// (injecting its ARNs / URLs as environment variables at deploy time).
// Only constructs provided by this module are intended to implement this interface.
type Linkable interface {
	LinkEnv() pulumi.StringMap
	LinkName() string
}

// ── Entry point ───────────────────────────────────────────────────────────────

// Run is the single entry point for your sst.config.go.
// It reads FORGE_MODE and FORGE_STAGE set by the CLI and acts accordingly.
//
//	func main() { forge.Run(&forge.Config{ ... }) }
func Run(cfg *Config) {
	if cfg.App == nil {
		fatal("forge: Config.App must not be nil")
	}
	if cfg.App.Name == "" {
		fatal("forge: Config.App.Name must not be empty")
	}
	if cfg.Run == nil {
		fatal("forge: Config.Run must not be nil")
	}
	if cfg.App.Removal == "" {
		cfg.App.Removal = RemovalDestroy
	}

	stage := os.Getenv("FORGE_STAGE")
	if stage == "" {
		stage = "dev"
	}

	mode := os.Getenv("FORGE_MODE")
	switch mode {
	case "deploy":
		must(runPulumi(cfg, stage, "up"))
	case "remove":
		must(runPulumi(cfg, stage, "destroy"))
	case "diff":
		must(runPulumi(cfg, stage, "preview"))
	case "dev":
		must(runPulumi(cfg, stage, "dev"))
	default:
		fmt.Fprintln(os.Stderr, "forge: run via the forge CLI")
		fmt.Fprintln(os.Stderr, "  forge deploy    Deploy your stack")
		fmt.Fprintln(os.Stderr, "  forge dev       Live development tunnel")
		fmt.Fprintln(os.Stderr, "  forge diff      Preview changes")
		fmt.Fprintln(os.Stderr, "  forge remove    Tear down a stage")
		os.Exit(1)
	}
}

// ── Internal Pulumi runner ────────────────────────────────────────────────────

func runPulumi(cfg *Config, stage, action string) error {
	ctx := context.Background()
	stackName := auto.FullyQualifiedStackName("organization", cfg.App.Name, stage)
	devMode := action == "dev"

	// Build the inline Pulumi program from the user's Run func.
	pulumiProg := func(pulumiCtx *pulumi.Context) error {
		runCtx := &RunContext{
			pulumiCtx: pulumiCtx,
			Stage:     stage,
			App:       cfg.App,
			DevMode:   devMode,
		}
		return cfg.Run(runCtx)
	}

	// Workspace options — state is stored in an S3 bucket named
	// <app>-<stage>-forge-state (created automatically on first deploy).
	workspaceOpts := []auto.LocalWorkspaceOption{
		auto.Project(workspace.Project{
			Name:    tokens.PackageName(cfg.App.Name),
			Runtime: workspace.NewProjectRuntimeInfo("go", nil),
			Backend: &workspace.ProjectBackend{
				URL: stateBackendURL(cfg.App.Name, stage),
			},
		}),
		auto.EnvVars(map[string]string{
			// Passphrase for state encryption.
			// Users can override via PULUMI_CONFIG_PASSPHRASE env var.
			"PULUMI_CONFIG_PASSPHRASE": envOrDefault("PULUMI_CONFIG_PASSPHRASE", ""),
		}),
	}

	stack, err := auto.UpsertStackInlineSource(ctx, stackName, cfg.App.Name, pulumiProg, workspaceOpts...)
	if err != nil {
		return fmt.Errorf("stack init: %w", err)
	}

	// Ensure the AWS plugin is available.
	if err := stack.Workspace().InstallPlugin(ctx, "aws", "v6.0.0"); err != nil {
		return fmt.Errorf("install aws plugin: %w", err)
	}

	switch action {
	case "up", "dev":
		_, err = stack.Up(ctx,
			optup.ProgressStreams(os.Stdout),
			optup.ErrorProgressStreams(os.Stderr),
		)
		return err

	case "destroy":
		_, err = stack.Destroy(ctx,
			optdestroy.ProgressStreams(os.Stdout),
			optdestroy.ErrorProgressStreams(os.Stderr),
		)
		return err

	case "preview":
		_, err = stack.Preview(ctx,
			optpreview.ProgressStreams(os.Stdout),
			optpreview.ErrorProgressStreams(os.Stderr),
		)
		return err
	}
	return nil
}

// ── Helpers ───────────────────────────────────────────────────────────────────

// stateBackendURL returns an S3 URL for Pulumi state storage.
// The bucket is tagged and created automatically by forge on first deploy.
func stateBackendURL(appName, stage string) string {
	bucket := os.Getenv("FORGE_STATE_BUCKET")
	if bucket != "" {
		return "s3://" + bucket
	}
	return fmt.Sprintf("s3://%s-%s-forge-state", appName, stage)
}

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func must(err error) {
	if err != nil {
		fmt.Fprintf(os.Stderr, "forge: %v\n", err)
		os.Exit(1)
	}
}

func fatal(msg string) {
	fmt.Fprintln(os.Stderr, msg)
	os.Exit(1)
}
