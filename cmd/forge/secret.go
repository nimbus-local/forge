package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"github.com/spf13/cobra"
	"github.com/nimbus-local/forge/secrets"
)

var secretCmd = &cobra.Command{
	Use:   "secret",
	Short: "Manage secrets stored in SSM Parameter Store",
}

var secretSetCmd = &cobra.Command{
	Use:   "set <NAME> <VALUE>",
	Short: "Store or update a secret",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		name, value := args[0], args[1]
		m, err := newSecretManager()
		if err != nil {
			return err
		}
		if err := m.Set(context.Background(), name, value); err != nil {
			return err
		}
		fmt.Printf("%s  Secret %s set for stage %s\n",
			green.Render("✓"),
			bold.Render(name),
			bold.Render(resolveStage()),
		)
		return nil
	},
}

var secretGetCmd = &cobra.Command{
	Use:   "get <NAME>",
	Short: "Retrieve a secret value",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		m, err := newSecretManager()
		if err != nil {
			return err
		}
		value, err := m.Get(context.Background(), args[0])
		if err != nil {
			return err
		}
		fmt.Println(value)
		return nil
	},
}

var secretRemoveCmd = &cobra.Command{
	Use:     "remove <NAME>",
	Aliases: []string{"rm", "delete"},
	Short:   "Delete a secret",
	Args:    cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		m, err := newSecretManager()
		if err != nil {
			return err
		}
		if err := m.Remove(context.Background(), args[0]); err != nil {
			return err
		}
		fmt.Printf("%s  Secret %s removed\n", green.Render("✓"), bold.Render(args[0]))
		return nil
	},
}

var secretListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all secrets for the current stage",
	RunE: func(cmd *cobra.Command, args []string) error {
		m, err := newSecretManager()
		if err != nil {
			return err
		}
		names, err := m.List(context.Background())
		if err != nil {
			return err
		}
		if len(names) == 0 {
			fmt.Printf("%s  No secrets found for stage %s\n",
				dim.Render("○"),
				bold.Render(resolveStage()),
			)
			return nil
		}
		fmt.Printf("\n%s  Secrets for stage %s\n\n",
			bold.Render("🔑"),
			bold.Render(resolveStage()),
		)
		for _, name := range names {
			fmt.Printf("  %s  %s\n", dim.Render("•"), name)
		}
		fmt.Println()
		return nil
	},
}

func init() {
	secretCmd.AddCommand(secretSetCmd)
	secretCmd.AddCommand(secretGetCmd)
	secretCmd.AddCommand(secretRemoveCmd)
	secretCmd.AddCommand(secretListCmd)
}

// newSecretManager creates a Manager using the current flags / env.
func newSecretManager() (*secrets.Manager, error) {
	appName := appNameFromConfig()
	return secrets.New(appName, resolveStage(), flagProfile, flagRegion)
}

// appNameRE matches the Name field inside an AppConfig literal.
// Handles both single-line and multiline config blocks.
var appNameRE = regexp.MustCompile(`AppConfig\{[^}]*Name:\s*"([^"]+)"`)

// appNameFromConfig reads the app name from sst.config.go.
// Search order: FORGE_APP env var → parsed sst.config.go → directory name.
func appNameFromConfig() string {
	if name := os.Getenv("FORGE_APP"); name != "" {
		return name
	}

	// Locate sst.config.go using the same search order as findConfig().
	candidates := []string{
		"sst.config.go",
		filepath.Join("infra", "sst.config.go"),
		filepath.Join("..", "sst.config.go"),
	}
	if flagConfig != "" {
		candidates = append([]string{flagConfig}, candidates...)
	}
	for _, c := range candidates {
		data, err := os.ReadFile(c)
		if err != nil {
			continue
		}
		if m := appNameRE.FindSubmatch(data); len(m) > 1 {
			return string(m[1])
		}
	}

	// Last resort: use the working directory name, skipping "infra".
	if dir, err := os.Getwd(); err == nil {
		if base := filepath.Base(dir); base != "infra" {
			return base
		}
		if parent := filepath.Base(filepath.Dir(dir)); parent != "" && parent != "." {
			return parent
		}
	}
	return "app"
}
