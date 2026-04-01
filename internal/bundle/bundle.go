package bundle

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/bluecadet/preflight/internal/action"
)

const (
	ManifestPath = "manifest.json"
	PlanPath     = "plan.json"
)

// Manifest describes a staged offline bundle.
type Manifest struct {
	FormatVersion int                `json:"format_version"`
	CreatedAt     time.Time          `json:"created_at"`
	PlaybookName  string             `json:"playbook_name"`
	TargetName    string             `json:"target_name"`
	TargetOS      string             `json:"target_os"`
	TargetArch    string             `json:"target_arch"`
	RuntimeBinary string             `json:"runtime_binary"`
	Build         BuildInfo          `json:"build"`
	Modules       []ModuleInfo       `json:"modules,omitempty"`
	Checksums     map[string]string  `json:"checksums,omitempty"`
	LockEntries   []action.LockEntry `json:"lock_entries,omitempty"`
}

// BuildInfo identifies the preflight binary that created the bundle.
type BuildInfo struct {
	Version string `json:"version"`
	Commit  string `json:"commit"`
	Date    string `json:"date"`
}

// ModuleInfo records one module referenced by the staged plan.
type ModuleInfo struct {
	Name    string `json:"name"`
	Kind    string `json:"kind"`
	Path    string `json:"path,omitempty"`
	Version string `json:"version,omitempty"`
}

// FileSpec is one file to include in a bundle.
type FileSpec struct {
	Path string
	Mode os.FileMode
	Data []byte
}

// ExtractedBundle is a bundle extracted to a temporary directory for execution.
type ExtractedBundle struct {
	Manifest    *Manifest
	RootDir     string
	PlanPath    string
	PluginDir   string
	RuntimePath string
}

func Write(path string, manifest *Manifest, files []FileSpec) error {
	if manifest == nil {
		return fmt.Errorf("bundle: nil manifest")
	}
	if manifest.FormatVersion == 0 {
		manifest.FormatVersion = 1
	}
	if manifest.CreatedAt.IsZero() {
		manifest.CreatedAt = time.Now().UTC()
	}
	manifest.Checksums = make(map[string]string, len(files))
	for _, file := range files {
		manifest.Checksums[file.Path] = checksum(file.Data)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("bundle: mkdir %q: %w", filepath.Dir(path), err)
	}

	out, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("bundle: create %q: %w", path, err)
	}
	defer func() {
		_ = out.Close()
	}()

	zw := zip.NewWriter(out)
	defer func() {
		_ = zw.Close()
	}()

	manifestBytes, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("bundle: marshal manifest: %w", err)
	}
	if err := writeZipFile(zw, ManifestPath, 0o644, manifestBytes); err != nil {
		return err
	}

	for _, file := range files {
		mode := file.Mode
		if mode == 0 {
			mode = 0o644
		}
		if err := writeZipFile(zw, file.Path, mode, file.Data); err != nil {
			return err
		}
	}

	if err := zw.Close(); err != nil {
		return fmt.Errorf("bundle: close writer: %w", err)
	}
	return nil
}

func Extract(path string) (*ExtractedBundle, error) {
	reader, err := zip.OpenReader(path)
	if err != nil {
		return nil, fmt.Errorf("bundle: open %q: %w", path, err)
	}
	defer func() {
		_ = reader.Close()
	}()

	manifest, err := readManifest(reader.File)
	if err != nil {
		return nil, err
	}

	tempDir, err := os.MkdirTemp("", "preflight-bundle-*")
	if err != nil {
		return nil, fmt.Errorf("bundle: temp dir: %w", err)
	}

	loaded := &ExtractedBundle{
		Manifest:  manifest,
		RootDir:   tempDir,
		PluginDir: filepath.Join(tempDir, "plugins"),
	}
	seenChecksums := make(map[string]struct{}, len(manifest.Checksums))

	for _, file := range reader.File {
		if file.Name == ManifestPath {
			continue
		}

		outPath, err := extractionPath(tempDir, file.Name)
		if err != nil {
			_ = loaded.Cleanup()
			return nil, err
		}

		data, err := readZipEntry(file)
		if err != nil {
			_ = loaded.Cleanup()
			return nil, err
		}
		if err := verifyExtractedChecksum(manifest.Checksums, file.Name, data, seenChecksums); err != nil {
			_ = loaded.Cleanup()
			return nil, err
		}

		if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
			_ = loaded.Cleanup()
			return nil, fmt.Errorf("bundle: mkdir %q: %w", filepath.Dir(outPath), err)
		}
		mode := file.Mode()
		if mode == 0 {
			mode = 0o644
		}
		if err := os.WriteFile(outPath, data, mode); err != nil {
			_ = loaded.Cleanup()
			return nil, fmt.Errorf("bundle: write %q: %w", outPath, err)
		}

		switch file.Name {
		case PlanPath:
			loaded.PlanPath = outPath
		default:
			if strings.HasPrefix(file.Name, "plugins/") {
				loaded.PluginDir = filepath.Join(tempDir, "plugins")
			}
			if loaded.RuntimePath == "" && strings.HasPrefix(file.Name, "runtime/") {
				loaded.RuntimePath = outPath
			}
		}
	}

	if err := verifyExpectedChecksums(manifest.Checksums, seenChecksums); err != nil {
		_ = loaded.Cleanup()
		return nil, err
	}
	if loaded.PlanPath == "" {
		_ = loaded.Cleanup()
		return nil, fmt.Errorf("bundle: missing plan payload")
	}
	return loaded, nil
}

