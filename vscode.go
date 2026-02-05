package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/tailscale/hujson"
)

type VSCodeContext struct {
	SettingsPath        string   `json:"settingsPath"`
	AddedKeys           []string `json:"addedKeys"`
	FilesExcludeCreated bool     `json:"filesExcludeCreated"`
	SettingsCreated     bool     `json:"settingsCreated"`
}

func applyVSCodeExcludes(cwd string, stagedFiles []string, createdFiles *[]string, createdDirs *[]string) (*VSCodeContext, error) {
	paths := uniqueRelativePaths(cwd, stagedFiles)
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
	value, root, err := parseVSCodeSettings(content, settingsPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "confik: unable to parse %s (must be valid JSON/JSONC)\n", settingsPath)
		return nil, nil
	}

	minified := !bytes.Contains(content, []byte("\n"))
	indentUnit := detectIndentUnit(root)
	rootIndent := objectMemberIndent(root)
	if rootIndent == "" {
		rootIndent = indentUnit
	}
	_, excludeObj, created := ensureObjectMember(root, "files.exclude", minified, rootIndent)
	if excludeObj == nil {
		fmt.Fprintf(os.Stderr, "confik: %s files.exclude is not an object; skipping VS Code excludes\n", settingsPath)
		return nil, nil
	}
	if created && !minified {
		excludeObj.AfterExtra = hujson.Extra("\n" + rootIndent)
	}

	excludeIndent := objectMemberIndent(excludeObj)
	if excludeIndent == "" {
		excludeIndent = rootIndent + indentUnit
	}
	addedKeys := []string{}
	for _, key := range paths {
		if objectHasKey(excludeObj, key) {
			continue
		}
		addObjectMember(excludeObj, key, boolValue(true), minified, excludeIndent)
		addedKeys = append(addedKeys, key)
	}

	if len(addedKeys) == 0 && !created {
		return nil, nil
	}

	packed := value.Pack()
	if err := os.WriteFile(settingsPath, packed, 0o644); err != nil {
		return nil, err
	}

	return &VSCodeContext{
		SettingsPath:        settingsPath,
		AddedKeys:           addedKeys,
		FilesExcludeCreated: created,
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
	value, root, err := parseVSCodeSettings(content, ctx.SettingsPath)
	if err != nil {
		return nil
	}

	memberIndex, excludeObj := findObjectMember(root, "files.exclude")
	if excludeObj == nil || memberIndex < 0 {
		return nil
	}

	changed := false
	for _, key := range ctx.AddedKeys {
		if removeObjectMember(excludeObj, key) {
			changed = true
		}
	}

	if ctx.FilesExcludeCreated && len(excludeObj.Members) == 0 {
		root.Members = append(root.Members[:memberIndex], root.Members[memberIndex+1:]...)
		changed = true
	}

	if !changed {
		if ctx.SettingsCreated && len(root.Members) == 0 && !containsJSONCComment(content) {
			return removeVSCodeSettingsIfEmpty(map[string]any{}, ctx.SettingsPath)
		}
		return nil
	}

	packed := value.Pack()
	if err := os.WriteFile(ctx.SettingsPath, packed, 0o644); err != nil {
		return err
	}

	if ctx.SettingsCreated && len(root.Members) == 0 && !containsJSONCComment(content) {
		return removeVSCodeSettingsIfEmpty(map[string]any{}, ctx.SettingsPath)
	}
	return nil
}

func findObjectMember(root *hujson.Object, name string) (int, *hujson.Object) {
	for i := range root.Members {
		member := &root.Members[i]
		memberName, ok := literalString(member.Name)
		if !ok || memberName != name {
			continue
		}
		obj, ok := member.Value.Value.(*hujson.Object)
		if !ok {
			return i, nil
		}
		return i, obj
	}
	return -1, nil
}

func parseVSCodeSettings(content []byte, settingsPath string) (hujson.Value, *hujson.Object, error) {
	value, err := hujson.Parse(content)
	if err != nil {
		return hujson.Value{}, nil, err
	}
	root, ok := value.Value.(*hujson.Object)
	if !ok {
		return hujson.Value{}, nil, fmt.Errorf("%s root must be an object", settingsPath)
	}
	return value, root, nil
}

