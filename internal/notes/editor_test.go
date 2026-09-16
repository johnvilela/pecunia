package notes

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEditor(t *testing.T) {
	cases := []struct {
		name, visual, editor, want string
	}{
		{"VISUAL wins", "code --wait", "nvim", "code --wait"},
		{"EDITOR next", "", "nvim", "nvim"},
		{"vi last", "", "", "vi"},
		{"blank counts as unset", "  ", " ", "vi"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("VISUAL", tc.visual)
			t.Setenv("EDITOR", tc.editor)
			if got := Editor(); got != tc.want {
				t.Fatalf("Editor() = %q; want %q", got, tc.want)
			}
		})
	}
}

func TestEditorArgs(t *testing.T) {
	cases := []struct {
		editor string
		line   int
		want   string
	}{
		{"vim", 12, "vim +12 /n/3.md"},
		{"nvim", 12, "nvim +12 /n/3.md"},
		{"/usr/bin/nvim", 12, "/usr/bin/nvim +12 /n/3.md"},
		{"vi", 12, "vi +12 /n/3.md"},
		{"nano", 12, "nano +12 /n/3.md"},
		{"micro", 12, "micro +12 /n/3.md"},
		{"emacs -nw", 12, "emacs -nw +12 /n/3.md"},
		{"emacsclient -t", 12, "emacsclient -t +12 /n/3.md"},
		{"kak", 12, "kak +12 /n/3.md"},
		{"hx", 12, "hx /n/3.md:12"},
		{"code", 12, "code --wait --goto /n/3.md:12"},
		{"code --wait", 12, "code --wait --goto /n/3.md:12"},
		{"codium -w", 12, "codium -w --goto /n/3.md:12"},
		{"code-insiders", 12, "code-insiders --wait --goto /n/3.md:12"},
		{"subl", 12, "subl -w /n/3.md:12"},
		{"subl --wait", 12, "subl --wait /n/3.md:12"},
		{"zed", 12, "zed --wait /n/3.md:12"},
		{"myeditor --flag", 12, "myeditor --flag /n/3.md"},
		{"nvim", 0, "nvim /n/3.md"},
		{"code", 0, "code --wait /n/3.md"},
		{"hx", 0, "hx /n/3.md"},
	}
	for _, tc := range cases {
		t.Run(tc.editor, func(t *testing.T) {
			got := strings.Join(editorArgs(tc.editor, "/n/3.md", tc.line), " ")
			if got != tc.want {
				t.Fatalf("editorArgs(%q, line %d) = %q; want %q", tc.editor, tc.line, got, tc.want)
			}
		})
	}
}

func TestEditFile(t *testing.T) {
	write := func(t *testing.T, content string) string {
		t.Helper()
		path := filepath.Join(t.TempDir(), "n.md")
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}

	t.Run("an editor that saved nothing", func(t *testing.T) {
		path := write(t, "hello")
		changed, err := EditFile(path, 3, func(string, int) error { return nil })
		if err != nil || changed {
			t.Fatalf("EditFile() = %v, %v; want unchanged", changed, err)
		}
	})

	t.Run("an editor that wrote", func(t *testing.T) {
		path := write(t, "hello")
		var gotPath string
		var gotLine int
		changed, err := EditFile(path, 3, func(p string, line int) error {
			gotPath, gotLine = p, line
			return os.WriteFile(p, []byte("hello world"), 0o600)
		})
		if err != nil || !changed {
			t.Fatalf("EditFile() = %v, %v; want changed", changed, err)
		}
		if gotPath != path || gotLine != 3 {
			t.Errorf("the runner got %q line %d", gotPath, gotLine)
		}
	})

	t.Run("an editor that failed", func(t *testing.T) {
		path := write(t, "hello")
		boom := errors.New("exit status 1")
		_, err := EditFile(path, 3, func(string, int) error { return boom })
		if !errors.Is(err, boom) {
			t.Fatalf("EditFile() = %v; want the editor's error", err)
		}
	})

	t.Run("a file that is not there", func(t *testing.T) {
		_, err := EditFile(filepath.Join(t.TempDir(), "missing.md"), 1, func(string, int) error { return nil })
		if err == nil {
			t.Fatal("EditFile() on a missing file succeeded")
		}
	})
}
