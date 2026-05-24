package main

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"

	forge "github.com/nimbus-local/forge"
)

// buildStub cross-compiles cmd/forge-stub for linux/amd64, zips it as "bootstrap",
// and returns the path to the zip file. The caller is responsible for removing it.
//
// configDir is the directory containing the user's sst.config.go. The forge module
// source is located via `go list -m -json github.com/nimbus-local/forge` run there.
func buildStub(configDir string) (zipPath string, err error) {
	forgeDir, err := findForgeModuleDir(configDir)
	if err != nil {
		return "", err
	}

	// Cross-compile stub for Lambda (linux/amd64, static).
	stubBin := filepath.Join(os.TempDir(), "forge-stub")
	buildCmd := exec.Command("go", "build", "-o", stubBin, "./cmd/forge-stub/")
	buildCmd.Dir = forgeDir
	buildCmd.Env = append(os.Environ(), "GOOS=linux", "GOARCH=amd64", "CGO_ENABLED=0")
	if out, buildErr := buildCmd.CombinedOutput(); buildErr != nil {
		return "", fmt.Errorf("compile forge-stub: %w\n%s", buildErr, out)
	}
	defer os.Remove(stubBin)

	zipPath = stubBin + ".zip"
	if err := zipBinary(stubBin, "bootstrap", zipPath); err != nil {
		return "", fmt.Errorf("zip stub: %w", err)
	}
	return zipPath, nil
}

// findForgeModuleDir runs `go list -m -json github.com/nimbus-local/forge` in dir
// and returns the module's local directory path (respecting replace directives).
func findForgeModuleDir(dir string) (string, error) {
	cmd := exec.Command("go", "list", "-m", "-json", "github.com/nimbus-local/forge")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("locate forge module: %w", err)
	}
	var info struct {
		Dir string `json:"Dir"`
	}
	if err := json.Unmarshal(out, &info); err != nil {
		return "", fmt.Errorf("parse module info: %w", err)
	}
	if info.Dir == "" {
		return "", fmt.Errorf("forge module directory not found — is github.com/nimbus-local/forge in your go.mod?")
	}
	return info.Dir, nil
}

// zipBinary creates a zip archive at dest containing src named entryName.
func zipBinary(src, entryName, dest string) error {
	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer f.Close()

	w := zip.NewWriter(f)
	defer w.Close()

	src_f, err := os.Open(src)
	if err != nil {
		return err
	}
	defer src_f.Close()

	info, err := src_f.Stat()
	if err != nil {
		return err
	}

	hdr := &zip.FileHeader{
		Name:   entryName,
		Method: zip.Deflate,
	}
	hdr.SetMode(info.Mode())

	entry, err := w.CreateHeader(hdr)
	if err != nil {
		return err
	}
	_, err = io.Copy(entry, src_f)
	return err
}

// readDevOutputs reads the JSON file written by forge.writeDevOutputs.
func readDevOutputs(path string) (forge.DevOutputFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			// No dev functions were registered.
			return forge.DevOutputFile{Handlers: map[string]forge.DevHandler{}}, nil
		}
		return forge.DevOutputFile{}, err
	}
	var out forge.DevOutputFile
	if err := json.Unmarshal(data, &out); err != nil {
		return forge.DevOutputFile{}, fmt.Errorf("parse dev outputs: %w", err)
	}
	if out.Handlers == nil {
		out.Handlers = map[string]forge.DevHandler{}
	}
	return out, nil
}
