package main

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"
)

func TestFSIsDirectoryAndExists(t *testing.T) {
	base := t.TempDir()
	dirPath := filepath.Join(base, "dir")
	filePath := filepath.Join(base, "file.txt")
	if err := os.MkdirAll(dirPath, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filePath, []byte("data"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	if !isDirectory(dirPath) {
		t.Fatalf("expected directory to be detected")
	}
	if isDirectory(filePath) {
		t.Fatalf("did not expect regular file to be treated as directory")
	}
	if !exists(filePath) {
		t.Fatalf("expected file to exist")
	}
	if exists(filepath.Join(base, "missing.txt")) {
		t.Fatalf("did not expect missing file to exist")
	}
}

func TestEnsureDirWithCache(t *testing.T) {
	base := t.TempDir()
	nested := filepath.Join(base, "a", "b", "c")
	created := []string{}
	cache := map[string]bool{}

	ok, err := ensureDirWithCache(nested, &created, false, cache)
	if err != nil {
		t.Fatalf("ensureDirWithCache error: %v", err)
	}
	if !ok {
		t.Fatalf("expected ok")
	}
	if _, err := os.Stat(nested); err != nil {
		t.Fatalf("expected directory created: %v", err)
	}
	if len(created) == 0 {
		t.Fatalf("expected created dirs recorded")
	}

	before := len(created)
	ok, err = ensureDirWithCache(nested, &created, false, cache)
	if err != nil || !ok {
		t.Fatalf("ensureDirWithCache second pass error: %v", err)
	}
	if len(created) != before {
		t.Fatalf("expected cached ensureDirWithCache call to avoid new created dirs")
	}

	dryNested := filepath.Join(base, "x", "y")
	createdDry := []string{}
	ok, err = ensureDirWithCache(dryNested, &createdDry, true, map[string]bool{})
	if err != nil || !ok {
		t.Fatalf("dry-run ensureDirWithCache error: %v", err)
	}
	if _, err := os.Stat(dryNested); err == nil {
		t.Fatalf("expected dry-run to not create dirs")
	}
}

func TestCopyFilePreservesContentAndMode(t *testing.T) {
	base := t.TempDir()
	src := filepath.Join(base, "source.txt")
	dest := filepath.Join(base, "dest.txt")
	content := []byte("hello world")
	if err := os.WriteFile(src, content, 0o750); err != nil {
		t.Fatalf("write source: %v", err)
	}

	if err := copyFile(src, dest); err != nil {
		t.Fatalf("copyFile error: %v", err)
	}

	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("read dest: %v", err)
	}
	if string(got) != string(content) {
		t.Fatalf("unexpected copied content: %q", string(got))
	}

	if runtime.GOOS != "windows" {
		srcInfo, err := os.Stat(src)
		if err != nil {
			t.Fatalf("stat source: %v", err)
		}
		destInfo, err := os.Stat(dest)
		if err != nil {
			t.Fatalf("stat dest: %v", err)
		}
		if srcInfo.Mode().Perm() != destInfo.Mode().Perm() {
			t.Fatalf("expected copied mode %v, got %v", srcInfo.Mode().Perm(), destInfo.Mode().Perm())
		}
	}
}

func TestRemoveDirIfEmptyChecked(t *testing.T) {
	base := t.TempDir()

	emptyDir := filepath.Join(base, "empty")
	if err := os.MkdirAll(emptyDir, 0o755); err != nil {
		t.Fatalf("mkdir empty: %v", err)
	}
	removed, err := removeDirIfEmptyChecked(emptyDir)
	if err != nil {
		t.Fatalf("removeDirIfEmptyChecked empty error: %v", err)
	}
	if !removed {
		t.Fatalf("expected empty directory to be removed")
	}
	if _, err := os.Stat(emptyDir); err == nil {
		t.Fatalf("expected empty directory removed")
	}

	nonEmpty := filepath.Join(base, "non-empty")
	if err := os.MkdirAll(nonEmpty, 0o755); err != nil {
		t.Fatalf("mkdir non-empty: %v", err)
	}
	if err := os.WriteFile(filepath.Join(nonEmpty, "keep.txt"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write keep file: %v", err)
	}
	removed, err = removeDirIfEmptyChecked(nonEmpty)
	if err != nil {
		t.Fatalf("removeDirIfEmptyChecked non-empty error: %v", err)
	}
	if removed {
		t.Fatalf("did not expect non-empty directory to be removed")
	}

	missing := filepath.Join(base, "missing")
	removed, err = removeDirIfEmptyChecked(missing)
	if err != nil {
		t.Fatalf("removeDirIfEmptyChecked missing error: %v", err)
	}
	if !removed {
		t.Fatalf("expected missing directory to be treated as removed")
	}
}

func TestUniqueStringsAndToRelativeList(t *testing.T) {
	unique := uniqueStrings([]string{"a", "b", "a", "c", "b"})
	if !slices.Equal(unique, []string{"a", "b", "c"}) {
		t.Fatalf("unexpected uniqueStrings result: %#v", unique)
	}

	base := t.TempDir()
	values := []string{
		filepath.Join(base, "a.txt"),
		filepath.Join(base, "nested", "b.txt"),
	}
	rel := toRelativeList(base, values)
	expected := []string{"a.txt", "nested/b.txt"}
	if !slices.Equal(rel, expected) {
		t.Fatalf("unexpected toRelativeList result: got %#v want %#v", rel, expected)
	}
}
