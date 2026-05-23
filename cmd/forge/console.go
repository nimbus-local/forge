package main

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"sort"
	"strings"

	"github.com/pulumi/pulumi/sdk/v3/go/auto"
	"github.com/pulumi/pulumi/sdk/v3/go/common/tokens"
	"github.com/pulumi/pulumi/sdk/v3/go/common/workspace"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	"github.com/spf13/cobra"
	"github.com/nimbus-local/forge/secrets"
)

//go:embed consoleassets
var consoleAssets embed.FS

var consoleCmd = &cobra.Command{
	Use:   "console",
	Short: "Open the forge web console",
	Long: `Starts a local web server and opens the forge console in your browser.

The console shows stack outputs, deployed resources, and secrets for the active stage.
Data is fetched live on load and on each Refresh click.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		port, _ := cmd.Flags().GetInt("port")
		return runConsole(port)
	},
}

func init() {
	consoleCmd.Flags().IntP("port", "p", 3000, "Local port for the console server")
}

// ── Data types ─────────────────────────────────────────────────────────────────

type consolePayload struct {
	App       string          `json:"app"`
	Stage     string          `json:"stage"`
	Outputs   []outputEntry   `json:"outputs"`
	Resources []resourceEntry `json:"resources"`
	Secrets   []secretEntry   `json:"secrets"`
}

type outputEntry struct {
	Key    string `json:"key"`
	Value  string `json:"value"`
	Secret bool   `json:"secret"`
}

type resourceEntry struct {
	Type string `json:"type"`
	Name string `json:"name"`
	ID   string `json:"id"`
}

type secretEntry struct {
	Name string `json:"name"`
}

// ── Console runner ─────────────────────────────────────────────────────────────

func runConsole(port int) error {
	stage := resolveStage()

	configPath, err := findConfig()
	if err != nil {
		return err
	}

	appName, err := resolveAppNameFromConfig(configPath)
	if err != nil {
		return err
	}

	addr := fmt.Sprintf("localhost:%d", port)
	url := "http://" + addr

	fmt.Printf("%s  stage:   %s\n", dim.Render("▸"), bold.Render(stage))
	fmt.Printf("%s  app:     %s\n", dim.Render("▸"), bold.Render(appName))
	fmt.Printf("%s  console: %s\n\n", dim.Render("▸"), bold.Render(url))

	mux := http.NewServeMux()

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		b, readErr := consoleAssets.ReadFile("consoleassets/index.html")
		if readErr != nil {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(b)
	})

	mux.HandleFunc("/api/data", func(w http.ResponseWriter, r *http.Request) {
		payload, fetchErr := fetchConsoleData(appName, stage)
		if fetchErr != nil {
			http.Error(w, fetchErr.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(payload)
	})

	go openBrowser(url)

	fmt.Printf("%s  Press Ctrl+C to stop.\n", dim.Render("▸"))
	return http.ListenAndServe(addr, mux)
}

// ── Data fetching ──────────────────────────────────────────────────────────────

func fetchConsoleData(appName, stage string) (*consolePayload, error) {
	ctx := context.Background()
	payload := &consolePayload{App: appName, Stage: stage}

	// ── Pulumi stack ────────────────────────────────────────────────────────────
	backendURL := fmt.Sprintf("s3://%s-%s-forge-state", appName, stage)
	if b := os.Getenv("FORGE_STATE_BUCKET"); b != "" {
		backendURL = "s3://" + b
	}
	stackName := auto.FullyQualifiedStackName("organization", appName, stage)

	stack, err := auto.SelectStackInlineSource(ctx, stackName, appName,
		func(*pulumi.Context) error { return nil }, // no-op — read-only access
		auto.Project(workspace.Project{
			Name:    tokens.PackageName(appName),
			Runtime: workspace.NewProjectRuntimeInfo("go", nil),
			Backend: &workspace.ProjectBackend{URL: backendURL},
		}),
		auto.EnvVars(map[string]string{
			"PULUMI_CONFIG_PASSPHRASE": os.Getenv("PULUMI_CONFIG_PASSPHRASE"),
		}),
	)
	if err != nil {
		return nil, fmt.Errorf("connect to stack %q: %w\n\nMake sure the stack has been deployed at least once.", stackName, err)
	}

	// Stack outputs
	outputs, err := stack.Outputs(ctx)
	if err != nil {
		return nil, fmt.Errorf("read outputs: %w", err)
	}
	keys := make([]string, 0, len(outputs))
	for k := range outputs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		v := outputs[k]
		val := ""
		if !v.Secret {
			val = fmt.Sprintf("%v", v.Value)
		}
		payload.Outputs = append(payload.Outputs, outputEntry{Key: k, Value: val, Secret: v.Secret})
	}

	// Stack resources (best-effort — ignore export errors)
	if state, exportErr := stack.Export(ctx); exportErr == nil {
		payload.Resources = parseResourceState(state.Deployment)
	}

	// ── Secrets (SSM) ───────────────────────────────────────────────────────────
	sm, err := secrets.New(appName, stage, flagProfile, flagRegion)
	if err == nil {
		if names, listErr := sm.List(ctx); listErr == nil {
			for _, n := range names {
				payload.Secrets = append(payload.Secrets, secretEntry{Name: n})
			}
		}
	}

	return payload, nil
}

// ── Resource state parsing ─────────────────────────────────────────────────────

type deploymentSnapshot struct {
	Resources []struct {
		Type string `json:"type"`
		URN  string `json:"urn"`
		ID   string `json:"id"`
	} `json:"resources"`
}

func parseResourceState(raw json.RawMessage) []resourceEntry {
	if len(raw) == 0 {
		return nil
	}
	var snap deploymentSnapshot
	if err := json.Unmarshal(raw, &snap); err != nil {
		return nil
	}
	entries := make([]resourceEntry, 0, len(snap.Resources))
	for _, r := range snap.Resources {
		parts := strings.Split(r.URN, "::")
		name := parts[len(parts)-1]
		entries = append(entries, resourceEntry{Type: r.Type, Name: name, ID: r.ID})
	}
	return entries
}

// ── Helpers ────────────────────────────────────────────────────────────────────

// appNameRe matches the App.Name field in sst.config.go.
var appNameRe = regexp.MustCompile(`Name:\s*"([^"]+)"`)

// resolveAppNameFromConfig parses the App.Name from the config file.
// Falls back to FORGE_APP env var when Name is set dynamically.
func resolveAppNameFromConfig(configPath string) (string, error) {
	if name := os.Getenv("FORGE_APP"); name != "" {
		return name, nil
	}
	content, err := os.ReadFile(configPath)
	if err != nil {
		return "", fmt.Errorf("read config: %w", err)
	}
	m := appNameRe.FindSubmatch(content)
	if m == nil {
		return "", fmt.Errorf(
			"could not find App.Name in %s\n  Set the FORGE_APP env var to specify the app name manually.",
			configPath,
		)
	}
	return string(m[1]), nil
}

func openBrowser(url string) {
	var cmd string
	switch runtime.GOOS {
	case "darwin":
		cmd = "open"
	case "windows":
		cmd = "start"
	default:
		cmd = "xdg-open"
	}
	_ = exec.Command(cmd, url).Start()
}
