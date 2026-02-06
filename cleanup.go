package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func combineErrors(primary, secondary error) error {
	if primary == nil {
		return secondary
	}
	if secondary == nil {
		return primary
	}
	return fmt.Errorf("%v; %v", primary, secondary)
}

func cleanupStagedArtifacts(manifestPath string, createdFiles, createdDirs []string, vscodeContext *VSCodeContext, gitContext *GitContext, unlock func() error, removeManifest bool) error {
	failures := []string{}
	remaining := map[string]struct{}{}

	recordFailure := func(format string, args ...any) {
		failures = append(failures, fmt.Sprintf(format, args...))
	}
	recordRemaining := func(path string) {
		if path == "" {
			return
		}
		remaining[path] = struct{}{}
	}

	if vscodeContext != nil {
		if err := removeVSCodeExcludes(vscodeContext); err != nil {
			recordFailure("remove VS Code excludes: %v", err)
		}
	}

	settingsCreatedPath := ""
	if vscodeContext != nil && vscodeContext.SettingsCreated {
		settingsCreatedPath = vscodeContext.SettingsPath
	}

	for _, filePath := range createdFiles {
		if settingsCreatedPath != "" && filePath == settingsCreatedPath {
			continue
		}
		if err := os.Remove(filePath); err != nil && !errors.Is(err, os.ErrNotExist) {
			recordFailure("remove file %s: %v", filePath, err)
		}
		if exists(filePath) {
			recordRemaining(filePath)
		}
	}

	uniqueDirs := uniqueStrings(createdDirs)
	sort.Slice(uniqueDirs, func(i, j int) bool { return len(uniqueDirs[i]) > len(uniqueDirs[j]) })
	for _, dirPath := range uniqueDirs {
		removed, err := removeDirIfEmptyChecked(dirPath)
		if err != nil {
			recordFailure("remove dir %s: %v", dirPath, err)
			continue
		}
		if !removed && exists(dirPath) {
			recordRemaining(dirPath)
		}
	}

	if gitContext != nil {
		if err := removeGitIgnoreBlock(gitContext.ExcludePath, gitContext.RunID); err != nil {
			recordFailure("remove gitignore block %s: %v", gitContext.ExcludePath, err)
		}
	}

	shouldRemoveManifest := removeManifest && len(failures) == 0 && len(remaining) == 0
	if shouldRemoveManifest {
		if err := os.Remove(manifestPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			recordFailure("remove manifest %s: %v", manifestPath, err)
		}
		if exists(manifestPath) {
			recordRemaining(manifestPath)
		}
	}

	if unlock != nil {
		if err := unlock(); err != nil {
			recordFailure("release lock: %v", err)
		}
	}

	if len(failures) == 0 && len(remaining) == 0 {
		return nil
	}

	remainingPaths := make([]string, 0, len(remaining))
	for path := range remaining {
		remainingPaths = append(remainingPaths, path)
	}
	sort.Strings(remainingPaths)

	parts := []string{}
	if len(failures) > 0 {
		parts = append(parts, fmt.Sprintf("%d cleanup error(s): %s", len(failures), strings.Join(failures, "; ")))
	}
	if len(remainingPaths) > 0 {
		parts = append(parts, fmt.Sprintf("%d path(s) remain: %s", len(remainingPaths), strings.Join(remainingPaths, ", ")))
	}
	return errors.New(strings.Join(parts, "; "))
}

func cleanLeftovers(cwd string, force bool, quiet bool) error {
	configDir := filepath.Join(cwd, ".config")
	manifestPath := filepath.Join(configDir, manifestFilename)
	cleaned := false
	var cleanupErr error

	if exists(manifestPath) {
		manifest, err := readManifest(manifestPath)
		if err != nil {
			cleanupErr = fmt.Errorf("failed to read manifest %s (%v)", manifestPath, err)
		}
		if manifest != nil {
			createdFiles := make([]string, 0, len(manifest.CreatedFiles))
			for _, rel := range manifest.CreatedFiles {
				createdFiles = append(createdFiles, filepath.Join(cwd, rel))
			}
			createdDirs := make([]string, 0, len(manifest.CreatedDirs))
			for _, rel := range manifest.CreatedDirs {
				createdDirs = append(createdDirs, filepath.Join(cwd, rel))
			}
			cleanupErr = cleanupStagedArtifacts(manifestPath, createdFiles, createdDirs, manifest.VSCode, manifest.Gitignore, nil, true)
			cleaned = true
		}
	}

	if !cleaned && force {
		gitRoot := findGitRoot(cwd)
		if gitRoot != "" {
			if gitDir, err := resolveGitDir(gitRoot); err == nil {
				excludePath := filepath.Join(gitDir, "info", "exclude")
				cleanupErr = combineErrors(cleanupErr, removeAllGitIgnoreBlocks(excludePath))
			}
		}
	}

	if !quiet {
		if cleanupErr != nil {
			_, _ = fmt.Fprintf(os.Stdout, "confik: cleanup incomplete (%v)\n", cleanupErr)
		} else {
			_, _ = fmt.Fprintln(os.Stdout, "confik: cleanup complete")
		}
	}
	return cleanupErr
}
