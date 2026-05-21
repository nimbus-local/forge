package main

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"
)

// ── forge deploy ───────────────────────────────────────────────────────────────

var deployCmd = &cobra.Command{
	Use:   "deploy",
	Short: "Deploy your app to AWS",
	Long: `Deploy (or update) your stack to AWS.

On first run, forge creates an S3 bucket for Pulumi state and an IAM role
for deployments. Subsequent deploys are incremental — only changed resources
are updated.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		stage := resolveStage()
		fmt.Printf("\n%s Deploying to %s\n\n", green.Render("▶"), bold.Render(stage))
		start := time.Now()

		if err := runConfig("deploy", stage); err != nil {
			return err
		}

		fmt.Printf("\n%s Deployed in %s\n\n",
			green.Render("✓"),
			time.Since(start).Round(time.Millisecond),
		)
		return nil
	},
}

// ── forge remove ───────────────────────────────────────────────────────────────

var removeCmd = &cobra.Command{
	Use:   "remove",
	Short: "Remove (destroy) a deployed stage",
	Long: `Destroy all resources belonging to the given stage.

Resources with RemovalRetain are preserved. Use --force to override this
and destroy everything including retained resources.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		stage := resolveStage()
		force, _ := cmd.Flags().GetBool("force")

		fmt.Printf("\n%s Removing stage %s", red.Render("▶"), bold.Render(stage))
		if force {
			fmt.Print(" (--force: retained resources will also be destroyed)")
		}
		fmt.Println()

		if force {
			// Signal the library to ignore RemovalRetain.
			// We do this via an extra env var picked up by forge.Run().
			_ = appendEnv(nil, "FORGE_FORCE_REMOVE", "true")
		}

		return runConfig("remove", stage)
	},
}

func init() {
	removeCmd.Flags().Bool("force", false, "Destroy retained resources too")
}

// ── forge diff ─────────────────────────────────────────────────────────────────

var diffCmd = &cobra.Command{
	Use:     "diff",
	Aliases: []string{"preview"},
	Short:   "Preview infrastructure changes without deploying",
	Long: `Run a Pulumi preview to show what would change on the next deploy.

No AWS resources are created, modified, or deleted. Equivalent to
terraform plan or pulumi preview.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		stage := resolveStage()
		fmt.Printf("\n%s Diffing stage %s\n\n", bold.Render("◈"), bold.Render(stage))
		return runConfig("diff", stage)
	},
}
