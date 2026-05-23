package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/nimbus-local/forge/migrate"
)

var migrateCmd = &cobra.Command{
	Use:   "migrate [path/to/sst.config.ts]",
	Short: "Convert sst.config.ts → infra/sst.config.go",
	Long: `Automatically convert an SST v3 TypeScript config to the forge Go equivalent.

The migrator handles the most common SST constructs:
  • sst.aws.Function       → constructs.NewFunction
  • sst.aws.ApiGatewayV2  → constructs.NewApiGatewayV2
  • sst.aws.DynamoDB       → constructs.NewDynamoDB
  • sst.aws.Bucket         → constructs.NewBucket
  • sst.aws.Cron           → constructs.NewCron
  • sst.aws.Queue          → constructs.NewQueue

For complex patterns (multi-line args, conditional logic) the migrator emits
// TODO comments so you know exactly what needs manual attention.

Output is written to ./infra/sst.config.go (created if it doesn't exist).
A go.mod for the infra module is also created if missing.`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		// Resolve input path.
		inputPath := "sst.config.ts"
		if len(args) == 1 {
			inputPath = args[0]
		}
		abs, err := filepath.Abs(inputPath)
		if err != nil || !fileExists(abs) {
			return fmt.Errorf("sst.config.ts not found at %s\n\nPass the path explicitly: forge migrate path/to/sst.config.ts", abs)
		}

		fmt.Printf("\n%s  Reading %s\n", bold.Render("→"), dim.Render(abs))

		result, err := migrate.ConvertFile(abs)
		if err != nil {
			return fmt.Errorf("migration failed: %w", err)
		}

		// Resolve output path.
		outDir, _ := cmd.Flags().GetString("out")
		if outDir == "" {
			outDir = "infra"
		}
		outPath := filepath.Join(outDir, "sst.config.go")

		// Create infra/ directory if needed.
		if err := os.MkdirAll(outDir, 0755); err != nil {
			return fmt.Errorf("create output dir: %w", err)
		}

		// Write go.mod for the infra module if it doesn't exist.
		goModPath := filepath.Join(outDir, "go.mod")
		if !fileExists(goModPath) {
			appName := appNameFromConfig()
			goMod := fmt.Sprintf(`module %s/infra

go 1.22

require github.com/nimbus-local/forge v0.1.0
`, appName)
			if err := os.WriteFile(goModPath, []byte(goMod), 0644); err != nil {
				return fmt.Errorf("write go.mod: %w", err)
			}
			fmt.Printf("%s  Created %s\n", green.Render("✓"), dim.Render(goModPath))
		}

		// Check for existing output — don't overwrite without --force.
		force, _ := cmd.Flags().GetBool("force")
		if fileExists(outPath) && !force {
			return fmt.Errorf(
				"%s already exists.\n\nUse %s to overwrite it.",
				bold.Render(outPath),
				bold.Render("forge migrate --force"),
			)
		}

		if err := os.WriteFile(outPath, []byte(result.GoSource), 0644); err != nil {
			return fmt.Errorf("write output: %w", err)
		}

		fmt.Printf("%s  Generated %s\n\n", green.Render("✓"), bold.Render(outPath))

		// Print warnings.
		if len(result.Warnings) > 0 {
			fmt.Printf("%s  %d items need manual review:\n\n",
				bold.Render("⚠"),
				len(result.Warnings),
			)
			for _, w := range result.Warnings {
				fmt.Printf("  %s  %s\n", dim.Render("•"), w)
			}
			fmt.Println()
		}

		// Print unsupported patterns.
		if len(result.Unsupported) > 0 {
			fmt.Printf("%s  %d patterns could not be converted (marked as TODO):\n\n",
				red.Render("✗"),
				len(result.Unsupported),
			)
			for _, u := range result.Unsupported {
				fmt.Printf("  %s  %s\n", red.Render("•"), u)
			}
			fmt.Println()
		}

		fmt.Printf("%s  Next steps:\n\n", bold.Render("→"))
		fmt.Printf("  1. Review %s\n", bold.Render(outPath))
		fmt.Printf("  2. Fix any %s comments\n", dim.Render("// TODO:"))
		fmt.Printf("  3. %s\n", bold.Render("cd infra && go mod tidy"))
		fmt.Printf("  4. %s\n\n", bold.Render("forge diff  # preview changes"))

		return nil
	},
}

func init() {
	migrateCmd.Flags().String("out", "infra", "Output directory for sst.config.go")
	migrateCmd.Flags().Bool("force", false, "Overwrite existing sst.config.go")
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
