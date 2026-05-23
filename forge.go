// Package forge is the Go replacement for SST (Serverless Stack).
// Import this in your infra/sst.config.go and call forge.Run() from main().
// The forge CLI sets FORGE_MODE to control deploy/dev/remove behaviour.
package forge

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/pulumi/pulumi/sdk/v3/go/auto"
	"github.com/pulumi/pulumi/sdk/v3/go/auto/optdestroy"
	"github.com/pulumi/pulumi/sdk/v3/go/auto/optpreview"
	"github.com/pulumi/pulumi/sdk/v3/go/auto/optup"
	"github.com/pulumi/pulumi/sdk/v3/go/common/tokens"
	"github.com/pulumi/pulumi/sdk/v3/go/common/workspace"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	"github.com/nimbus-local/forge/internal/pulumibundle"
)

// ── Types ─────────────────────────────────────────────────────────────────────

// Config is the top-level definition of your infrastructure.
// Create one in infra/sst.config.go and pass it to forge.Run().
type Config struct {
	App    *AppConfig
	Stages map[string]*StageConfig // per-stage overrides; key is the stage name
	Run    func(ctx *RunContext) error
}

// AppConfig holds project-level metadata.
type AppConfig struct {
	Name       string
	Home       string        // "aws" | "cloudflare" | "aws+cloudflare"
	Removal    RemovalPolicy // default: RemovalDestroy
	Cloudflare *CloudflareConfig
}

// CloudflareConfig holds Cloudflare account settings used by CF constructs.
// Fields default to the corresponding CLOUDFLARE_* environment variables.
type CloudflareConfig struct {
	// AccountID is the Cloudflare account ID. Defaults to CLOUDFLARE_ACCOUNT_ID.
	AccountID string
	// ZoneID is the Cloudflare zone ID used for custom Worker domains. Defaults to CLOUDFLARE_ZONE_ID.
	ZoneID string
}

// RemovalPolicy controls what happens to resources when a stage is torn down.
type RemovalPolicy string

const (
	RemovalDestroy            RemovalPolicy = "destroy"
	RemovalRetain             RemovalPolicy = "retain"
	RemovalRetainOnProtection RemovalPolicy = "retain-on-protection"
)

// StageConfig holds per-stage overrides applied on top of the base AppConfig.
type StageConfig struct {
	// Removal overrides the base AppConfig removal policy for this stage.
	Removal RemovalPolicy
	// AWSProfile uses a different AWS credentials profile when deploying this stage.
	AWSProfile string
	// AWSRegion deploys this stage to a different AWS region.
	AWSRegion string
	// Protected means `forge remove` requires --force to proceed.
	Protected bool
	// Tags adds extra resource tags for every resource in this stage.
	Tags map[string]string
}

// RunContext is passed to your Config.Run function.
// Use it to create constructs and export stack outputs.
type RunContext struct {
	pulumiCtx   *pulumi.Context
	Stage       string
	App         *AppConfig
	DevMode     bool
	IsProtected bool
	stageTags   map[string]string
}

// IsProduction returns true when the active stage is "production" or "prod".
func (r *RunContext) IsProduction() bool {
	return r.Stage == "production" || r.Stage == "prod"
}

// StageIn returns true if the active stage matches any of the provided names.
func (r *RunContext) StageIn(stages ...string) bool {
	for _, s := range stages {
		if r.Stage == s {
			return true
		}
	}
	return false
}

