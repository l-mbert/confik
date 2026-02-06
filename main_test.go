package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
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

func TestLoadRegistryPatterns(t *testing.T) {
	patterns := loadRegistryPatterns()
	if len(patterns) == 0 {
		t.Fatalf("expected embedded registry patterns")
	}
	found := slices.Contains(patterns, "confik.json")
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

func TestStagingFailureRollsBackPartialFiles(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission-based unreadable file test is not reliable on windows")
	}

	dir := t.TempDir()
	configDir := filepath.Join(dir, ".config")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("mkdir config: %v", err)
	}

	readable := filepath.Join(configDir, "a.txt")
	if err := os.WriteFile(readable, []byte("ok"), 0o644); err != nil {
		t.Fatalf("write readable file: %v", err)
	}

	unreadable := filepath.Join(configDir, "b.txt")
	if err := os.WriteFile(unreadable, []byte("secret"), 0o644); err != nil {
		t.Fatalf("write unreadable file: %v", err)
	}
	if err := os.Chmod(unreadable, 0); err != nil {
		t.Skipf("unable to set unreadable permissions: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(unreadable, 0o644) })

	code, _, _ := runConfik(t, dir, testCommandArgs(0)...)
	if code == 0 {
		t.Fatalf("expected non-zero exit code for staging failure")
	}

	if _, err := os.Stat(filepath.Join(dir, "a.txt")); err == nil {
		t.Fatalf("expected partial staged file rollback")
	}
	if _, err := os.Stat(filepath.Join(configDir, manifestFilename)); err == nil {
		t.Fatalf("expected no manifest after failed staging cleanup")
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

func TestVSCodeExcludeCleanupKeepsJSONCCommentOnly(t *testing.T) {
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
	withComment := append([]byte("// user note\n"), content...)
	if err := os.WriteFile(settingsPath, withComment, 0o644); err != nil {
		t.Fatalf("write settings: %v", err)
	}

	if err := removeVSCodeExcludes(ctx); err != nil {
		t.Fatalf("removeVSCodeExcludes: %v", err)
	}

	after, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("expected settings.json to remain")
	}
	if !strings.Contains(string(after), "// user note") {
		t.Fatalf("expected user comment to remain")
	}
	if strings.Contains(string(after), "files.exclude") {
		t.Fatalf("expected files.exclude to be removed")
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
		return exitErr.ExitCode(), stdout.String(), stderr.String()
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
