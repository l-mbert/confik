package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestManifestWriteReadRoundTrip(t *testing.T) {
	base := t.TempDir()
	manifestPath := filepath.Join(base, manifestFilename)

	expected := Manifest{
		RunID:        "run-123",
		CreatedFiles: []string{"a.txt", "nested/b.txt"},
		CreatedDirs:  []string{"nested"},
		Gitignore: &GitContext{
			GitRoot:     "/tmp/repo",
			GitDir:      "/tmp/repo/.git",
			ExcludePath: "/tmp/repo/.git/info/exclude",
			RunID:       "run-123",
		},
		VSCode: &VSCodeContext{
			SettingsPath:        "/tmp/repo/.vscode/settings.json",
			AddedKeys:           []string{"a.txt"},
			FilesExcludeCreated: true,
			SettingsCreated:     false,
		},
		CreatedAt: "2024-01-01T00:00:00Z",
	}

	if err := writeManifest(manifestPath, expected); err != nil {
		t.Fatalf("writeManifest error: %v", err)
	}

	got, err := readManifest(manifestPath)
	if err != nil {
		t.Fatalf("readManifest error: %v", err)
	}
	if got.RunID != expected.RunID || got.CreatedAt != expected.CreatedAt {
		t.Fatalf("unexpected manifest header fields: got %#v", got)
	}
	if len(got.CreatedFiles) != len(expected.CreatedFiles) {
		t.Fatalf("unexpected CreatedFiles length: got %d want %d", len(got.CreatedFiles), len(expected.CreatedFiles))
	}
	for i := range expected.CreatedFiles {
		if got.CreatedFiles[i] != expected.CreatedFiles[i] {
			t.Fatalf("unexpected CreatedFiles[%d]: got %q want %q", i, got.CreatedFiles[i], expected.CreatedFiles[i])
		}
	}
	if got.Gitignore == nil || got.Gitignore.ExcludePath != expected.Gitignore.ExcludePath {
		t.Fatalf("unexpected git context: %#v", got.Gitignore)
	}
	if got.VSCode == nil || len(got.VSCode.AddedKeys) != 1 || got.VSCode.AddedKeys[0] != "a.txt" {
		t.Fatalf("unexpected vscode context: %#v", got.VSCode)
	}
}

func TestReadManifestInvalidJSON(t *testing.T) {
	base := t.TempDir()
	manifestPath := filepath.Join(base, manifestFilename)
	if err := os.WriteFile(manifestPath, []byte("{bad json"), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	if _, err := readManifest(manifestPath); err == nil {
		t.Fatalf("expected readManifest to fail for invalid json")
	}
}