func ensureObjectMember(root *hujson.Object, name string, minified bool, nameIndent string) (int, *hujson.Object, bool) {
	for i := range root.Members {
		member := &root.Members[i]
		memberName, ok := literalString(member.Name)
		if !ok || memberName != name {
			continue
		}
		obj, ok := member.Value.Value.(*hujson.Object)
		if !ok {
			return i, nil, false
		}
		return i, obj, false
	}

	var beforeExtra hujson.Extra
	var valueBefore hujson.Extra
	if !minified {
		if nameIndent == "" {
			nameIndent = detectIndentUnit(root)
		}
		beforeExtra = hujson.Extra("\n" + nameIndent)
		valueBefore = hujson.Extra(" ")
	}
	obj := &hujson.Object{}
	member := hujson.ObjectMember{
		Name:  hujson.Value{Value: hujson.String(name), BeforeExtra: beforeExtra},
		Value: hujson.Value{Value: obj, BeforeExtra: valueBefore},
	}
	root.Members = append(root.Members, member)
	return len(root.Members) - 1, obj, true
}

func objectHasKey(obj *hujson.Object, key string) bool {
	for i := range obj.Members {
		memberName, ok := literalString(obj.Members[i].Name)
		if ok && memberName == key {
			return true
		}
	}
	return false
}

func addObjectMember(obj *hujson.Object, key string, value hujson.Value, minified bool, nameIndent string) {
	var beforeExtra hujson.Extra
	var valueBefore hujson.Extra
	if !minified {
		if nameIndent == "" {
			nameIndent = detectIndentUnit(obj)
		}
		beforeExtra = hujson.Extra("\n" + nameIndent)
		valueBefore = hujson.Extra(" ")
	}
	member := hujson.ObjectMember{
		Name:  hujson.Value{Value: hujson.String(key), BeforeExtra: beforeExtra},
		Value: hujson.Value{Value: value.Value, BeforeExtra: valueBefore},
	}
	obj.Members = append(obj.Members, member)
}

func removeObjectMember(obj *hujson.Object, key string) bool {
	for i := range obj.Members {
		memberName, ok := literalString(obj.Members[i].Name)
		if ok && memberName == key {
			obj.Members = append(obj.Members[:i], obj.Members[i+1:]...)
			return true
		}
	}
	return false
}

func literalString(value hujson.Value) (string, bool) {
	literal, ok := value.Value.(hujson.Literal)
	if !ok {
		return "", false
	}
	var s string
	if err := json.Unmarshal([]byte(literal), &s); err != nil {
		return "", false
	}
	return s, true
}

func boolValue(value bool) hujson.Value {
	return hujson.Value{Value: hujson.Bool(value)}
}

func objectMemberIndent(obj *hujson.Object) string {
	for i := range obj.Members {
		indent := indentFromExtra(obj.Members[i].Name.BeforeExtra)
		if indent != "" {
			return indent
		}
	}
	return ""
}

func detectIndentUnit(obj *hujson.Object) string {
	unit := ""
	for i := range obj.Members {
		indent := indentFromExtra(obj.Members[i].Name.BeforeExtra)
		if indent == "" {
			continue
		}
		if unit == "" || len(indent) < len(unit) {
			unit = indent
		}
	}
	if unit == "" {
		return "  "
	}
	return unit
}

func indentFromExtra(extra hujson.Extra) string {
	text := string(extra)
	idx := strings.LastIndex(text, "\n")
	if idx == -1 {
		return ""
	}
	rest := text[idx+1:]
	if rest == "" {
		return ""
	}
	end := 0
	for end < len(rest) {
		if rest[end] != ' ' && rest[end] != '\t' {
			break
		}
		end++
	}
	if end == 0 {
		return ""
	}
	return rest[:end]
}

func uniqueRelativePaths(base string, paths []string) []string {
	seen := map[string]bool{}
	result := []string{}
	for _, filePath := range paths {
		rel, err := filepath.Rel(base, filePath)
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
			result = append(result, relPosix)
		}
	}
	return result
}

func containsJSONCComment(content []byte) bool {
	inString := false
	escape := false
	for i := range content {
		ch := content[i]
		if inString {
			if escape {
				escape = false
				continue
			}
			if ch == '\\' {
				escape = true
				continue
			}
			if ch == '"' {
				inString = false
			}
			continue
		}
		if ch == '"' {
			inString = true
			continue
		}
		if ch == '/' && i+1 < len(content) {
			if content[i+1] == '/' || content[i+1] == '*' {
				return true
			}
		}
	}
	return false
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
