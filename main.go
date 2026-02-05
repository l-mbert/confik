package main

import (
	"crypto/rand"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"os/signal"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/bmatcuk/doublestar/v4"
)

//go:embed registry.json
var embeddedRegistry []byte

const (
	configFilename   = "confik.json"
	manifestFilename = ".confik-manifest.json"
	lockFilename     = ".confik.lock"
)

type CLIFlags struct {
	DryRun    bool
	Clean     bool
	Gitignore bool
	Registry  bool
	Help      bool
}

type ParsedArgs struct {
	Flags       CLIFlags
	Command     string
	CommandArgs []string
}

type ConfigFile struct {
	Exclude          []string `json:"exclude"`
	Registry         *bool    `json:"registry"`
	RegistryOverride []string `json:"registryOverride"`
	Gitignore        *bool    `json:"gitignore"`
}

type ConfikConfig struct {
	Exclude          []string
	Registry         bool
	RegistryOverride []string
	Gitignore        bool
	Path             string
}

type GitContext struct {
	GitRoot     string `json:"gitRoot"`
	GitDir      string `json:"gitDir"`
	ExcludePath string `json:"excludePath"`
	RunID       string `json:"runId"`
}

type Manifest struct {
	RunID        string      `json:"runId"`
	CreatedFiles []string    `json:"createdFiles"`
	CreatedDirs  []string    `json:"createdDirs"`
	Gitignore    *GitContext `json:"gitignore"`
	CreatedAt    string      `json:"createdAt"`
}

