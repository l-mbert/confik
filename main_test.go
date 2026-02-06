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
	"time"
)

func TestParseArgs(t *testing.T) {
	t.Run("flags-and-command", func(t *testing.T) {
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
	})

	t.Run("unknown-option", func(t *testing.T) {
		_, err := parseArgs([]string{"--unknown"})
		if err == nil {
			t.Fatalf("expected error for unknown option")
		}
	})

	t.Run("double-dash-separator", func(t *testing.T) {
		parsed, err := parseArgs([]string{"--dry-run", "--", "npm", "run", "build"})
		if err != nil {
			t.Fatalf("parseArgs error: %v", err)
		}
		if !parsed.Flags.DryRun {
			t.Fatalf("expected dryRun true")
		}
		if parsed.Command != "npm" {
			t.Fatalf("expected command npm, got %q", parsed.Command)
		}
		if len(parsed.CommandArgs) != 2 || parsed.CommandArgs[0] != "run" || parsed.CommandArgs[1] != "build" {
			t.Fatalf("unexpected command args: %#v", parsed.CommandArgs)
		}
	})

	t.Run("help-flag", func(t *testing.T) {
		parsed, err := parseArgs([]string{"-h"})
		if err != nil {
			t.Fatalf("parseArgs error: %v", err)
		}
		if !parsed.Flags.Help {
			t.Fatalf("expected help true")
		}

		parsed, err = parseArgs([]string{"--help"})
		if err != nil {
			t.Fatalf("parseArgs error: %v", err)
		}
		if !parsed.Flags.Help {
			t.Fatalf("expected help true")
		}
	})

	t.Run("clean-flag", func(t *testing.T) {
		parsed, err := parseArgs([]string{"--clean"})
		if err != nil {
			t.Fatalf("parseArgs error: %v", err)
		}
		if !parsed.Flags.Clean {
			t.Fatalf("expected clean true")
		}
	})

	t.Run("no-registry-flag", func(t *testing.T) {
		parsed, err := parseArgs([]string{"--no-registry", "echo"})
		if err != nil {
			t.Fatalf("parseArgs error: %v", err)
		}
		if parsed.Flags.Registry {
			t.Fatalf("expected registry false")
		}
	})

	t.Run("no-args", func(t *testing.T) {
		parsed, err := parseArgs([]string{})
		if err != nil {
			t.Fatalf("parseArgs error: %v", err)
		}
		if parsed.Command != "" {
			t.Fatalf("expected empty command, got %q", parsed.Command)
		}
		if !parsed.Flags.Gitignore || !parsed.Flags.Registry {
			t.Fatalf("expected default flags")
		}
	})

	t.Run("double-dash-only", func(t *testing.T) {
		parsed, err := parseArgs([]string{"--"})
		if err != nil {
			t.Fatalf("parseArgs error: %v", err)
		}
		if parsed.Command != "" {
			t.Fatalf("expected empty command after bare --, got %q", parsed.Command)
		}
	})
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

func TestNoConfigStandaloneExitsCleanly(t *testing.T) {
	dir := t.TempDir()
	code, _, stderr := runConfik(t, dir)
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d", code)
	}
	if !strings.Contains(stderr, "no .config directory found") {
		t.Fatalf("expected warning about missing .config, got: %s", stderr)
	}
	if strings.Contains(stderr, "missing command") {
		t.Fatalf("did not expect missing command error, got: %s", stderr)
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

func TestStandaloneModeStagesAndCleansOnInterrupt(t *testing.T) {
	dir := t.TempDir()
	configDir := filepath.Join(dir, ".config")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("mkdir config: %v", err)
	}

	sourcePath := filepath.Join(configDir, "example.txt")
	if err := os.WriteFile(sourcePath, []byte("hello"), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}

	code, _, stderr := runConfikUntilStagedThenInterrupt(t, dir, filepath.Join(dir, "example.txt"))
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

func TestDryRunDoesNotWriteFiles(t *testing.T) {
	dir := t.TempDir()
	configDir := filepath.Join(dir, ".config")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("mkdir config: %v", err)
	}

	sourcePath := filepath.Join(configDir, "example.txt")
	if err := os.WriteFile(sourcePath, []byte("hello"), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}

	code, stdout, stderr := runConfik(t, dir, "--dry-run", testCommandArgs(0)[0], testCommandArgs(0)[1], testCommandArgs(0)[2], testCommandArgs(0)[3])
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d (stderr: %s)", code, stderr)
	}

	if _, err := os.Stat(filepath.Join(dir, "example.txt")); err == nil {
		t.Fatalf("expected no staged file in dry-run mode")
	}

	if _, err := os.Stat(filepath.Join(configDir, manifestFilename)); err == nil {
		t.Fatalf("expected no manifest in dry-run mode")
	}

	combined := stdout + stderr
	if !strings.Contains(combined, "dry-run") {
		t.Fatalf("expected dry-run output, got: stdout=%s stderr=%s", stdout, stderr)
	}
}

