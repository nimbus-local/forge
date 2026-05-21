// Package main is the forge CLI — a drop-in replacement for the sst CLI.
package main

import (
	"fmt"
	"os"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
)

var (
	// Global flags — available on every subcommand.
	flagStage   string
	flagProfile string
	flagRegion  string
	flagConfig  string // path to sst.config.go (default: ./infra/sst.config.go)

	// Styling.
	bold  = lipgloss.NewStyle().Bold(true)
	green = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	red   = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	dim   = lipgloss.NewStyle().Faint(true)
)

var rootCmd = &cobra.Command{
	Use:   "forge",
	Short: "forge — Go SST: deploy AWS serverless apps from native Go",
	Long: fmt.Sprintf(`%s

A drop-in replacement for SST (Serverless Stack) written in Go.
Powered by Pulumi under the hood — state files are fully compatible with SST v3 Ion.

%s
  forge deploy           Deploy your app to AWS
  forge dev              Start live Lambda development
  forge diff             Preview infrastructure changes
  forge remove           Tear down a stage
  forge secret set KEY   Store a secret in SSM Parameter Store
  forge migrate          Convert sst.config.ts → sst.config.go

%s
  FORGE_STATE_BUCKET     S3 bucket for Pulumi state (default: <app>-<stage>-forge-state)
  PULUMI_CONFIG_PASSPHRASE  Passphrase for state encryption (default: empty)
  AWS_PROFILE / AWS_REGION  Standard AWS credential chain vars`,
		bold.Render("forge"),
		bold.Render("Commands:"),
		bold.Render("Environment variables:"),
	),
	SilenceUsage:  true,
	SilenceErrors: true,
}

func init() {
	rootCmd.PersistentFlags().StringVarP(&flagStage, "stage", "s", "", "Deployment stage (default: $USER or \"dev\")")
	rootCmd.PersistentFlags().StringVar(&flagProfile, "profile", "", "AWS credentials profile")
	rootCmd.PersistentFlags().StringVar(&flagRegion, "region", "", "AWS region (overrides profile / env)")
	rootCmd.PersistentFlags().StringVar(&flagConfig, "config", "", "Path to sst.config.go (default: ./infra/sst.config.go or ./sst.config.go)")

	rootCmd.AddCommand(deployCmd)
	rootCmd.AddCommand(removeCmd)
	rootCmd.AddCommand(diffCmd)
	rootCmd.AddCommand(devCmd)
	rootCmd.AddCommand(secretCmd)
	rootCmd.AddCommand(migrateCmd)
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, red.Render("Error: ")+err.Error())
		os.Exit(1)
	}
}

// resolveStage returns the stage from --stage flag, $FORGE_STAGE, or $USER.
func resolveStage() string {
	if flagStage != "" {
		return flagStage
	}
	if s := os.Getenv("FORGE_STAGE"); s != "" {
		return s
	}
	if u := os.Getenv("USER"); u != "" {
		return u
	}
	return "dev"
}
