package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
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
	if defaults.VSCodeExclude {
		t.Fatalf("expected vscodeExclude default false")
	}

	cfg := `{"exclude":["**/*.local"],"gitignore":false,"vscodeExclude":true}`
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
	if !loaded.VSCodeExclude {
		t.Fatalf("expected vscodeExclude true")
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

func TestNoConfigWarnsAndRunsCommand(t *testing.T) {
	dir := t.TempDir()
	code, _, stderr := runConfik(t, dir, testCommandArgs(0)...)
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d", code)
	}
	if !strings.Contains(stderr, "no .config directory found") {
		t.Fatalf("expected warning about missing .config, got: %s", stderr)
	}
	if _, err := os.Stat(filepath.Join(dir, ".config")); err == nil {
		t.Fatalf("did not expect .config to be created")
	}
}

func TestNoConfigPropagatesExitCode(t *testing.T) {
	dir := t.TempDir()
	code, _, _ := runConfik(t, dir, testCommandArgs(7)...)
	if code != 7 {
		t.Fatalf("expected exit code 7, got %d", code)
	}
}

func TestStagingCopiesAndCleans(t *testing.T) {
	dir := t.TempDir()
	configDir := filepath.Join(dir, ".config")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("mkdir config: %v", err)
	}

	sourcePath := filepath.Join(configDir, "example.txt")
	if err := os.WriteFile(sourcePath, []byte("hello"), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}

	code, _, stderr := runConfik(t, dir, testCommandArgs(0)...)
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d (stderr: %s)", code, stderr)
	}

	if _, err := os.Stat(filepath.Join(dir, "example.txt")); err == nil {
		t.Fatalf("expected staged file to be removed")
	}

	if _, err := os.Stat(filepath.Join(configDir, "example.txt")); err != nil {
		t.Fatalf("expected source file to remain in .config")
	}

	if _, err := os.Stat(filepath.Join(configDir, manifestFilename)); err == nil {
		t.Fatalf("expected manifest to be removed")
	}
}

func TestVSCodeExcludeCleanupKeepsUserEdits(t *testing.T) {
	dir := t.TempDir()
	staged := filepath.Join(dir, "example.txt")

	createdFiles := []string{}
	createdDirs := []string{}
	ctx, err := applyVSCodeExcludes(dir, []string{staged}, &createdFiles, &createdDirs)
	if err != nil {
		t.Fatalf("applyVSCodeExcludes error: %v", err)
	}
	if ctx == nil || !ctx.SettingsCreated {
		t.Fatalf("expected settings to be created")
	}

	settingsPath := filepath.Join(dir, ".vscode", "settings.json")
	content, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("read settings: %v", err)
	}
	settings := map[string]any{}
	if err := json.Unmarshal(content, &settings); err != nil {
		t.Fatalf("parse settings: %v", err)
	}
	settings["editor.tabSize"] = 2
	updated, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		t.Fatalf("marshal settings: %v", err)
	}
	updated = append(updated, '\n')
	if err := os.WriteFile(settingsPath, updated, 0o644); err != nil {
		t.Fatalf("write settings: %v", err)
	}

	if err := removeVSCodeExcludes(ctx); err != nil {
		t.Fatalf("removeVSCodeExcludes: %v", err)
	}

	after, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("expected settings.json to remain")
	}
	finalSettings := map[string]any{}
	if err := json.Unmarshal(after, &finalSettings); err != nil {
		t.Fatalf("parse settings after: %v", err)
	}
	if _, ok := finalSettings["editor.tabSize"]; !ok {
		t.Fatalf("expected user setting to remain")
	}
	if _, ok := finalSettings["files.exclude"]; ok {
		t.Fatalf("expected files.exclude to be removed")
	}
}

func TestVSCodeExcludeCleanupRemovesEmptySettings(t *testing.T) {
	dir := t.TempDir()
	staged := filepath.Join(dir, "example.txt")

	createdFiles := []string{}
	createdDirs := []string{}
	ctx, err := applyVSCodeExcludes(dir, []string{staged}, &createdFiles, &createdDirs)
	if err != nil {
		t.Fatalf("applyVSCodeExcludes error: %v", err)
	}
	if ctx == nil || !ctx.SettingsCreated {
		t.Fatalf("expected settings to be created")
	}

	if err := removeVSCodeExcludes(ctx); err != nil {
		t.Fatalf("removeVSCodeExcludes: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, ".vscode", "settings.json")); err == nil {
		t.Fatalf("expected settings.json to be removed")
	}
	if _, err := os.Stat(filepath.Join(dir, ".vscode")); err == nil {
		t.Fatalf("expected .vscode directory to be removed")
	}
}

func runConfik(t *testing.T, dir string, args ...string) (int, string, string) {
	t.Helper()

	cmdArgs := append([]string{"-test.run=TestHelperProcess", "--"}, args...)
	cmd := exec.Command(os.Args[0], cmdArgs...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "CONFIK_HELPER=1", "CONFIK_CMD=1")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err == nil {
		return 0, stdout.String(), stderr.String()
	}
	if exitErr, ok := err.(*exec.ExitError); ok {
		return exitErr.ProcessState.ExitCode(), stdout.String(), stderr.String()
	}
	t.Fatalf("unexpected error running confik: %v", err)
	return 1, stdout.String(), stderr.String()
}

func testCommandArgs(exitCode int) []string {
	return []string{os.Args[0], "-test.run=TestHelperCommand", "--", strconv.Itoa(exitCode)}
}

func TestHelperProcess(t *testing.T) {
	if os.Getenv("CONFIK_HELPER") != "1" {
		return
	}
	args := []string{"confik"}
	for i, arg := range os.Args {
		if arg == "--" && i+1 < len(os.Args) {
			args = append(args, os.Args[i+1:]...)
			break
		}
	}
	os.Args = args
	if err := run(); err != nil {
		os.Exit(1)
	}
	os.Exit(0)
}

func TestHelperCommand(t *testing.T) {
	if os.Getenv("CONFIK_CMD") != "1" {
		return
	}
	code := 0
	for i, arg := range os.Args {
		if arg == "--" && i+1 < len(os.Args) {
			if parsed, err := strconv.Atoi(os.Args[i+1]); err == nil {
				code = parsed
			}
			break
		}
	}
	os.Exit(code)
}