func (b *ExtractedBundle) Cleanup() error {
	if b == nil || b.RootDir == "" {
		return nil
	}
	return os.RemoveAll(b.RootDir)
}

func BundleFileName(playbookName, targetName, osName, arch string) string {
	return sanitize(playbookName) + "-" + sanitize(targetName) + "-" + sanitize(osName) + "-" + sanitize(arch) + ".zip"
}

func writeZipFile(zw *zip.Writer, path string, mode os.FileMode, data []byte) error {
	header := &zip.FileHeader{
		Name:   path,
		Method: zip.Deflate,
	}
	header.SetMode(mode)
	w, err := zw.CreateHeader(header)
	if err != nil {
		return fmt.Errorf("bundle: create entry %q: %w", path, err)
	}
	if _, err := w.Write(data); err != nil {
		return fmt.Errorf("bundle: write entry %q: %w", path, err)
	}
	return nil
}

func readManifest(files []*zip.File) (*Manifest, error) {
	for _, file := range files {
		if file.Name != ManifestPath {
			continue
		}
		data, err := readZipEntry(file)
		if err != nil {
			return nil, err
		}
		var manifest Manifest
		if err := json.Unmarshal(data, &manifest); err != nil {
			return nil, fmt.Errorf("bundle: parse manifest: %w", err)
		}
		return &manifest, nil
	}
	return nil, fmt.Errorf("bundle: missing manifest")
}

func readZipEntry(file *zip.File) ([]byte, error) {
	rc, err := file.Open()
	if err != nil {
		return nil, fmt.Errorf("bundle: open entry %q: %w", file.Name, err)
	}
	defer func() {
		_ = rc.Close()
	}()

	data, err := io.ReadAll(rc)
	if err != nil {
		return nil, fmt.Errorf("bundle: read entry %q: %w", file.Name, err)
	}
	return data, nil
}

func extractionPath(root, entryName string) (string, error) {
	normalized := strings.ReplaceAll(entryName, "\\", "/")
	cleaned := path.Clean(normalized)
	switch {
	case cleaned == ".":
		return "", fmt.Errorf("bundle: invalid empty entry path")
	case cleaned == ".." || strings.HasPrefix(cleaned, "../"):
		return "", fmt.Errorf("bundle: entry %q escapes extraction root", entryName)
	case strings.HasPrefix(cleaned, "/"):
		return "", fmt.Errorf("bundle: entry %q uses an absolute path", entryName)
	case hasWindowsVolumePrefix(cleaned):
		return "", fmt.Errorf("bundle: entry %q uses an absolute path", entryName)
	}
	return filepath.Join(root, filepath.FromSlash(cleaned)), nil
}

func hasWindowsVolumePrefix(path string) bool {
	return len(path) >= 2 && path[1] == ':'
}

func verifyExtractedChecksum(expected map[string]string, name string, data []byte, seen map[string]struct{}) error {
	if len(expected) == 0 {
		return nil
	}

	want, ok := expected[name]
	if !ok {
		return fmt.Errorf("bundle: unexpected file %q is missing a checksum entry", name)
	}
	got := checksum(data)
	if got != want {
		return fmt.Errorf("bundle: checksum mismatch for %q", name)
	}
	seen[name] = struct{}{}
	return nil
}

func verifyExpectedChecksums(expected map[string]string, seen map[string]struct{}) error {
	if len(expected) == 0 {
		return nil
	}
	for name := range expected {
		if _, ok := seen[name]; ok {
			continue
		}
		return fmt.Errorf("bundle: missing expected file %q", name)
	}
	return nil
}

func checksum(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func sanitize(s string) string {
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "" {
		return "bundle"
	}
	var b strings.Builder
	lastDash := false
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastDash = false
		default:
			if !lastDash {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}
	return strings.Trim(b.String(), "-")
}
