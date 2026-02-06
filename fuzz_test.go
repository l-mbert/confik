package main

import (
	"strings"
	"testing"
)

func FuzzMatchesPatternList(f *testing.F) {
	f.Add("foo.local", "**/*.local", true)
	f.Add("config/foo.local", "**/*.local", true)
	f.Add("private/secret.txt", "private/**", true)
	f.Add("configs/vite.config.ts", "vite.config.*", true)
	f.Add("config/other.ts", "**/*.local", false)
	f.Add("", "**/*", true)
	f.Add("a.txt", "", false)
	f.Add("deeply/nested/path/file.ext", "**/*.ext", true)

	f.Fuzz(func(t *testing.T, target, pattern string, matchBase bool) {
		if pattern == "" || target == "" {
			return
		}
		_ = matchesPatternList(target, []string{pattern}, matchBase) // Just ensure it doesn't panic
	})
}

func FuzzRemoveGitIgnoreBlocks(f *testing.F) {
	// Seed corpus
	f.Add("# confik:start:abc\nfoo\n# confik:end:abc\n", "abc")
	f.Add("line1\n# confik:start:x\ny\n# confik:end:x\nline2\n", "x")
	f.Add("no blocks here\n", "any")
	f.Add("# confik:start:a\n# confik:end:a\n# confik:start:b\n# confik:end:b\n", "")
	f.Add("", "")
	f.Add("# confik:start:unclosed\ndata\n", "unclosed")
	f.Add("mixed\n# confik:start:m\ninner\n# confik:end:m\ntrailing\n", "m")

	f.Fuzz(func(t *testing.T, content, runID string) {
		result := removeGitIgnoreBlocks(content, runID)

		if runID != "" {
			startMarker := "# confik:start:" + runID
			endMarker := "# confik:end:" + runID
			if strings.Contains(result, startMarker) || strings.Contains(result, endMarker) {
				t.Errorf("result still contains block markers for runID %q", runID)
			}
		}

		if runID == "" {
			if strings.Contains(result, "# confik:start:") {
				t.Errorf("result still contains confik:start blocks after remove-all")
			}
		}

		if strings.HasSuffix(content, "\n") && len(result) > 0 && !strings.HasSuffix(result, "\n") {
			t.Errorf("trailing newline not preserved")
		}
	})
}

func FuzzParseVSCodeSettings(f *testing.F) {
	f.Add([]byte(`{}`))
	f.Add([]byte(`{"files.exclude": {"a.txt": true}}`))
	f.Add([]byte("{\n  // comment\n  \"files.exclude\": {}\n}\n"))
	f.Add([]byte(`{"files.exclude": true}`))
	f.Add([]byte(`{"editor.tabSize": 2, "files.exclude": {"b.txt": true}}`))
	f.Add([]byte("// top comment\n{}"))
	f.Add([]byte(`{"files.exclude": {"a": true, "b": false}}`))

	f.Fuzz(func(t *testing.T, content []byte) {
		_, _, _ = parseVSCodeSettings(content, "test.json") // Just ensure it doesn't panic
	})
}

func FuzzContainsJSONCComment(f *testing.F) {
	f.Add([]byte(`{}`))
	f.Add([]byte(`// comment`))
	f.Add([]byte(`/* block */`))
	f.Add([]byte(`{"key": "// not a comment"}`))
	f.Add([]byte(`{"key": "/* also not */"}`))
	f.Add([]byte("{\n  // real comment\n}"))
	f.Add([]byte(`{}`))

	f.Fuzz(func(t *testing.T, content []byte) {
		_ = containsJSONCComment(content) // Just ensure it doesn't panic
	})
}