type RegistryPayload struct {
	Patterns []string `json:"patterns"`
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "confik: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}

	parsed, err := parseArgs(os.Args[1:])
	if err != nil {
		return err
	}

	if parsed.Flags.Help {
		printHelp()
		return nil
	}

	configDir := filepath.Join(cwd, ".config")
	if !isDirectory(configDir) {
		fmt.Fprintln(os.Stderr, "confik: no .config directory found, continuing without staging")
		if parsed.Flags.Clean {
			return nil
		}
		if parsed.Command == "" {
			printHelp()
			return errors.New("missing command")
		}
		return runCommandAndExit(parsed.Command, parsed.CommandArgs, func() {})
	}

	lockPath := filepath.Join(configDir, lockFilename)
	lock, err := acquireLock(lockPath)
	if err != nil {
		return err
	}
	lockHeld := true
	unlock := func() {
		if lockHeld {
			_ = lock.Unlock()
			lockHeld = false
		}
	}

	if parsed.Flags.Clean {
		defer unlock()
		return cleanLeftovers(cwd, true, false)
	}

	if parsed.Command == "" {
		defer unlock()
		printHelp()
		return errors.New("missing command")
	}

	if exists(filepath.Join(configDir, manifestFilename)) {
		_ = cleanLeftovers(cwd, false, true)
	}

	config := loadConfig(configDir)

	useGitignore := parsed.Flags.Gitignore && config.Gitignore
	useRegistry := parsed.Flags.Registry && config.Registry

	registryPatterns := []string{}
	if useRegistry {
		registryPatterns = loadRegistryPatterns()
	}

	createdFiles := []string{}
	createdDirs := []string{}
	skippedExisting := []string{}
	skippedExcluded := []string{}
	skippedRegistry := []string{}

	runID := createRunID()
	manifestPath := filepath.Join(configDir, manifestFilename)

	filesToCopy := []string{}
	if isDirectory(configDir) {
		_ = filepath.WalkDir(configDir, func(pathname string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return nil
			}
			if d.IsDir() {
				return nil
			}
			filesToCopy = append(filesToCopy, pathname)
			return nil
		})
	}

	for _, filePath := range filesToCopy {
		rel, err := filepath.Rel(configDir, filePath)
		if err != nil {
			continue
		}

		relPosix := filepath.ToSlash(rel)
		if relPosix == configFilename || relPosix == manifestFilename || relPosix == lockFilename {
			continue
		}

		if matchesPatternList(relPosix, config.Exclude, true) {
			skippedExcluded = append(skippedExcluded, relPosix)
			continue
		}

		if useRegistry && matchesPatternList(relPosix, registryPatterns, true) {
			if !matchesPatternList(relPosix, config.RegistryOverride, true) {
				skippedRegistry = append(skippedRegistry, relPosix)
				continue
			}
		}

		dest := filepath.Join(cwd, rel)
		if exists(dest) {
			skippedExisting = append(skippedExisting, relPosix)
			continue
		}

		ok, err := ensureDir(filepath.Dir(dest), &createdDirs, parsed.Flags.DryRun)
		if err != nil || !ok {
			skippedExisting = append(skippedExisting, relPosix)
			continue
		}

		if !parsed.Flags.DryRun {
			if err := copyFile(filePath, dest); err != nil {
				return err
			}
		}
		createdFiles = append(createdFiles, dest)
	}

	var gitContext *GitContext
	if !parsed.Flags.DryRun && useGitignore && len(createdFiles) > 0 {
		gitRoot := findGitRoot(cwd)
		if gitRoot != "" {
			gitDir, err := resolveGitDir(gitRoot)
			if err == nil {
				relPaths := []string{}
				for _, filePath := range createdFiles {
					rel, err := filepath.Rel(gitRoot, filePath)
					if err != nil {
						continue
					}
					if strings.HasPrefix(rel, "..") {
						continue
					}
					relPaths = append(relPaths, filepath.ToSlash(rel))
				}
				if len(relPaths) > 0 {
					excludePath, err := appendGitIgnoreBlock(gitDir, runID, relPaths)
					if err == nil {
						gitContext = &GitContext{
							GitRoot:     gitRoot,
							GitDir:      gitDir,
							ExcludePath: excludePath,
							RunID:       runID,
						}
					}
				}
			}
		}
	}

	manifest := Manifest{
		RunID:        runID,
		CreatedFiles: toRelativeList(cwd, createdFiles),
		CreatedDirs:  toRelativeList(cwd, createdDirs),
		Gitignore:    gitContext,
		CreatedAt:    time.Now().UTC().Format(time.RFC3339),
	}

	if !parsed.Flags.DryRun && len(createdFiles) > 0 {
		if err := writeManifest(manifestPath, manifest); err != nil {
			return err
		}
	}

	printSummary(parsed.Flags.DryRun, createdFiles, skippedExisting, skippedExcluded, skippedRegistry)

	if parsed.Flags.DryRun {
		unlock()
		if len(createdFiles) > 0 {
			fmt.Fprintln(os.Stdout, "confik: dry-run complete. No files were written.")
		} else {
			fmt.Fprintln(os.Stdout, "confik: dry-run complete. No files to stage.")
		}
		return nil
	}

	cleanupOnce := sync.Once{}
	cleanup := func() {
		cleanupOnce.Do(func() {
			for _, filePath := range createdFiles {
				_ = os.Remove(filePath)
			}

			uniqueDirs := uniqueStrings(createdDirs)
			sort.Slice(uniqueDirs, func(i, j int) bool { return len(uniqueDirs[i]) > len(uniqueDirs[j]) })
			for _, dirPath := range uniqueDirs {
				removeDirIfEmpty(dirPath)
			}

			if gitContext != nil {
				_ = removeGitIgnoreBlock(gitContext.ExcludePath, gitContext.RunID)
			}

			_ = os.Remove(manifestPath)
			unlock()
		})
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	go func() {
		sig := <-sigCh
		fmt.Fprintf(os.Stderr, "confik: received %s, cleaning up...\n", sig.String())
		cleanup()
		os.Exit(1)
	}()

	return runCommandAndExit(parsed.Command, parsed.CommandArgs, cleanup)
}

func parseArgs(args []string) (ParsedArgs, error) {
	flags := CLIFlags{DryRun: false, Clean: false, Gitignore: true, Registry: true, Help: false}
	cmdIndex := -1

	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			cmdIndex = i + 1
			break
		}
		switch arg {
		case "-h", "--help":
			flags.Help = true
		case "--dry-run":
			flags.DryRun = true
		case "--clean":
			flags.Clean = true
		case "--no-gitignore":
			flags.Gitignore = false
		case "--no-registry":
			flags.Registry = false
		default:
			if strings.HasPrefix(arg, "-") {
				return ParsedArgs{}, fmt.Errorf("unknown option: %s", arg)
			}
			cmdIndex = i
			i = len(args)
		}
	}

	if cmdIndex < 0 || cmdIndex >= len(args) {
		return ParsedArgs{Flags: flags}, nil
	}

	return ParsedArgs{
		Flags:       flags,
		Command:     args[cmdIndex],
		CommandArgs: args[cmdIndex+1:],
	}, nil
}