func TestDryRunWithNoFiles(t *testing.T) {
	dir := t.TempDir()
	configDir := filepath.Join(dir, ".config")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("mkdir config: %v", err)
	}

	code, stdout, _ := runConfik(t, dir, "--dry-run", testCommandArgs(0)[0], testCommandArgs(0)[1], testCommandArgs(0)[2], testCommandArgs(0)[3])
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d", code)
	}
	if !strings.Contains(stdout, "No files to stage") {
		t.Fatalf("expected 'No files to stage' message, got stdout: %s", stdout)
	}
}

func TestRegistrySkipsKnownPatterns(t *testing.T) {
	dir := t.TempDir()
	configDir := filepath.Join(dir, ".config")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("mkdir config: %v", err)
	}

	// cspell.json matches the registry pattern "cspell*.json"
	registryFile := filepath.Join(configDir, "cspell.json")
	if err := os.WriteFile(registryFile, []byte(`{"words":[]}`), 0o644); err != nil {
		t.Fatalf("write registry file: %v", err)
	}

	// A normal file that should be staged
	normalFile := filepath.Join(configDir, "myconfig.txt")
	if err := os.WriteFile(normalFile, []byte("data"), 0o644); err != nil {
		t.Fatalf("write normal file: %v", err)
	}

	code, stdout, stderr := runConfik(t, dir, testCommandArgs(0)...)
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d (stderr: %s)", code, stderr)
	}

	// The registry file should NOT have been staged
	if _, err := os.Stat(filepath.Join(dir, "cspell.json")); err == nil {
		t.Fatalf("expected registry-matched file to be skipped")
	}

	// The normal file should have been staged and cleaned up
	combined := stdout + stderr
	if !strings.Contains(combined, "registry-skipped") {
		t.Fatalf("expected registry-skipped in output, got: %s", combined)
	}
}

func TestRegistryOverrideForcesCopy(t *testing.T) {
	dir := t.TempDir()
	configDir := filepath.Join(dir, ".config")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("mkdir config: %v", err)
	}

	// Write a config that overrides cspell.json
	cfg := `{"registryOverride":["cspell.json"]}`
	if err := os.WriteFile(filepath.Join(configDir, configFilename), []byte(cfg), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	// cspell.json matches the registry but is overridden
	registryFile := filepath.Join(configDir, "cspell.json")
	if err := os.WriteFile(registryFile, []byte(`{"words":[]}`), 0o644); err != nil {
		t.Fatalf("write registry file: %v", err)
	}

	code, stdout, stderr := runConfik(t, dir, testCommandArgs(0)...)
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d (stderr: %s)", code, stderr)
	}

	// The registry file should have been staged (override) and then cleaned up
	combined := stdout + stderr
	if strings.Contains(combined, "registry-skipped") {
		t.Fatalf("expected no registry-skipped for overridden file, got: %s", combined)
	}
	if !strings.Contains(combined, "staged 1 file") {
		t.Fatalf("expected 'staged 1 file' in output, got: %s", combined)
	}
}

