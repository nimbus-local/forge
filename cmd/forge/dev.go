package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

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
	RunE: func(cmd *cobra.Command, args []string) error {
		stage := resolveStage()

		fmt.Printf("\n%s  Starting live dev for stage %s\n", green.Render("⚡"), bold.Render(stage))
		fmt.Printf("%s  Lambda invocations will be routed to your local machine\n\n", dim.Render("  "))

		// Set up Ctrl-C handling so we can clean up stub Lambdas.
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		go func() {
			<-sigCh
			fmt.Printf("\n%s  Cleaning up dev resources…\n", red.Render("✗"))
			os.Exit(0)
		}()

		return runConfig("dev", stage)
	},
}