func printHelp() {
	msg := fmt.Sprintf(`confik - stage .config files in project root while running a command

Usage:
  confik [options] -- <command> [args...]
  confik [options] <command> [args...]
  confik --clean

Options:
  --dry-run         Show what would be copied/ignored without running command
  --clean           Remove leftover staged files and confik gitignore blocks
  --no-gitignore    Skip updating .git/info/exclude during the run
  --no-registry     Ignore the built-in registry skip list
  -h, --help        Show this help

Config:
  .config/%s
  {
    "exclude": ["**/*.local", "private/**"],
    "registry": true,
    "registryOverride": ["vite.config.ts"],
    "gitignore": true
  }
`, configFilename)

	fmt.Fprint(os.Stdout, msg)
}

func loadConfig(configDir string) ConfikConfig {
	configPath := filepath.Join(configDir, configFilename)
	config := ConfikConfig{
		Exclude:          []string{},
		Registry:         true,
		RegistryOverride: []string{},
		Gitignore:        true,
		Path:             configPath,
	}

	if !exists(configPath) {
		return config
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "confik: failed to read %s; using defaults (%v)\n", configPath, err)
		return config
	}

	var parsed ConfigFile
	if err := json.Unmarshal(data, &parsed); err != nil {
		fmt.Fprintf(os.Stderr, "confik: failed to parse %s; using defaults (%v)\n", configPath, err)
		return config
	}

	if parsed.Exclude != nil {
		config.Exclude = parsed.Exclude
	}
	if parsed.RegistryOverride != nil {
		config.RegistryOverride = parsed.RegistryOverride
	}
	if parsed.Registry != nil {
		config.Registry = *parsed.Registry
	}
	if parsed.Gitignore != nil {
		config.Gitignore = *parsed.Gitignore
	}

	return config
}

func loadRegistryPatterns() []string {
	if len(embeddedRegistry) == 0 {
		return []string{}
	}
	var payload RegistryPayload
	if err := json.Unmarshal(embeddedRegistry, &payload); err != nil {
		return []string{}
	}
	return payload.Patterns
}

func matchesPatternList(target string, patterns []string, matchBase bool) bool {
	if len(patterns) == 0 {
		return false
	}
	target = filepath.ToSlash(target)
	base := path.Base(target)
	for _, pattern := range patterns {
		if pattern == "" {
			continue
		}
		normalized := filepath.ToSlash(pattern)
		if matchBase && !strings.Contains(normalized, "/") {
			if ok, _ := doublestar.Match(normalized, base); ok {
				return true
			}
		}
		if ok, _ := doublestar.Match(normalized, target); ok {
			return true
		}
	}
	return false
}

func isDirectory(dirPath string) bool {
	stat, err := os.Stat(dirPath)
	if err != nil {
		return false
	}
	return stat.IsDir()
}

func exists(pathname string) bool {
	_, err := os.Stat(pathname)
	return err == nil
}

func ensureDir(dirPath string, createdDirs *[]string, dryRun bool) (bool, error) {
	resolved, err := filepath.Abs(dirPath)
	if err != nil {
		return false, err
	}
	parts := strings.Split(resolved, string(filepath.Separator))
	if filepath.IsAbs(resolved) {
		parts[0] = string(filepath.Separator)
	}

	current := ""
	for _, part := range parts {
		if part == "" {
			continue
		}
		if current == "" || current == string(filepath.Separator) {
			current = filepath.Join(current, part)
		} else {
			current = filepath.Join(current, part)
		}
		info, err := os.Stat(current)
		if err == nil {
			if !info.IsDir() {
				return false, nil
			}
			continue
		}
		if !errors.Is(err, os.ErrNotExist) {
			return false, err
		}
		if dryRun {
			continue
		}
		if err := os.Mkdir(current, 0o755); err != nil {
			return false, err
		}
		*createdDirs = append(*createdDirs, current)
	}

	return true, nil
}

