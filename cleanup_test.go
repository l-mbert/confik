package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCleanLeftoversFromManifest(t *testing.T) {
	base := t.TempDir()

	fileA := filepath.Join(base, "alpha.txt")
	if err := os.WriteFile(fileA, []byte("alpha"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	nestedDir := filepath.Join(base, "nested")
	if err := os.MkdirAll(nestedDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	fileB := filepath.Join(nestedDir, "beta.txt")
	if err := os.WriteFile(fileB, []byte("beta"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	gitDir := filepath.Join(base, ".git")
	infoDir := filepath.Join(gitDir, "info")
	if err := os.MkdirAll(infoDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	excludePath := filepath.Join(infoDir, "exclude")
	block := "# confik:start:run1\nalpha.txt\n# confik:end:run1\n"
	if err := os.WriteFile(excludePath, []byte(block), 0o644); err != nil {
		t.Fatalf("write exclude: %v", err)
	}

	manifest := Manifest{
		RunID:        "run1",
		CreatedFiles: []string{"alpha.txt", filepath.ToSlash(filepath.Join("nested", "beta.txt"))},
		CreatedDirs:  []string{filepath.ToSlash("nested")},
		Gitignore: &GitContext{
			GitRoot:     base,
			GitDir:      gitDir,
			ExcludePath: excludePath,
			RunID:       "run1",
		},
		CreatedAt: "2024-01-01T00:00:00Z",
	}

	configDir := filepath.Join(base, ".config")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("mkdir config: %v", err)
	}

	if err := writeManifest(filepath.Join(configDir, manifestFilename), manifest); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	if err := cleanLeftovers(base, true, false); err != nil {
		t.Fatalf("cleanLeftovers error: %v", err)
	}

	if _, err := os.Stat(fileA); err == nil {
		t.Fatalf("expected fileA removed")
	}
	if _, err := os.Stat(fileB); err == nil {
		t.Fatalf("expected fileB removed")
	}
	if _, err := os.Stat(nestedDir); err == nil {
		t.Fatalf("expected nestedDir removed")
	}
	content, _ := os.ReadFile(excludePath)
	if strings.Contains(string(content), "confik:start:run1") {
		t.Fatalf("expected gitignore block removed")
	}
	if _, err := os.Stat(filepath.Join(configDir, manifestFilename)); err == nil {
		t.Fatalf("expected manifest removed")
	}
}

func TestCleanLeftoversRemovesBlocksWithoutManifest(t *testing.T) {
	base := t.TempDir()
	gitDir := filepath.Join(base, ".git")
	infoDir := filepath.Join(gitDir, "info")
	if err := os.MkdirAll(infoDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	excludePath := filepath.Join(infoDir, "exclude")
	block := "# confik:start:run2\nfoo\n# confik:end:run2\n"
	if err := os.WriteFile(excludePath, []byte(block), 0o644); err != nil {
		t.Fatalf("write exclude: %v", err)
	}

	if err := cleanLeftovers(base, true, false); err != nil {
		t.Fatalf("cleanLeftovers error: %v", err)
	}

	content, _ := os.ReadFile(excludePath)
	if strings.Contains(string(content), "confik:start:run2") {
		t.Fatalf("expected block removed")
	}
}

func TestCleanLeftoversFailsWhenKnownArtifactsRemain(t *testing.T) {
	base := t.TempDir()
	configDir := filepath.Join(base, ".config")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("mkdir config: %v", err)
	}

	nested := filepath.Join(base, "nested")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatalf("mkdir nested: %v", err)
	}
	if err := os.WriteFile(filepath.Join(nested, "user.txt"), []byte("keep"), 0o644); err != nil {
		t.Fatalf("write nested file: %v", err)
	}

	manifest := Manifest{
		RunID:        "run-remains",
		CreatedFiles: []string{},
		CreatedDirs:  []string{filepath.ToSlash("nested")},
		CreatedAt:    "2024-01-01T00:00:00Z",
	}
	manifestPath := filepath.Join(configDir, manifestFilename)
	if err := writeManifest(manifestPath, manifest); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	err := cleanLeftovers(base, true, true)
	if err == nil {
		t.Fatalf("expected cleanup to report remaining artifacts")
	}
	if !strings.Contains(err.Error(), "path(s) remain") {
		t.Fatalf("expected remaining artifacts in error, got: %v", err)
	}
	if _, statErr := os.Stat(manifestPath); statErr != nil {
		t.Fatalf("expected manifest to remain for retry when cleanup is incomplete")
	}
}
