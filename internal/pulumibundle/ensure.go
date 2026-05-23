// Package pulumibundle ensures the Pulumi CLI binary is available,
// downloading it automatically when it is not found on PATH.
package pulumibundle

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
)

// version is pinned to match go.mod's pulumi/sdk/v3 dependency.
const version = "3.148.0"

// EnsureDir returns a root directory that contains a Pulumi binary at
// <root>/bin/pulumi (or pulumi.exe on Windows), downloading and
// extracting it if missing.
//
// Pass the returned path to the Automation API:
//
//	cmd, _ := auto.NewPulumiCommand(&auto.PulumiCommandOptions{Root: root})
func EnsureDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("pulumibundle: home dir: %w", err)
	}

	root := filepath.Join(home, ".forge", "pulumi", version)
	binary := filepath.Join(root, "bin", binaryName())

	if _, err := os.Stat(binary); err == nil {
		return root, nil // already installed
	}

	fmt.Fprintf(os.Stderr, "◈  Downloading Pulumi v%s\n", version)
	if err := install(root); err != nil {
		return "", fmt.Errorf("pulumibundle: install: %w", err)
	}
	fmt.Fprintf(os.Stderr, "\n✓  Pulumi v%s ready\n\n", version)
	return root, nil
}

// ── helpers ───────────────────────────────────────────────────────────────────

func binaryName() string {
	if runtime.GOOS == "windows" {
		return "pulumi.exe"
	}
	return "pulumi"
}

func releaseURL() string {
	goos := runtime.GOOS
	arch := runtime.GOARCH
	if arch == "amd64" {
		arch = "x64"
	}
	ext := "tar.gz"
	if goos == "windows" {
		ext = "zip"
	}
	return fmt.Sprintf(
		"https://get.pulumi.com/releases/sdk/pulumi-v%s-%s-%s.%s",
		version, goos, arch, ext,
	)
}

func install(root string) error {
	if err := os.MkdirAll(filepath.Join(root, "bin"), 0o755); err != nil {
		return err
	}

	url := releaseURL()
	resp, err := http.Get(url) //nolint:noctx
	if err != nil {
		return fmt.Errorf("download %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download %s: HTTP %d", url, resp.StatusCode)
	}

	// Stream to a temp file — atomic: move to dst only on success.
	tmp, err := os.CreateTemp("", "forge-pulumi-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	pr := &progressReader{r: resp.Body, total: resp.ContentLength}
	if _, err := io.Copy(tmp, pr); err != nil {
		tmp.Close()
		return fmt.Errorf("download: %w", err)
	}
	tmp.Close()

	dst := filepath.Join(root, "bin", binaryName())
	if runtime.GOOS == "windows" {
		return extractZip(tmpName, dst)
	}
	return extractTarGz(tmpName, dst)
}

// extractTarGz extracts only the main pulumi binary from a .tar.gz archive.
// The Pulumi release tar.gz layout is pulumi/<binary> alongside many
// pulumi-language-* siblings; we only need the main pulumi binary.
func extractTarGz(src, dst string) error {
	f, err := os.Open(src)
	if err != nil {
		return err
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		if hdr.Name != "pulumi/pulumi" {
			continue
		}
		return writeExecutable(tr, dst)
	}
	return fmt.Errorf("pulumi binary not found in archive")
}

// extractZip extracts only the main pulumi binary from a .zip archive (Windows).
func extractZip(src, dst string) error {
	r, err := zip.OpenReader(src)
	if err != nil {
		return err
	}
	defer r.Close()

	for _, f := range r.File {
		if f.Name != "pulumi/pulumi.exe" {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		defer rc.Close()
		return writeExecutable(rc, dst)
	}
	return fmt.Errorf("pulumi.exe not found in archive")
}

func writeExecutable(r io.Reader, dst string) error {
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, r)
	return err
}

// progressReader wraps an io.Reader and prints a simple download progress line.
type progressReader struct {
	r       io.Reader
	total   int64
	written int64
}

func (p *progressReader) Read(b []byte) (int, error) {
	n, err := p.r.Read(b)
	p.written += int64(n)
	if p.total > 0 {
		pct := float64(p.written) / float64(p.total) * 100
		fmt.Fprintf(os.Stderr, "\r    %.0f%%  (%d / %d MB)",
			pct, p.written/1024/1024, p.total/1024/1024)
	} else {
		fmt.Fprintf(os.Stderr, "\r    %d MB downloaded", p.written/1024/1024)
	}
	return n, err
}
