package main

import (
	"crypto/rand"
	_ "embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"os/signal"
	"path"
	"path/filepath"
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
	VSCodeExclude    *bool    `json:"vscodeExclude"`
}

type ConfikConfig struct {
	Exclude          []string
	Registry         bool
	RegistryOverride []string
	Gitignore        bool
	VSCodeExclude    bool
	Path             string
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
			return nil
		}
		return runCommandAndExit(parsed.Command, parsed.CommandArgs, func() error { return nil })
	}

	lockPath := filepath.Join(configDir, lockFilename)
	lock, err := acquireLock(lockPath)
	if err != nil {
		return err
	}
	lockHeld := true
	unlock := func() error {
		if lockHeld {
			err := lock.Unlock()
			lockHeld = false
			return err
		}
		return nil
	}

	if parsed.Flags.Clean {
		defer func() { _ = unlock() }()
		return cleanLeftovers(cwd, true, false)
	}

	if exists(filepath.Join(configDir, manifestFilename)) {
		if err := cleanLeftovers(cwd, false, true); err != nil {
			fmt.Fprintf(os.Stderr, "confik: pre-run cleanup incomplete (%v)\n", err)
		}
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

	var vscodeContext *VSCodeContext
	var gitContext *GitContext
	runID := createRunID()
	manifestPath := filepath.Join(configDir, manifestFilename)
	cleanupStaging := func() error {
		return cleanupStagedArtifacts(manifestPath, createdFiles, createdDirs, vscodeContext, gitContext, unlock, true)
	}

	dirCache := map[string]bool{}
	walkErr := filepath.WalkDir(configDir, func(pathname string, d fs.DirEntry, entryErr error) error {
		if entryErr != nil {
			return nil
		}
		if d.IsDir() {
			return nil
		}

		rel, err := filepath.Rel(configDir, pathname)
		if err != nil {
			return nil
		}

		relPosix := filepath.ToSlash(rel)
		if relPosix == configFilename || relPosix == manifestFilename || relPosix == lockFilename {
			return nil
		}

		if matchesPatternList(relPosix, config.Exclude, true) {
			skippedExcluded = append(skippedExcluded, relPosix)
			return nil
		}

		if useRegistry && matchesPatternList(relPosix, registryPatterns, true) {
			if !matchesPatternList(relPosix, config.RegistryOverride, true) {
				skippedRegistry = append(skippedRegistry, relPosix)
				return nil
			}
		}

		dest := filepath.Join(cwd, rel)
		if exists(dest) {
			skippedExisting = append(skippedExisting, relPosix)
			return nil
		}

		ok, err := ensureDirWithCache(filepath.Dir(dest), &createdDirs, parsed.Flags.DryRun, dirCache)
		if err != nil {
			return err
		}
		if !ok {
			skippedExisting = append(skippedExisting, relPosix)
			return nil
		}

		if !parsed.Flags.DryRun {
			if err := copyFile(pathname, dest); err != nil {
				return err
			}
		}
		createdFiles = append(createdFiles, dest)
		return nil
	})
	if walkErr != nil {
		return combineErrors(walkErr, cleanupStaging())
	}

	stagedFiles := append([]string(nil), createdFiles...)
	if !parsed.Flags.DryRun && config.VSCodeExclude && len(stagedFiles) > 0 {
		ctx, err := applyVSCodeExcludes(cwd, stagedFiles, &createdFiles, &createdDirs)
		if err != nil {
			fmt.Fprintf(os.Stderr, "confik: failed to update .vscode/settings.json (%v)\n", err)
		} else {
			vscodeContext = ctx
		}
	}

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
					posixRel := filepath.ToSlash(rel)
					if !strings.HasPrefix(posixRel, "/") {
						posixRel = "/" + posixRel
					}
					relPaths = append(relPaths, posixRel)
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
		VSCode:       vscodeContext,
		CreatedAt:    time.Now().UTC().Format(time.RFC3339),
	}

	if !parsed.Flags.DryRun && len(createdFiles) > 0 {
		if err := writeManifest(manifestPath, manifest); err != nil {
			return combineErrors(err, cleanupStaging())
		}
	}

	printSummary(parsed.Flags.DryRun, createdFiles, skippedExisting, skippedExcluded, skippedRegistry)

	if parsed.Flags.DryRun {
		if err := unlock(); err != nil {
			return err
		}
		if len(createdFiles) > 0 {
			_, _ = fmt.Fprintln(os.Stdout, "confik: dry-run complete. No files were written.")
		} else {
			_, _ = fmt.Fprintln(os.Stdout, "confik: dry-run complete. No files to stage.")
		}
		return nil
	}

	cleanupOnce := sync.Once{}
	var cleanupErr error
	cleanup := func() error {
		cleanupOnce.Do(func() {
			cleanupErr = cleanupStaging()
		})
		return cleanupErr
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	defer signal.Stop(sigCh)

	if parsed.Command == "" {
		_, _ = fmt.Fprintln(os.Stdout, "confik: standalone mode active. Press Ctrl+C to clean up and exit.")
		sig := <-sigCh
		// Restore default signal handling before cleanup so a second Ctrl+C force-exits.
		signal.Stop(sigCh)
		fmt.Fprintf(os.Stderr, "confik: received %s, cleaning up...\n", sig.String())
		if err := cleanup(); err != nil {
			fmt.Fprintf(os.Stderr, "confik: cleanup incomplete (%v)\n", err)
			return combineErrors(fmt.Errorf("interrupted"), err)
		}
		return fmt.Errorf("interrupted")
	}

	go func() {
		sig := <-sigCh
		// Restore default signal handling before cleanup so a second Ctrl+C force-exits.
		signal.Stop(sigCh)
		fmt.Fprintf(os.Stderr, "confik: received %s, cleaning up...\n", sig.String())
		if err := cleanup(); err != nil {
			fmt.Fprintf(os.Stderr, "confik: cleanup incomplete (%v)\n", err)
		}
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
	msg := `confik - stage .config files in project root while running a command

Usage:
  confik [options]
  confik [options] -- <command> [args...]
  confik [options] <command> [args...]
  confik --clean

Options:
  --dry-run         Show what would be copied/ignored without writing files
  --clean           Remove leftover staged files and confik gitignore blocks
  --no-gitignore    Skip updating .git/info/exclude during the run
  --no-registry     Ignore the built-in registry skip list
  -h, --help        Show this help
`

	_, _ = fmt.Fprint(os.Stdout, msg)
}

func loadConfig(configDir string) ConfikConfig {
	configPath := filepath.Join(configDir, configFilename)
	config := ConfikConfig{
		Exclude:          []string{},
		Registry:         true,
		RegistryOverride: []string{},
		Gitignore:        true,
		VSCodeExclude:    false,
		Path:             configPath,
	}

	if !exists(configPath) {
		return config
	}

	// #nosec G304 -- configPath is derived from cwd/.config and not user-supplied absolute input.
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
	if parsed.VSCodeExclude != nil {
		config.VSCodeExclude = *parsed.VSCodeExclude
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

func createRunID() string {
	now := time.Now().UTC().Format("20060102T150405")
	buf := make([]byte, 4)
	if _, err := rand.Read(buf); err == nil {
		return fmt.Sprintf("%s-%x", now, buf)
	}
	return fmt.Sprintf("%s-%d", now, time.Now().UnixNano())
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
		_, _ = fmt.Fprintln(os.Stdout, strings.Join(lines, "\n"))
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

func runCommandAndExit(command string, args []string, cleanup func() error) error {
	code, err := runCommand(command, args)
	cleanupErr := cleanup()
	if cleanupErr != nil {
		fmt.Fprintf(os.Stderr, "confik: cleanup incomplete (%v)\n", cleanupErr)
	}
	if err != nil {
		return combineErrors(err, cleanupErr)
	}
	if code != 0 {
		os.Exit(code)
	}
	if cleanupErr != nil {
		return cleanupErr
	}
	return nil
}