func TestRegistryDisabledStagesAll(t *testing.T) {
	dir := t.TempDir()
	configDir := filepath.Join(dir, ".config")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("mkdir config: %v", err)
	}

	// cspell.json matches the registry, but registry is disabled via flag
	registryFile := filepath.Join(configDir, "cspell.json")
	if err := os.WriteFile(registryFile, []byte(`{"words":[]}`), 0o644); err != nil {
		t.Fatalf("write registry file: %v", err)
	}

	code, stdout, stderr := runConfik(t, dir, append([]string{"--no-registry"}, testCommandArgs(0)...)...)
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d (stderr: %s)", code, stderr)
	}

	combined := stdout + stderr
	if strings.Contains(combined, "registry-skipped") {
		t.Fatalf("expected no registry-skipped when registry disabled, got: %s", combined)
	}
	if !strings.Contains(combined, "staged 1 file") {
		t.Fatalf("expected file to be staged when registry disabled, got: %s", combined)
	}
}

func TestExistingFileIsSkipped(t *testing.T) {
	dir := t.TempDir()
	configDir := filepath.Join(dir, ".config")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("mkdir config: %v", err)
	}

	// Create a file in .config
	if err := os.WriteFile(filepath.Join(configDir, "existing.txt"), []byte("from config"), 0o644); err != nil {
		t.Fatalf("write config file: %v", err)
	}

	// Pre-create the same file in the project root
	existingPath := filepath.Join(dir, "existing.txt")
	if err := os.WriteFile(existingPath, []byte("original"), 0o644); err != nil {
		t.Fatalf("write existing file: %v", err)
	}

	code, stdout, stderr := runConfik(t, dir, testCommandArgs(0)...)
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d (stderr: %s)", code, stderr)
	}

	// Verify the existing file was not overwritten
	content, err := os.ReadFile(existingPath)
	if err != nil {
		t.Fatalf("read existing file: %v", err)
	}
	if string(content) != "original" {
		t.Fatalf("expected existing file to remain unchanged, got %q", string(content))
	}

	combined := stdout + stderr
	if !strings.Contains(combined, "skipped 1 existing") {
		t.Fatalf("expected 'skipped 1 existing' in output, got: %s", combined)
	}
}

func TestHelpFlagExitsCleanly(t *testing.T) {
	dir := t.TempDir()
	code, stdout, _ := runConfik(t, dir, "--help")
	if code != 0 {
		t.Fatalf("expected exit code 0 for --help, got %d", code)
	}
	if !strings.Contains(stdout, "confik") || !strings.Contains(stdout, "--dry-run") {
		t.Fatalf("expected help text, got: %s", stdout)
	}
}

func TestCleanFlagEndToEnd(t *testing.T) {
	dir := t.TempDir()
	configDir := filepath.Join(dir, ".config")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("mkdir config: %v", err)
	}

	// Create a leftover staged file
	leftover := filepath.Join(dir, "leftover.txt")
	if err := os.WriteFile(leftover, []byte("stale"), 0o644); err != nil {
		t.Fatalf("write leftover: %v", err)
	}

	// Create a manifest pointing to the leftover
	manifest := Manifest{
		RunID:        "test-clean",
		CreatedFiles: []string{"leftover.txt"},
		CreatedDirs:  []string{},
		CreatedAt:    "2024-01-01T00:00:00Z",
	}
	manifestPath := filepath.Join(configDir, manifestFilename)
	if err := writeManifest(manifestPath, manifest); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	code, stdout, stderr := runConfik(t, dir, "--clean")
	if code != 0 {
		t.Fatalf("expected exit code 0 for --clean, got %d (stderr: %s)", code, stderr)
	}

	if _, err := os.Stat(leftover); err == nil {
		t.Fatalf("expected leftover file to be removed")
	}
	if _, err := os.Stat(manifestPath); err == nil {
		t.Fatalf("expected manifest to be removed")
	}
	if !strings.Contains(stdout, "cleanup complete") {
		t.Fatalf("expected cleanup complete message, got: %s", stdout)
	}
}

func TestCleanWithNoConfigDir(t *testing.T) {
	dir := t.TempDir()
	code, _, stderr := runConfik(t, dir, "--clean")
	if code != 0 {
		t.Fatalf("expected exit code 0 for --clean with no .config, got %d", code)
	}
	if !strings.Contains(stderr, "no .config directory found") {
		t.Fatalf("expected warning about missing .config, got: %s", stderr)
	}
}

