package main

import (
	"bufio"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/template"
	"unicode"

	"github.com/nimbus-local/forge/internal/templates"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

// ── Command ───────────────────────────────────────────────────────────────────

var createCmd = &cobra.Command{
	Use:   "create <project-name>",
	Short: "Scaffold a new forge project from a template",
	Long: `Create a new forge project directory pre-wired with infrastructure code.

Available templates:
  go-api      Simple HTTP API (Lambda + API Gateway v2)
  go-crud     REST CRUD API (Lambda + API Gateway v2 + DynamoDB)
  go-worker   Cloudflare Worker with KV storage
  fullstack   AWS Lambda API + Cloudflare Worker frontend`,
	Args: cobra.ExactArgs(1),
	RunE: runCreate,
}

func init() {
	createCmd.Flags().StringP("template", "t", "", "Template name: go-api, go-crud, go-worker, fullstack")
}

// ── Types ─────────────────────────────────────────────────────────────────────

type templateMeta struct {
	Name        string        `yaml:"name"`
	Description string        `yaml:"description"`
	Variables   []templateVar `yaml:"variables"`
}

type templateVar struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
	Default     string `yaml:"default"`
}

// ── Entry point ───────────────────────────────────────────────────────────────

func runCreate(cmd *cobra.Command, args []string) error {
	projectName := args[0]
	if strings.ContainsAny(projectName, "/\\:") {
		return fmt.Errorf("project name must not contain path separators")
	}

	tmplName, _ := cmd.Flags().GetString("template")
	if tmplName == "" {
		var err error
		tmplName, err = pickTemplate()
		if err != nil {
			return err
		}
	}

	// Load template metadata.
	meta, err := loadTemplateMeta(tmplName)
	if err != nil {
		return fmt.Errorf("unknown template %q — choose from: go-api, go-crud, go-worker, fullstack", tmplName)
	}

	fmt.Printf("\n%s  Creating %s from template %s\n\n",
		green.Render("▶"), bold.Render(projectName), bold.Render(tmplName))

	// Build template data: Name is always the project arg; remaining vars are prompted.
	data := map[string]string{
		"Name": projectName,
	}

	scanner := bufio.NewScanner(os.Stdin)
	for _, v := range meta.Variables {
		def := v.Default
		if def == "" && v.Name == "Module" {
			def = "github.com/example/" + projectName
		}
		data[v.Name] = prompt(scanner, v.Name, v.Description, def)
	}

	// Create project directory.
	if err := os.MkdirAll(projectName, 0755); err != nil {
		return fmt.Errorf("create project dir: %w", err)
	}

	// Render template files into the project directory.
	if err := renderTemplateFiles(tmplName, projectName, data); err != nil {
		// Clean up partial output on failure.
		os.RemoveAll(projectName)
		return fmt.Errorf("render template: %w", err)
	}

	// Run go mod tidy in each module directory.
	modDirs := findModuleDirs(projectName)
	for _, dir := range modDirs {
		fmt.Printf("%s  Running go mod tidy in %s\n", dim.Render("…"), dim.Render(dir))
		c := exec.Command("go", "mod", "tidy")
		c.Dir = dir
		c.Stdout = os.Stdout
		c.Stderr = os.Stderr
		_ = c.Run() // non-fatal — users can run it themselves
	}

	// Print next steps.
	fmt.Printf("\n%s  Project created at %s\n\n", green.Render("✓"), bold.Render(projectName))
	fmt.Printf("%s  Next steps:\n\n", bold.Render("→"))
	fmt.Printf("  %s\n", dim.Render("cd "+projectName))
	fmt.Printf("  %s\n", dim.Render("forge deploy"))
	fmt.Println()

	return nil
}

// ── Template selection ────────────────────────────────────────────────────────

var availableTemplates = []struct {
	name string
	desc string
}{
	{"go-api", "Simple HTTP API (Lambda + API Gateway v2)"},
	{"go-crud", "REST CRUD API (Lambda + API Gateway v2 + DynamoDB)"},
	{"go-worker", "Cloudflare Worker"},
	{"fullstack", "AWS Lambda API + Cloudflare Worker frontend"},
}

