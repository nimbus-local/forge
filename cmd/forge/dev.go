package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/nimbus-local/forge/dev"
	"github.com/spf13/cobra"
)

var devCmd = &cobra.Command{
	Use:   "dev",
	Short: "Start live Lambda development",
	Long: `Start the live development tunnel.

forge dev deploys stub Lambda functions to AWS that relay invocations over SQS
to your local machine. Your handlers run locally with full debugger access,
instant reloads, and real AWS resources (DynamoDB, S3, etc.).

The stub Lambdas are automatically removed when you Ctrl-C.`,
	RunE: runDev,
}

func runDev(_ *cobra.Command, _ []string) error {
	stage := resolveStage()

	fmt.Printf("\n%s  Starting live dev for stage %s\n", green.Render("⚡"), bold.Render(stage))
	fmt.Printf("%s  Lambda invocations will be routed to your local machine\n\n", dim.Render("  "))

	// ── Locate config ─────────────────────────────────────────────────────────
	configPath, err := findConfig()
	if err != nil {
		return err
	}
	configDir := filepath.Dir(configPath)

	// Project root: parent of infra/ if the parent has go.mod (two-module layout),
	// otherwise configDir itself (flat layout where sst.config.go is in the root module).
	projectRoot := filepath.Dir(configDir)
	if _, statErr := os.Stat(filepath.Join(projectRoot, "go.mod")); statErr != nil {
		projectRoot = configDir
	}

	// ── Build forge-stub for linux/amd64 ─────────────────────────────────────
	fmt.Printf("%s  Building stub Lambda binary...\n", dim.Render("  "))
	stubZip, err := buildStub(configDir)
	if err != nil {
		return fmt.Errorf("build stub: %w", err)
	}
	defer os.Remove(stubZip)

	// ── Bootstrap state bucket (idempotent) ──────────────────────────────────
	if err := ensureBootstrapped(stage); err != nil {
		return err
	}

	// ── Deploy stub Lambdas + shared SQS queues via Pulumi ───────────────────
	outputFile := filepath.Join(os.TempDir(), "forge-dev-"+stage+".json")
	defer os.Remove(outputFile)

	os.Setenv("FORGE_STUB_ZIP", stubZip)
	os.Setenv("FORGE_DEV_OUTPUT_FILE", outputFile)

	if err := runConfig("dev", stage); err != nil {
		return err
	}

	// ── Read handler mappings written by the deploy subprocess ───────────────
	devOut, err := readDevOutputs(outputFile)
	if err != nil {
		return fmt.Errorf("read dev outputs: %w", err)
	}

	if devOut.RequestQueueURL == "" {
		fmt.Printf("%s  No dev functions found — nothing to tunnel.\n", dim.Render("  "))
		return nil
	}

	// ── Build local handler binaries ─────────────────────────────────────────
	fmt.Printf("\n%s  Building %d handler(s)...\n", dim.Render("  "), len(devOut.Handlers))
	localBins := map[string]string{} // ARN → local binary path

	for name, h := range devOut.Handlers {
		if h.HandlerSrc == "" {
			fmt.Printf("    %s  %s: no DevHandler set — skipping\n", dim.Render("⚠"), name)
			continue
		}
		bin := filepath.Join(os.TempDir(), "forge-handler-"+name)
		buildCmd := exec.Command("go", "build", "-o", bin, h.HandlerSrc)
		buildCmd.Dir = projectRoot
		buildCmd.Env = os.Environ()
		if out, buildErr := buildCmd.CombinedOutput(); buildErr != nil {
			return fmt.Errorf("build handler %s: %w\n%s", name, buildErr, out)
		}
		localBins[h.ARN] = bin
		defer os.Remove(bin)
		fmt.Printf("    %s  %s → %s\n", green.Render("✓"), name, h.HandlerSrc)
	}

	// ── Start tunnel ──────────────────────────────────────────────────────────
	tunnel, err := dev.NewTunnel(devOut.RequestQueueURL, devOut.ResponseQueueURL)
	if err != nil {
		return fmt.Errorf("create tunnel: %w", err)
	}
	for arn, bin := range localBins {
		tunnel.RegisterHandler(arn, bin)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	go func() {
		<-ctx.Done()
		fmt.Printf("\n%s  Cleaning up dev resources…\n", red.Render("✗"))
		_ = runConfig("remove", stage)
		os.Exit(0)
	}()

	fmt.Printf("\n%s  Tunnel active — %d handler(s) ready\n\n",
		green.Render("⚡"), len(localBins))
	return tunnel.Poll(ctx)
}