func TestEmptyConfigDirStagesNothing(t *testing.T) {
	dir := t.TempDir()
	configDir := filepath.Join(dir, ".config")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("mkdir config: %v", err)
	}

	code, _, stderr := runConfik(t, dir, testCommandArgs(0)...)
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d (stderr: %s)", code, stderr)
	}

	if _, err := os.Stat(filepath.Join(configDir, manifestFilename)); err == nil {
		t.Fatalf("expected no manifest for empty .config")
	}
}

func TestNestedDirectoryStaging(t *testing.T) {
	dir := t.TempDir()
	configDir := filepath.Join(dir, ".config")
	nested := filepath.Join(configDir, "a", "b", "c")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatalf("mkdir nested: %v", err)
	}

	deepFile := filepath.Join(nested, "deep.txt")
	if err := os.WriteFile(deepFile, []byte("deep content"), 0o644); err != nil {
		t.Fatalf("write deep file: %v", err)
	}

	topFile := filepath.Join(configDir, "top.txt")
	if err := os.WriteFile(topFile, []byte("top content"), 0o644); err != nil {
		t.Fatalf("write top file: %v", err)
	}

	code, _, stderr := runConfik(t, dir, testCommandArgs(0)...)
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d (stderr: %s)", code, stderr)
	}

	// Verify both files were staged and cleaned up
	if _, err := os.Stat(filepath.Join(dir, "top.txt")); err == nil {
		t.Fatalf("expected top staged file to be cleaned up")
	}
	if _, err := os.Stat(filepath.Join(dir, "a", "b", "c", "deep.txt")); err == nil {
		t.Fatalf("expected deep staged file to be cleaned up")
	}

	// Verify nested directories were cleaned up
	if _, err := os.Stat(filepath.Join(dir, "a")); err == nil {
		t.Fatalf("expected created directory 'a' to be cleaned up")
	}

	// Verify source files remain
	if _, err := os.Stat(deepFile); err != nil {
		t.Fatalf("expected source deep file to remain")
	}
	if _, err := os.Stat(topFile); err != nil {
		t.Fatalf("expected source top file to remain")
	}
}

func TestExcludePatternSkipsFiles(t *testing.T) {
	dir := t.TempDir()
	configDir := filepath.Join(dir, ".config")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("mkdir config: %v", err)
	}

	cfg := `{"exclude":["**/*.secret"]}`
	if err := os.WriteFile(filepath.Join(configDir, configFilename), []byte(cfg), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	if err := os.WriteFile(filepath.Join(configDir, "keep.txt"), []byte("keep"), 0o644); err != nil {
		t.Fatalf("write keep file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "hidden.secret"), []byte("secret"), 0o644); err != nil {
		t.Fatalf("write secret file: %v", err)
	}

	code, stdout, stderr := runConfik(t, dir, testCommandArgs(0)...)
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d (stderr: %s)", code, stderr)
	}

	combined := stdout + stderr
	if !strings.Contains(combined, "excluded 1 file") {
		t.Fatalf("expected 'excluded 1 file' in output, got: %s", combined)
	}
	if !strings.Contains(combined, "staged 1 file") {
		t.Fatalf("expected 'staged 1 file' in output, got: %s", combined)
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

func runConfikUntilStagedThenInterrupt(t *testing.T, dir string, stagedFile string, args ...string) (int, string, string) {
	t.Helper()

	cmdArgs := append([]string{"-test.run=TestHelperProcess", "--"}, args...)
	cmd := exec.Command(os.Args[0], cmdArgs...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "CONFIK_HELPER=1", "CONFIK_CMD=1")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		t.Fatalf("failed to start confik: %v", err)
	}

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(stagedFile); err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if _, err := os.Stat(stagedFile); err != nil {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
		t.Fatalf("staged file did not appear before timeout")
	}

	if err := cmd.Process.Signal(os.Interrupt); err != nil {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
		t.Fatalf("failed to interrupt confik process: %v", err)
	}

	err := cmd.Wait()
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