func copyFile(src, dest string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer func() {
		_ = out.Close()
	}()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	if err := out.Sync(); err != nil {
		return err
	}
	if info, err := in.Stat(); err == nil {
		_ = os.Chmod(dest, info.Mode())
	}

	return nil
}

func createRunID() string {
	now := time.Now().UTC().Format("20060102T150405")
	buf := make([]byte, 4)
	if _, err := rand.Read(buf); err == nil {
		return fmt.Sprintf("%s-%x", now, buf)
	}
	return fmt.Sprintf("%s-%d", now, time.Now().UnixNano())
}

func findGitRoot(start string) string {
	current, err := filepath.Abs(start)
	if err != nil {
		return ""
	}
	for {
		gitPath := filepath.Join(current, ".git")
		if exists(gitPath) {
			return current
		}
		parent := filepath.Dir(current)
		if parent == current {
			return ""
		}
		current = parent
	}
}

func resolveGitDir(gitRoot string) (string, error) {
	gitPath := filepath.Join(gitRoot, ".git")
	info, err := os.Stat(gitPath)
	if err != nil {
		return "", err
	}
	if info.IsDir() {
		return gitPath, nil
	}
	data, err := os.ReadFile(gitPath)
	if err != nil {
		return "", err
	}
	content := strings.TrimSpace(string(data))
	if !strings.HasPrefix(content, "gitdir:") {
		return "", errors.New("unable to resolve git directory")
	}
	gitDir := strings.TrimSpace(strings.TrimPrefix(content, "gitdir:"))
	return filepath.Abs(filepath.Join(gitRoot, gitDir))
}

func appendGitIgnoreBlock(gitDir, runID string, paths []string) (string, error) {
	infoDir := filepath.Join(gitDir, "info")
	excludePath := filepath.Join(infoDir, "exclude")

	if err := os.MkdirAll(infoDir, 0o755); err != nil {
		return "", err
	}

	blockStart := fmt.Sprintf("# confik:start:%s", runID)
	blockEnd := fmt.Sprintf("# confik:end:%s", runID)

	existing := ""
	if data, err := os.ReadFile(excludePath); err == nil {
		existing = string(data)
		if strings.Contains(existing, blockStart) {
			return excludePath, nil
		}
	}

	lines := append([]string{blockStart}, paths...)
	lines = append(lines, blockEnd)
	block := strings.Join(lines, "\n") + "\n"

	if existing != "" && !strings.HasSuffix(existing, "\n") {
		existing += "\n"
	}

	return excludePath, os.WriteFile(excludePath, []byte(existing+block), 0o644)
}

func removeGitIgnoreBlock(excludePath, runID string) error {
	data, err := os.ReadFile(excludePath)
	if err != nil {
		return nil
	}

	updated := removeGitIgnoreBlocks(string(data), runID)
	if updated == string(data) {
		return nil
	}

	return os.WriteFile(excludePath, []byte(updated), 0o644)
}

func removeAllGitIgnoreBlocks(excludePath string) error {
	data, err := os.ReadFile(excludePath)
	if err != nil {
		return nil
	}

	updated := removeGitIgnoreBlocks(string(data), "")
	if updated == string(data) {
		return nil
	}

	return os.WriteFile(excludePath, []byte(updated), 0o644)
}

func removeGitIgnoreBlocks(content, runID string) string {
	lines := strings.Split(content, "\n")
	out := []string{}
	skipping := false
	targetStart := ""
	targetEnd := ""
	if runID != "" {
		targetStart = fmt.Sprintf("# confik:start:%s", runID)
		targetEnd = fmt.Sprintf("# confik:end:%s", runID)
	}

	for _, line := range lines {
		if strings.HasPrefix(line, "# confik:start:") {
			if runID == "" || line == targetStart {
				skipping = true
				continue
			}
		}
		if skipping {
			if strings.HasPrefix(line, "# confik:end:") {
				if runID == "" || line == targetEnd {
					skipping = false
					continue
				}
			}
			continue
		}
		out = append(out, line)
	}

	result := strings.Join(out, "\n")
	if strings.HasSuffix(content, "\n") && !strings.HasSuffix(result, "\n") {
		result += "\n"
	}
	return result
}

