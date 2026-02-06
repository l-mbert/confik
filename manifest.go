package main

import (
	"encoding/json"
	"os"
)

type Manifest struct {
	RunID        string         `json:"runId"`
	CreatedFiles []string       `json:"createdFiles"`
	CreatedDirs  []string       `json:"createdDirs"`
	Gitignore    *GitContext    `json:"gitignore"`
	VSCode       *VSCodeContext `json:"vscode,omitempty"`
	CreatedAt    string         `json:"createdAt"`
}

func writeManifest(pathname string, manifest Manifest) error {
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(pathname, data, 0o600)
}

func readManifest(pathname string) (*Manifest, error) {
	// #nosec G304 -- pathname is always generated from cwd/.config/manifestFilename.
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