func pickTemplate() (string, error) {
	fmt.Println("Select a template:")
	for i, t := range availableTemplates {
		fmt.Printf("  %s  %s %s\n",
			bold.Render(fmt.Sprintf("%d.", i+1)),
			t.name,
			dim.Render("— "+t.desc),
		)
	}
	fmt.Print("\nTemplate [1]: ")

	scanner := bufio.NewScanner(os.Stdin)
	scanner.Scan()
	choice := strings.TrimSpace(scanner.Text())
	if choice == "" {
		choice = "1"
	}

	for _, t := range availableTemplates {
		if t.name == choice {
			return t.name, nil
		}
	}

	idx := 0
	if _, err := fmt.Sscanf(choice, "%d", &idx); err == nil && idx >= 1 && idx <= len(availableTemplates) {
		return availableTemplates[idx-1].name, nil
	}

	return "", fmt.Errorf("invalid selection %q", choice)
}

// ── Metadata loading ──────────────────────────────────────────────────────────

func loadTemplateMeta(name string) (*templateMeta, error) {
	data, err := templates.FS.ReadFile(name + "/template.yaml")
	if err != nil {
		return nil, err
	}
	var meta templateMeta
	if err := yaml.Unmarshal(data, &meta); err != nil {
		return nil, fmt.Errorf("parse template.yaml: %w", err)
	}
	return &meta, nil
}

// ── Rendering ─────────────────────────────────────────────────────────────────

func renderTemplateFiles(tmplName, destDir string, data map[string]string) error {
	funcMap := template.FuncMap{
		"envKey": templateEnvKey,
	}

	root := tmplName + "/"
	return fs.WalkDir(templates.FS, tmplName, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		// Skip the template root directory itself and the metadata file.
		if path == tmplName {
			return nil
		}

		// Strip the template name prefix to get the relative output path.
		rel := strings.TrimPrefix(path, root)
		if rel == "" || rel == "template.yaml" {
			return nil
		}

		destPath := filepath.Join(destDir, filepath.FromSlash(rel))

		if d.IsDir() {
			return os.MkdirAll(destPath, 0755)
		}

		content, readErr := templates.FS.ReadFile(path)
		if readErr != nil {
			return readErr
		}

		// .tmpl files are rendered; the suffix is stripped from the output name.
		if strings.HasSuffix(destPath, ".tmpl") {
			destPath = strings.TrimSuffix(destPath, ".tmpl")
			t, parseErr := template.New(path).Funcs(funcMap).Parse(string(content))
			if parseErr != nil {
				return fmt.Errorf("parse template %s: %w", path, parseErr)
			}
			f, createErr := os.Create(destPath)
			if createErr != nil {
				return createErr
			}
			defer f.Close()
			return t.Execute(f, data)
		}

		// All other files are copied verbatim.
		return os.WriteFile(destPath, content, 0644)
	})
}

// ── Module tidy ───────────────────────────────────────────────────────────────

func findModuleDirs(root string) []string {
	var dirs []string
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if d.Name() == "go.mod" {
			dirs = append(dirs, filepath.Dir(path))
		}
		return nil
	})
	return dirs
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func prompt(scanner *bufio.Scanner, name, description, def string) string {
	if description != "" {
		fmt.Printf("  %s %s\n", bold.Render(name+":"), dim.Render(description))
	}
	if def != "" {
		fmt.Printf("  [%s]: ", def)
	} else {
		fmt.Printf("  %s: ", name)
	}

	scanner.Scan()
	val := strings.TrimSpace(scanner.Text())
	if val == "" {
		val = def
	}
	fmt.Println()
	return val
}

// templateEnvKey converts camelCase/kebab-case to SCREAMING_SNAKE_CASE for use in templates.
func templateEnvKey(name string) string {
	var b strings.Builder
	for i, r := range name {
		if unicode.IsUpper(r) && i > 0 {
			b.WriteRune('_')
		}
		b.WriteRune(unicode.ToUpper(r))
	}
	s := b.String()
	s = strings.ReplaceAll(s, "-", "_")
	return s
}
