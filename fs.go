package main

import (
	"errors"
	"io"
	"os"
	"path/filepath"
)

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

func ensureDirWithCache(dirPath string, createdDirs *[]string, dryRun bool, dirCache map[string]bool) (bool, error) {
	resolved, err := filepath.Abs(dirPath)
	if err != nil {
		return false, err
	}
	if dirCache != nil && dirCache[resolved] {
		return true, nil
	}
	info, err := os.Stat(resolved)
	if err == nil {
		if !info.IsDir() {
			return false, nil
		}
		if dirCache != nil {
			dirCache[resolved] = true
		}
		return true, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	if dryRun {
		return true, nil
	}

	missing := []string{}
	current := resolved
	for dirCache == nil || !dirCache[current] {
		parentInfo, parentErr := os.Stat(current)
		if parentErr == nil {
			if !parentInfo.IsDir() {
				return false, nil
			}
			if dirCache != nil {
				dirCache[current] = true
			}
			break
		}
		if !errors.Is(parentErr, os.ErrNotExist) {
			return false, parentErr
		}
		missing = append(missing, current)
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
		current = parent
	}

	if err := os.MkdirAll(resolved, 0o750); err != nil {
		return false, err
	}
	for i := len(missing) - 1; i >= 0; i-- {
		if dirCache != nil {
			dirCache[missing[i]] = true
		}
		*createdDirs = append(*createdDirs, missing[i])
	}
	if dirCache != nil {
		dirCache[resolved] = true
	}

	return true, nil
}

func copyFile(src, dest string) error {
	// #nosec G304 -- src originates from WalkDir over the local .config tree.
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() {
		_ = in.Close()
	}()

	// #nosec G304 -- dest is derived from cwd + relative .config path.
	out, err := os.Create(dest)
	if err != nil {
		return err
	}

	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	if info, err := in.Stat(); err == nil {
		_ = os.Chmod(dest, info.Mode())
	}

	return nil
}

func removeDirIfEmpty(dirPath string) {
	_, _ = removeDirIfEmptyChecked(dirPath)
}

func removeDirIfEmptyChecked(dirPath string) (bool, error) {
	entries, err := os.ReadDir(dirPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return true, nil
		}
		return false, err
	}
	if len(entries) != 0 {
		return false, nil
	}
	if err := os.Remove(dirPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return true, nil
		}
		return false, err
	}
	return true, nil
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
