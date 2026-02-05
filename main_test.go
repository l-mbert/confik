package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseArgs(t *testing.T) {
	parsed, err := parseArgs([]string{"--dry-run", "--no-gitignore", "echo", "hi"})
	if err != nil {
		t.Fatalf("parseArgs error: %v", err)
	}
	if !parsed.Flags.DryRun || parsed.Flags.Gitignore {
		t.Fatalf("expected dryRun true and gitignore false")
	}
	if parsed.Command != "echo" {
		t.Fatalf("expected command echo, got %q", parsed.Command)
	}
	if len(parsed.CommandArgs) != 1 || parsed.CommandArgs[0] != "hi" {
		t.Fatalf("unexpected command args: %#v", parsed.CommandArgs)
	}

	_, err = parseArgs([]string{"--unknown"})
	if err == nil {
		t.Fatalf("expected error for unknown option")
	}
}

func TestMatchesPatternList(t *testing.T) {
	patterns := []string{"**/*.local", "private/**", "vite.config.*"}

	cases := []struct {
		name   string
		target string
		want   bool
	}{
		{"local file", "foo.local", true},
		{"nested local file", "config/foo.local", true},
		{"private path", "private/secret.txt", true},
		{"match base", "configs/vite.config.ts", true},
		{"non match", "config/other.ts", false},
	}

	for _, tc := range cases {
		if got := matchesPatternList(tc.target, patterns, true); got != tc.want {
			t.Fatalf("%s: expected %v, got %v", tc.name, tc.want, got)
		}
	}
}

func TestEnsureDir(t *testing.T) {
	base := t.TempDir()
	nested := filepath.Join(base, "a", "b", "c")
	created := []string{}

	ok, err := ensureDir(nested, &created, false)
	if err != nil {
		t.Fatalf("ensureDir error: %v", err)
	}
	if !ok {
		t.Fatalf("expected ok")
	}
	if _, err := os.Stat(nested); err != nil {
		t.Fatalf("expected directory created: %v", err)
	}
	if len(created) == 0 {
		t.Fatalf("expected created dirs recorded")
	}

	dryNested := filepath.Join(base, "x", "y")
	createdDry := []string{}
	ok, err = ensureDir(dryNested, &createdDry, true)
	if err != nil || !ok {
		t.Fatalf("dry-run ensureDir error: %v", err)
	}
	if _, err := os.Stat(dryNested); err == nil {
		t.Fatalf("expected dry-run to not create dirs")
	}
}

func TestGitIgnoreBlockHelpers(t *testing.T) {
	dir := t.TempDir()
	gitDir := filepath.Join(dir, ".git")
	infoDir := filepath.Join(gitDir, "info")
	if err := os.MkdirAll(infoDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	exclude := filepath.Join(infoDir, "exclude")

	paths := []string{"foo.txt", "bar/baz.json"}
	if _, err := appendGitIgnoreBlock(gitDir, "abc", paths); err != nil {
		t.Fatalf("appendGitIgnoreBlock error: %v", err)
	}

	content, _ := os.ReadFile(exclude)
	if !strings.Contains(string(content), "confik:start:abc") {
		t.Fatalf("expected block start")
	}
	if err := removeGitIgnoreBlock(exclude, "abc"); err != nil {
		t.Fatalf("removeGitIgnoreBlock error: %v", err)
	}
	content, _ = os.ReadFile(exclude)
	if strings.Contains(string(content), "confik:start:abc") {
		t.Fatalf("expected block removed")
	}

	// remove all blocks
	_, _ = appendGitIgnoreBlock(gitDir, "one", []string{"a"})
	_, _ = appendGitIgnoreBlock(gitDir, "two", []string{"b"})
	if err := removeAllGitIgnoreBlocks(exclude); err != nil {
		t.Fatalf("removeAllGitIgnoreBlocks error: %v", err)
	}
	content, _ = os.ReadFile(exclude)
	if strings.Contains(string(content), "confik:start:") {
		t.Fatalf("expected all blocks removed")
	}
}

func TestRemoveGitIgnoreBlocksString(t *testing.T) {
	content := strings.Join([]string{
		"line1",
		"# confik:start:abc",
		"foo",
		"# confik:end:abc",
		"line2",
		"# confik:start:def",
		"bar",
		"# confik:end:def",
		"line3",
	}, "\n") + "\n"

	updated := removeGitIgnoreBlocks(content, "abc")
	if strings.Contains(updated, "confik:start:abc") {
		t.Fatalf("expected abc block removed")
	}
	if !strings.Contains(updated, "confik:start:def") {
		t.Fatalf("expected def block to remain")
	}

	updatedAll := removeGitIgnoreBlocks(content, "")
	if strings.Contains(updatedAll, "confik:start:") {
		t.Fatalf("expected all blocks removed")
	}
}

func TestLoadConfigDefaultsAndOverrides(t *testing.T) {
	base := t.TempDir()
	configDir := filepath.Join(base, ".config")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	defaults := loadConfig(configDir)
	if !defaults.Registry || !defaults.Gitignore {
		t.Fatalf("expected default registry/gitignore true")
	}

	cfg := `{"exclude":["**/*.local"],"gitignore":false}`
	if err := os.WriteFile(filepath.Join(configDir, configFilename), []byte(cfg), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	loaded := loadConfig(configDir)
	if len(loaded.Exclude) != 1 || loaded.Exclude[0] != "**/*.local" {
		t.Fatalf("expected exclude to load")
	}
	if loaded.Gitignore {
		t.Fatalf("expected gitignore false")
	}
	if !loaded.Registry {
		t.Fatalf("expected registry default true")
	}
}

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

func TestLoadRegistryPatterns(t *testing.T) {
	patterns := loadRegistryPatterns()
	if len(patterns) == 0 {
		t.Fatalf("expected embedded registry patterns")
	}
	found := false
	for _, p := range patterns {
		if p == "confik.json" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected registry to include confik.json")
	}
}
