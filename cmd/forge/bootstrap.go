package main

import (
	"context"
	"fmt"

	"github.com/nimbus-local/forge/internal/bootstrap"
	"github.com/spf13/cobra"
)

var bootstrapCmd = &cobra.Command{
	Use:   "bootstrap",
	Short: "Create the Pulumi state S3 bucket",
	Long: `Create the S3 bucket used to store Pulumi state.

The bucket is named <app>-<stage>-forge-state and is configured with:
  • Versioning enabled
  • SSE-S3 encryption
  • All public access blocked
  • Lifecycle rule to expire old state versions after 90 days

Idempotent — safe to run multiple times. forge deploy calls this automatically
on first deploy, so you rarely need to run this command directly.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		stage := resolveStage()
		appName := appNameFromConfig()

		fmt.Printf("\n%s  Bootstrapping state bucket for %s/%s\n",
			bold.Render("◈"), bold.Render(appName), bold.Render(stage))

		bucketName, created, err := bootstrap.EnsureStateBucket(context.Background(), bootstrap.Config{
			AppName:    appName,
			Stage:      stage,
			AWSProfile: flagProfile,
			AWSRegion:  flagRegion,
		})
		if err != nil {
			return fmt.Errorf("bootstrap: %w", err)
		}

		if created {
			fmt.Printf("%s  Created %s\n\n", green.Render("✓"), bold.Render(bucketName))
		} else {
			fmt.Printf("%s  Already exists: %s\n\n", green.Render("✓"), dim.Render(bucketName))
		}
		return nil
	},
}