func writeManifest(pathname string, manifest Manifest) error {
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(pathname, data, 0o644)
}

func readManifest(pathname string) (*Manifest, error) {
	data, err := os.ReadFile(pathname)
	if err != nil {
		return nil, err
	}
	var manifest Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, err
	}
	return &manifest, nil
}

func cleanLeftovers(cwd string, force bool, quiet bool) error {
	configDir := filepath.Join(cwd, ".config")
	manifestPath := filepath.Join(configDir, manifestFilename)
	cleaned := false

	if exists(manifestPath) {
		manifest, err := readManifest(manifestPath)
		if err != nil {
			manifest = nil
		}
		if manifest != nil {
			for _, rel := range manifest.CreatedFiles {
				_ = os.Remove(filepath.Join(cwd, rel))
			}
			dirs := make([]string, 0, len(manifest.CreatedDirs))
			for _, rel := range manifest.CreatedDirs {
				dirs = append(dirs, filepath.Join(cwd, rel))
			}
			sort.Slice(dirs, func(i, j int) bool { return len(dirs[i]) > len(dirs[j]) })
			for _, dir := range dirs {
				removeDirIfEmpty(dir)
			}
			if manifest.Gitignore != nil && manifest.Gitignore.ExcludePath != "" && manifest.Gitignore.RunID != "" {
				_ = removeGitIgnoreBlock(manifest.Gitignore.ExcludePath, manifest.Gitignore.RunID)
			}
			_ = os.Remove(manifestPath)
			cleaned = true
		}
	}

	if !cleaned && force {
		gitRoot := findGitRoot(cwd)
		if gitRoot != "" {
			if gitDir, err := resolveGitDir(gitRoot); err == nil {
				excludePath := filepath.Join(gitDir, "info", "exclude")
				_ = removeAllGitIgnoreBlocks(excludePath)
			}
		}
	}

	if !quiet {
		fmt.Fprintln(os.Stdout, "confik: cleanup complete")
	}
	return nil
}

func printSummary(dryRun bool, createdFiles, skippedExisting, skippedExcluded, skippedRegistry []string) {
	lines := []string{}
	if len(createdFiles) > 0 {
		verb := "staged"
		if dryRun {
			verb = "would stage"
		}
		lines = append(lines, fmt.Sprintf("confik: %s %d file(s)", verb, len(createdFiles)))
	}
	if len(skippedExisting) > 0 {
		lines = append(lines, fmt.Sprintf("confik: skipped %d existing file(s)", len(skippedExisting)))
	}
	if len(skippedExcluded) > 0 {
		lines = append(lines, fmt.Sprintf("confik: excluded %d file(s)", len(skippedExcluded)))
	}
	if len(skippedRegistry) > 0 {
		lines = append(lines, fmt.Sprintf("confik: registry-skipped %d file(s)", len(skippedRegistry)))
	}
	if len(lines) > 0 {
		fmt.Fprintln(os.Stdout, strings.Join(lines, "\n"))
	}
}

func runCommand(command string, args []string) (int, error) {
	cmd := exec.Command(command, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	err := cmd.Run()
	if err == nil {
		return 0, nil
	}

	if exitErr, ok := err.(*exec.ExitError); ok {
		if status, ok := exitErr.Sys().(syscall.WaitStatus); ok {
			return status.ExitStatus(), nil
		}
		return 1, nil
	}

	return 1, fmt.Errorf("failed to run %s (%v)", command, err)
}

func runCommandAndExit(command string, args []string, cleanup func()) error {
	code, err := runCommand(command, args)
	cleanup()
	if err != nil {
		return err
	}
	if code != 0 {
		os.Exit(code)
	}
	return nil
}

func removeDirIfEmpty(dirPath string) {
	entries, err := os.ReadDir(dirPath)
	if err != nil {
		return
	}
	if len(entries) == 0 {
		_ = os.Remove(dirPath)
	}
}

func uniqueStrings(input []string) []string {
	seen := map[string]struct{}{}
	out := []string{}
	for _, value := range input {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func toRelativeList(base string, values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		rel, err := filepath.Rel(base, value)
		if err != nil {
			continue
		}
		out = append(out, filepath.ToSlash(rel))
	}
	return out
}
