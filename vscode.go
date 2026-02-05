package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type VSCodeContext struct {
	SettingsPath        string   `json:"settingsPath"`
	AddedKeys           []string `json:"addedKeys"`
	FilesExcludeCreated bool     `json:"filesExcludeCreated"`
	SettingsCreated     bool     `json:"settingsCreated"`
}

func applyVSCodeExcludes(cwd string, stagedFiles []string, createdFiles *[]string, createdDirs *[]string) (*VSCodeContext, error) {
	paths := []string{}
	seen := map[string]bool{}
	for _, filePath := range stagedFiles {
		rel, err := filepath.Rel(cwd, filePath)
		if err != nil {
			continue
		}
		if strings.HasPrefix(rel, "..") {
			continue
		}
		relPosix := filepath.ToSlash(rel)
		if relPosix == "." || relPosix == "" {
			continue
		}
		if !seen[relPosix] {
			seen[relPosix] = true
			paths = append(paths, relPosix)
		}
	}
	if len(paths) == 0 {
		return nil, nil
	}

	settingsDir := filepath.Join(cwd, ".vscode")
	settingsPath := filepath.Join(settingsDir, "settings.json")
	if !exists(settingsPath) {
		ok, err := ensureDir(settingsDir, createdDirs, false)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, fmt.Errorf("failed to create %s", settingsDir)
		}
		settings := map[string]any{
			"files.exclude": map[string]any{},
		}
		excludeMap := settings["files.exclude"].(map[string]any)
		for _, key := range paths {
			excludeMap[key] = true
		}
		data, err := json.MarshalIndent(settings, "", "  ")
		if err != nil {
			return nil, err
		}
		data = append(data, '\n')
		if err := os.WriteFile(settingsPath, data, 0o644); err != nil {
			return nil, err
		}
		*createdFiles = append(*createdFiles, settingsPath)
		return &VSCodeContext{
			SettingsPath:        settingsPath,
			AddedKeys:           paths,
			FilesExcludeCreated: true,
			SettingsCreated:     true,
		}, nil
	}

	content, err := os.ReadFile(settingsPath)
	if err != nil {
		return nil, err
	}
	trimmed := strings.TrimSpace(string(content))
	settings := map[string]any{}
	if trimmed != "" {
		if err := json.Unmarshal(content, &settings); err != nil {
			fmt.Fprintf(os.Stderr, "confik: unable to parse %s (must be valid JSON)\n", settingsPath)
			return nil, nil
		}
	}

	indent := detectIndent(content)
	hadNewline := strings.HasSuffix(string(content), "\n")
	minified := !strings.Contains(string(content), "\n")

	filesExcludeCreated := false
	excludeMap := map[string]any{}
	if existing, ok := settings["files.exclude"]; ok {
		cast, ok := existing.(map[string]any)
		if !ok {
			fmt.Fprintf(os.Stderr, "confik: %s files.exclude is not an object; skipping VS Code excludes\n", settingsPath)
			return nil, nil
		}
		excludeMap = cast
	} else {
		filesExcludeCreated = true
		settings["files.exclude"] = excludeMap
	}

	addedKeys := []string{}
	for _, key := range paths {
		if _, ok := excludeMap[key]; ok {
			continue
		}
		excludeMap[key] = true
		addedKeys = append(addedKeys, key)
	}

	if len(addedKeys) == 0 && !filesExcludeCreated {
		return nil, nil
	}

	var data []byte
	if minified {
		data, err = json.Marshal(settings)
	} else {
		data, err = json.MarshalIndent(settings, "", indent)
	}
	if err != nil {
		return nil, err
	}
	if hadNewline {
		data = append(data, '\n')
	}
	if err := os.WriteFile(settingsPath, data, 0o644); err != nil {
		return nil, err
	}

	return &VSCodeContext{
		SettingsPath:        settingsPath,
		AddedKeys:           addedKeys,
		FilesExcludeCreated: filesExcludeCreated,
		SettingsCreated:     false,
	}, nil
}

func removeVSCodeExcludes(ctx *VSCodeContext) error {
	if ctx == nil || ctx.SettingsPath == "" {
		return nil
	}
	content, err := os.ReadFile(ctx.SettingsPath)
	if err != nil {
		return nil
	}
	trimmed := strings.TrimSpace(string(content))
	if trimmed == "" {
		return nil
	}

	settings := map[string]any{}
	if err := json.Unmarshal(content, &settings); err != nil {
		return nil
	}

	existing, ok := settings["files.exclude"]
	if !ok {
		return nil
	}
	excludeMap, ok := existing.(map[string]any)
	if !ok {
		return nil
	}

	changed := false
	for _, key := range ctx.AddedKeys {
		if _, ok := excludeMap[key]; ok {
			delete(excludeMap, key)
			changed = true
		}
	}

	if ctx.FilesExcludeCreated && len(excludeMap) == 0 {
		delete(settings, "files.exclude")
		changed = true
	}

	if !changed {
		if ctx.SettingsCreated {
			return removeVSCodeSettingsIfEmpty(settings, ctx.SettingsPath)
		}
		return nil
	}

	indent := detectIndent(content)
	hadNewline := strings.HasSuffix(string(content), "\n")
	minified := !strings.Contains(string(content), "\n")
	var data []byte
	if minified {
		data, err = json.Marshal(settings)
	} else {
		data, err = json.MarshalIndent(settings, "", indent)
	}
	if err != nil {
		return err
	}
	if hadNewline {
		data = append(data, '\n')
	}
	if err := os.WriteFile(ctx.SettingsPath, data, 0o644); err != nil {
		return err
	}
	if ctx.SettingsCreated {
		return removeVSCodeSettingsIfEmpty(settings, ctx.SettingsPath)
	}
	return nil
}

func detectIndent(data []byte) string {
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		trimmed := strings.TrimLeft(line, " \t")
		if trimmed == "" {
			continue
		}
		if !strings.HasPrefix(trimmed, "\"") {
			continue
		}
		leading := line[:len(line)-len(trimmed)]
		if strings.Contains(leading, "\t") {
			return "\t"
		}
		if len(leading) > 0 {
			return leading
		}
	}
	return "  "
}

func removeVSCodeSettingsIfEmpty(settings map[string]any, settingsPath string) error {
	if len(settings) != 0 {
		return nil
	}
	if err := os.Remove(settingsPath); err != nil {
		return nil
	}
	removeDirIfEmpty(filepath.Dir(settingsPath))
	return nil
}