// ExtraTags returns the additional resource tags configured for this stage via StageConfig.Tags.
func (r *RunContext) ExtraTags() map[string]string {
	return r.stageTags
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

	// Resolve per-stage overrides.
	var stageCfg *StageConfig
	if cfg.Stages != nil {
		stageCfg = cfg.Stages[stage]
	}
	if stageCfg != nil {
		if stageCfg.AWSProfile != "" {
			os.Setenv("AWS_PROFILE", stageCfg.AWSProfile)
		}
		if stageCfg.AWSRegion != "" {
			os.Setenv("AWS_DEFAULT_REGION", stageCfg.AWSRegion)
		}
		if stageCfg.Removal != "" {
			cfg.App.Removal = stageCfg.Removal
		}
	}

	mode := os.Getenv("FORGE_MODE")

	// Guard: block remove on protected stages without --force.
	if mode == "remove" && stageCfg != nil && stageCfg.Protected {
		if os.Getenv("FORGE_FORCE_REMOVE") != "true" {
			fatal(fmt.Sprintf("forge: stage %q is protected — use `forge remove --force` to override", stage))
		}
	}

	switch mode {
	case "deploy":
		must(runPulumi(cfg, stage, stageCfg, "up"))
	case "remove":
		must(runPulumi(cfg, stage, stageCfg, "destroy"))
	case "diff":
		must(runPulumi(cfg, stage, stageCfg, "preview"))
	case "dev":
		must(runPulumi(cfg, stage, stageCfg, "dev"))
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

func runPulumi(cfg *Config, stage string, stageCfg *StageConfig, action string) error {
	ctx := context.Background()
	stackName := auto.FullyQualifiedStackName("organization", cfg.App.Name, stage)
	devMode := action == "dev"

	// Build the inline Pulumi program from the user's Run func.
	pulumiProg := func(pulumiCtx *pulumi.Context) error {
		runCtx := &RunContext{
			pulumiCtx:   pulumiCtx,
			Stage:       stage,
			App:         cfg.App,
			DevMode:     devMode,
			IsProtected: stageCfg != nil && stageCfg.Protected,
			stageTags:   stageCfgTags(stageCfg),
		}
		return cfg.Run(runCtx)
	}

	// Workspace options — state is stored in an S3 bucket named
	// <app>-<stage>-forge-state (created automatically on first deploy).
	workspaceEnv := map[string]string{
		// Passphrase for state encryption.
		// Users can override via PULUMI_CONFIG_PASSPHRASE env var.
		"PULUMI_CONFIG_PASSPHRASE": envOrDefault("PULUMI_CONFIG_PASSPHRASE", ""),
	}
	if endpoint := os.Getenv("FORGE_AWS_ENDPOINT"); endpoint != "" {
		workspaceEnv["AWS_ENDPOINT_URL"] = endpoint
		workspaceEnv["AWS_S3_USE_PATH_STYLE"] = "true"
		// Inject dummy credentials if none are configured — local emulators accept any value.
		if os.Getenv("AWS_ACCESS_KEY_ID") == "" {
			workspaceEnv["AWS_ACCESS_KEY_ID"] = "test"
			workspaceEnv["AWS_SECRET_ACCESS_KEY"] = "test"
		}
	}
	pulumiCmd, err := resolvePulumiCommand()
	if err != nil {
		return err
	}

	workspaceOpts := []auto.LocalWorkspaceOption{
		auto.Project(workspace.Project{
			Name:    tokens.PackageName(cfg.App.Name),
			Runtime: workspace.NewProjectRuntimeInfo("go", nil),
			Backend: &workspace.ProjectBackend{
				URL: stateBackendURL(cfg.App.Name, stage),
			},
		}),
		auto.EnvVars(workspaceEnv),
		auto.Pulumi(pulumiCmd),
	}

	stack, err := auto.UpsertStackInlineSource(ctx, stackName, cfg.App.Name, pulumiProg, workspaceOpts...)
	if err != nil {
		return fmt.Errorf("stack init: %w", err)
	}

	// Install cloud provider plugins based on Home.
	home := cfg.App.Home
	if home == "" || home == "aws" || home == "aws+cloudflare" {
		if err := stack.Workspace().InstallPlugin(ctx, "aws", "v6.83.3"); err != nil {
			return fmt.Errorf("install aws plugin: %w", err)
		}
	}
	if home == "cloudflare" || home == "aws+cloudflare" {
		if err := validateCFCredentials(); err != nil {
			return err
		}
		if err := stack.Workspace().InstallPlugin(ctx, "cloudflare", "v5.49.1"); err != nil {
			return fmt.Errorf("install cloudflare plugin: %w", err)
		}
	}

	switch action {
	case "up":
		res, upErr := stack.Up(ctx,
			optup.ProgressStreams(os.Stdout),
			optup.ErrorProgressStreams(os.Stderr),
		)
		if upErr == nil {
			printDeploySummary(res)
		}
		return upErr

	case "dev":
		_, err = stack.Up(ctx,
			optup.ProgressStreams(os.Stdout),
			optup.ErrorProgressStreams(os.Stderr),
		)
		return err

	case "destroy":
		res, destroyErr := stack.Destroy(ctx,
			optdestroy.ProgressStreams(os.Stdout),
			optdestroy.ErrorProgressStreams(os.Stderr),
		)
		if destroyErr == nil {
			printDestroySummary(res)
		}
		return destroyErr

	case "preview":
		res, previewErr := stack.Preview(ctx,
			optpreview.ProgressStreams(os.Stdout),
			optpreview.ErrorProgressStreams(os.Stderr),
		)
		if previewErr == nil {
			printPreviewSummary(res)
		}
		return previewErr
	}
	return nil
}

// ── Helpers ───────────────────────────────────────────────────────────────────

// resolvePulumiCommand returns a PulumiCommand using the system-installed
// Pulumi binary if available, or auto-downloads it to ~/.forge/pulumi/<version>/.
func resolvePulumiCommand() (auto.PulumiCommand, error) {
	cmd, err := auto.NewPulumiCommand(nil)
	if err == nil {
		return cmd, nil
	}
	root, err := pulumibundle.EnsureDir()
	if err != nil {
		return nil, err
	}
	return auto.NewPulumiCommand(&auto.PulumiCommandOptions{Root: root})
}

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

// ── Deploy output ─────────────────────────────────────────────────────────────

// printDeploySummary prints a formatted change + output summary after a successful deploy.
func printDeploySummary(res auto.UpResult) {
	fmt.Println()
	if res.Summary.ResourceChanges != nil {
		printChangeSummary(*res.Summary.ResourceChanges)
	}
	printOutputs(res.Outputs)
}

// printDestroySummary prints the resource counts removed after a successful destroy.
func printDestroySummary(res auto.DestroyResult) {
	fmt.Println()
	if res.Summary.ResourceChanges != nil {
		printChangeSummary(*res.Summary.ResourceChanges)
	}
	fmt.Println()
}

// printPreviewSummary prints the expected changes from a diff/preview run.
func printPreviewSummary(res auto.PreviewResult) {
	fmt.Println()
	counts := make(map[string]int, len(res.ChangeSummary))
	for op, n := range res.ChangeSummary {
		counts[string(op)] = n
	}
	printChangeSummary(counts)
	fmt.Println()
}

// printChangeSummary formats resource change counts into a single line.
func printChangeSummary(changes map[string]int) {
	order := []string{"create", "update", "replace", "delete", "same"}
	labels := map[string]string{
		"create":  "created",
		"update":  "updated",
		"replace": "replaced",
		"delete":  "deleted",
		"same":    "unchanged",
	}

	var parts []string
	for _, op := range order {
		n := changes[op]
		if n == 0 {
			continue
		}
		parts = append(parts, fmt.Sprintf("%d %s", n, labels[op]))
	}
	// Append any op types not in the ordered list above.
	for op, n := range changes {
		known := false
		for _, o := range order {
			if o == op {
				known = true
				break
			}
		}
		if !known && n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", n, op))
		}
	}

	if len(parts) == 0 {
		fmt.Println("  No changes.")
		return
	}
	fmt.Printf("  Changes   %s\n", strings.Join(parts, "  ·  "))
}

// printOutputs prints the stack outputs in a left-aligned table.
func printOutputs(outputs auto.OutputMap) {
	if len(outputs) == 0 {
		fmt.Println()
		return
	}

	// Measure the longest key for alignment.
	maxLen := 0
	keys := make([]string, 0, len(outputs))
	for k := range outputs {
		keys = append(keys, k)
		if len(k) > maxLen {
			maxLen = len(k)
		}
	}
	sort.Strings(keys)

	fmt.Println()
	fmt.Println("  Outputs")
	for _, k := range keys {
		v := outputs[k]
		if v.Secret {
			fmt.Printf("    %-*s  [secret]\n", maxLen, k)
		} else {
			fmt.Printf("    %-*s  %v\n", maxLen, k, v.Value)
		}
	}
	fmt.Println()
}

// validateCFCredentials returns an error if no Cloudflare authentication env vars are set.
func validateCFCredentials() error {
	if os.Getenv("CLOUDFLARE_API_TOKEN") != "" {
		return nil
	}
	if os.Getenv("CLOUDFLARE_API_KEY") != "" && os.Getenv("CLOUDFLARE_EMAIL") != "" {
		return nil
	}
	return fmt.Errorf("forge: Cloudflare credentials missing — set CLOUDFLARE_API_TOKEN (preferred) or both CLOUDFLARE_API_KEY and CLOUDFLARE_EMAIL")
}

func stageCfgTags(s *StageConfig) map[string]string {
	if s == nil {
		return nil
	}
	return s.Tags
}
