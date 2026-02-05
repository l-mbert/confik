package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestVSCodeApplyPreservesJSONCComments(t *testing.T) {
	dir := t.TempDir()
	settingsDir := filepath.Join(dir, ".vscode")
	if err := os.MkdirAll(settingsDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	settingsPath := filepath.Join(settingsDir, "settings.json")
	initial := `{
  // note
  "files.exclude": {
    "keep.txt": true
  }
}
`
	if err := os.WriteFile(settingsPath, []byte(initial), 0o644); err != nil {
		t.Fatalf("write settings: %v", err)
	}

	createdFiles := []string{}
	createdDirs := []string{}
	ctx, err := applyVSCodeExcludes(dir, []string{filepath.Join(dir, "example.txt")}, &createdFiles, &createdDirs)
	if err != nil {
		t.Fatalf("applyVSCodeExcludes: %v", err)
	}
	if ctx == nil {
		t.Fatalf("expected context")
	}

	afterApply, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("read settings: %v", err)
	}
	content := string(afterApply)
	if !strings.Contains(content, "// note") {
		t.Fatalf("expected comment to remain after apply")
	}
	if !strings.Contains(content, `"keep.txt"`) {
		t.Fatalf("expected existing key to remain")
	}
	if !strings.Contains(content, `"example.txt"`) {
		t.Fatalf("expected staged key to be added")
	}

	if err := removeVSCodeExcludes(ctx); err != nil {
		t.Fatalf("removeVSCodeExcludes: %v", err)
	}

	afterRemove, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("read settings after remove: %v", err)
	}
	content = string(afterRemove)
	if !strings.Contains(content, "// note") {
		t.Fatalf("expected comment to remain after remove")
	}
	if !strings.Contains(content, `"keep.txt"`) {
		t.Fatalf("expected existing key to remain after remove")
	}
	if strings.Contains(content, `"example.txt"`) {
		t.Fatalf("expected staged key to be removed")
	}
}

func TestVSCodeApplySkipsNonObjectFilesExclude(t *testing.T) {
	dir := t.TempDir()
	settingsDir := filepath.Join(dir, ".vscode")
	if err := os.MkdirAll(settingsDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	settingsPath := filepath.Join(settingsDir, "settings.json")
	initial := `{"files.exclude": true}`
	if err := os.WriteFile(settingsPath, []byte(initial), 0o644); err != nil {
		t.Fatalf("write settings: %v", err)
	}

	createdFiles := []string{}
	createdDirs := []string{}
	ctx, err := applyVSCodeExcludes(dir, []string{filepath.Join(dir, "example.txt")}, &createdFiles, &createdDirs)
	if err != nil {
		t.Fatalf("applyVSCodeExcludes: %v", err)
	}
	if ctx != nil {
		t.Fatalf("expected nil context when files.exclude is not an object")
	}

	after, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("read settings: %v", err)
	}
	if strings.TrimSpace(string(after)) != initial {
		t.Fatalf("expected settings.json unchanged")
	}
}

func TestVSCodeApplyKeepsMinifiedFormat(t *testing.T) {
	dir := t.TempDir()
	settingsDir := filepath.Join(dir, ".vscode")
	if err := os.MkdirAll(settingsDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	settingsPath := filepath.Join(settingsDir, "settings.json")
	initial := `{"files.exclude":{"keep.txt":true}}`
	if err := os.WriteFile(settingsPath, []byte(initial), 0o644); err != nil {
		t.Fatalf("write settings: %v", err)
	}

	createdFiles := []string{}
	createdDirs := []string{}
	ctx, err := applyVSCodeExcludes(dir, []string{filepath.Join(dir, "example.txt")}, &createdFiles, &createdDirs)
	if err != nil {
		t.Fatalf("applyVSCodeExcludes: %v", err)
	}
	if ctx == nil {
		t.Fatalf("expected context")
	}

	after, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("read settings: %v", err)
	}
	if strings.Contains(string(after), "\n") {
		t.Fatalf("expected minified formatting to remain")
	}
}
