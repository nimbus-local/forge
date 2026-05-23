package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// runConfig builds and executes the user's sst.config.go, passing mode + stage
// via environment variables. This is the core mechanism that drives all deploy,
// remove, diff and dev commands.
//
//	mode:  "deploy" | "remove" | "diff" | "dev"
//	stage: e.g. "dev", "production"
func runConfig(mode, stage string) error {
	configPath, err := findConfig()
	if err != nil {
		return err
	}
	configDir := filepath.Dir(configPath)

	fmt.Printf("%s  stage:  %s\n", dim.Render("▸"), bold.Render(stage))
	fmt.Printf("%s  config: %s\n\n", dim.Render("▸"), dim.Render(configPath))

	// Build environment: inherit current env + forge-specific vars.
	env := os.Environ()
	env = appendEnv(env, "FORGE_MODE", mode)
	env = appendEnv(env, "FORGE_STAGE", stage)
	if flagProfile != "" {
		env = appendEnv(env, "AWS_PROFILE", flagProfile)
	}
	if flagRegion != "" {
		env = appendEnv(env, "AWS_DEFAULT_REGION", flagRegion)
	}

	// `go run .` inside the config directory.
	// Using `go run .` (not a specific file) so the user can split their config
	// across multiple files inside the infra/ directory.
	cmd := exec.Command("go", "run", ".")
	cmd.Dir = configDir
	cmd.Env = env
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin

	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			os.Exit(exitErr.ExitCode())
		}
		return fmt.Errorf("run config: %w", err)
	}
	return nil
}

// findConfig resolves the path to sst.config.go using this priority order:
//  1. --config flag
//  2. ./infra/sst.config.go
//  3. ./sst.config.go
func findConfig() (string, error) {
	if flagConfig != "" {
		abs, err := filepath.Abs(flagConfig)
		if err != nil {
			return "", err
		}
		if _, err := os.Stat(abs); err != nil {
			return "", fmt.Errorf("config not found: %s", abs)
		}
		return abs, nil
	}

	candidates := []string{
		filepath.Join("infra", "sst.config.go"),
		"sst.config.go",
	}
	for _, c := range candidates {
		abs, _ := filepath.Abs(c)
		// Check if the directory is a valid Go package (has go.mod or .go files).
		dir := filepath.Dir(abs)
		if _, err := os.Stat(abs); err == nil {
			if isGoPackage(dir) {
				return abs, nil
			}
		}
	}

	return "", fmt.Errorf(
		"no sst.config.go found.\n\n"+
			"  Run %s to convert your existing sst.config.ts, or\n"+
			"  create %s manually.\n\n"+
			"  See: https://github.com/nimbus-local/forge/blob/main/examples/sst.config.go",
		bold.Render("forge migrate"),
		bold.Render("infra/sst.config.go"),
	)
}

func isGoPackage(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".go" {
			return true
		}
		if e.Name() == "go.mod" {
			return true
		}
	}
	return false
}

func appendEnv(env []string, key, value string) []string {
	return append(env, fmt.Sprintf("%s=%s", key, value))
}
