package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGitIgnoreBlockHelpers(t *testing.T) {
	t.Run("append-and-remove-single-block", func(t *testing.T) {
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
		if !strings.Contains(string(content), "foo.txt") {
			t.Fatalf("expected foo.txt in block")
		}
		if !strings.Contains(string(content), "bar/baz.json") {
			t.Fatalf("expected bar/baz.json in block")
		}

		if err := removeGitIgnoreBlock(exclude, "abc"); err != nil {
			t.Fatalf("removeGitIgnoreBlock error: %v", err)
		}
		content, _ = os.ReadFile(exclude)
		if strings.Contains(string(content), "confik:start:abc") {
			t.Fatalf("expected block removed")
		}
	})

	t.Run("remove-all-blocks", func(t *testing.T) {
		dir := t.TempDir()
		gitDir := filepath.Join(dir, ".git")
		infoDir := filepath.Join(gitDir, "info")
		if err := os.MkdirAll(infoDir, 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		exclude := filepath.Join(infoDir, "exclude")

		_, _ = appendGitIgnoreBlock(gitDir, "one", []string{"a"})
		_, _ = appendGitIgnoreBlock(gitDir, "two", []string{"b"})
		if err := removeAllGitIgnoreBlocks(exclude); err != nil {
			t.Fatalf("removeAllGitIgnoreBlocks error: %v", err)
		}
		content, _ := os.ReadFile(exclude)
		if strings.Contains(string(content), "confik:start:") {
			t.Fatalf("expected all blocks removed")
		}
	})

	t.Run("append-idempotent-for-same-run-id", func(t *testing.T) {
		dir := t.TempDir()
		gitDir := filepath.Join(dir, ".git")
		infoDir := filepath.Join(gitDir, "info")
		if err := os.MkdirAll(infoDir, 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		exclude := filepath.Join(infoDir, "exclude")

		_, _ = appendGitIgnoreBlock(gitDir, "dup", []string{"x"})
		contentBefore, _ := os.ReadFile(exclude)

		_, _ = appendGitIgnoreBlock(gitDir, "dup", []string{"x"})
		contentAfter, _ := os.ReadFile(exclude)

		if string(contentBefore) != string(contentAfter) {
			t.Fatalf("expected idempotent append, but content changed")
		}
	})

	t.Run("append-to-existing-content", func(t *testing.T) {
		dir := t.TempDir()
		gitDir := filepath.Join(dir, ".git")
		infoDir := filepath.Join(gitDir, "info")
		if err := os.MkdirAll(infoDir, 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		exclude := filepath.Join(infoDir, "exclude")

		// Pre-populate with existing content
		existing := "# existing ignore rules\n*.log\n"
		if err := os.WriteFile(exclude, []byte(existing), 0o644); err != nil {
			t.Fatalf("write exclude: %v", err)
		}

		_, _ = appendGitIgnoreBlock(gitDir, "new", []string{"staged.txt"})
		content, _ := os.ReadFile(exclude)
		if !strings.Contains(string(content), "*.log") {
			t.Fatalf("expected existing content preserved")
		}
		if !strings.Contains(string(content), "confik:start:new") {
			t.Fatalf("expected new block appended")
		}
	})

	t.Run("remove-nonexistent-block-is-noop", func(t *testing.T) {
		dir := t.TempDir()
		gitDir := filepath.Join(dir, ".git")
		infoDir := filepath.Join(gitDir, "info")
		if err := os.MkdirAll(infoDir, 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		exclude := filepath.Join(infoDir, "exclude")
		if err := os.WriteFile(exclude, []byte("keep\n"), 0o644); err != nil {
			t.Fatalf("write exclude: %v", err)
		}

		if err := removeGitIgnoreBlock(exclude, "nonexistent"); err != nil {
			t.Fatalf("removeGitIgnoreBlock error: %v", err)
		}
		content, _ := os.ReadFile(exclude)
		if string(content) != "keep\n" {
			t.Fatalf("expected content unchanged, got %q", string(content))
		}
	})

	t.Run("remove-from-missing-file-is-noop", func(t *testing.T) {
		dir := t.TempDir()
		missing := filepath.Join(dir, "missing-exclude")
		if err := removeGitIgnoreBlock(missing, "any"); err != nil {
			t.Fatalf("removeGitIgnoreBlock should not error on missing file: %v", err)
		}
	})
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

func TestFindGitRoot(t *testing.T) {
	base := t.TempDir()
	gitRoot := filepath.Join(base, "repo")
	if err := os.MkdirAll(filepath.Join(gitRoot, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir git root: %v", err)
	}
	subDir := filepath.Join(gitRoot, "a", "b")
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		t.Fatalf("mkdir subdir: %v", err)
	}

	if got := findGitRoot(subDir); got != gitRoot {
		t.Fatalf("unexpected git root: got %q want %q", got, gitRoot)
	}

	plain := t.TempDir()
	if got := findGitRoot(plain); got != "" {
		t.Fatalf("expected no git root, got %q", got)
	}
}

func TestResolveGitDir(t *testing.T) {
	t.Run("directory-dot-git", func(t *testing.T) {
		root := t.TempDir()
		expected := filepath.Join(root, ".git")
		if err := os.MkdirAll(expected, 0o755); err != nil {
			t.Fatalf("mkdir .git: %v", err)
		}
		got, err := resolveGitDir(root)
		if err != nil {
			t.Fatalf("resolveGitDir error: %v", err)
		}
		if got != expected {
			t.Fatalf("unexpected git dir: got %q want %q", got, expected)
		}
	})

	t.Run("gitdir-pointer-file", func(t *testing.T) {
		root := t.TempDir()
		worktreeGit := filepath.Join(root, ".git", "modules", "worktree")
		if err := os.MkdirAll(worktreeGit, 0o755); err != nil {
			t.Fatalf("mkdir worktree git: %v", err)
		}
		gitFile := filepath.Join(root, ".git")
		if err := os.RemoveAll(gitFile); err != nil {
			t.Fatalf("remove .git dir: %v", err)
		}
		if err := os.WriteFile(gitFile, []byte("gitdir: .git/modules/worktree\n"), 0o644); err != nil {
			t.Fatalf("write .git file: %v", err)
		}

		got, err := resolveGitDir(root)
		if err != nil {
			t.Fatalf("resolveGitDir error: %v", err)
		}
		if got != worktreeGit {
			t.Fatalf("unexpected git dir: got %q want %q", got, worktreeGit)
		}
	})

	t.Run("invalid-git-file", func(t *testing.T) {
		root := t.TempDir()
		if err := os.WriteFile(filepath.Join(root, ".git"), []byte("not-a-gitdir"), 0o644); err != nil {
			t.Fatalf("write .git file: %v", err)
		}
		if _, err := resolveGitDir(root); err == nil {
			t.Fatalf("expected resolveGitDir to fail for invalid gitdir format")
		}
	})
}
