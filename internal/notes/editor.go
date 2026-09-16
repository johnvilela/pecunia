package notes

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

// Editor is the owner's editor: $VISUAL, then $EDITOR, then vi — the order
// every other tool that opens one uses. The value may carry arguments
// ("code --wait"); editorArgs splits them.
func Editor() string {
	for _, env := range []string{"VISUAL", "EDITOR"} {
		if v := strings.TrimSpace(os.Getenv(env)); v != "" {
			return v
		}
	}
	return "vi"
}

// The editors whose cursor can be put on a line, by how they spell it. Any
// other editor is opened on the file with nothing else said, which every
// editor understands.
var (
	plusLine = []string{"vi", "vim", "nvim", "gvim", "vis", "nano", "micro", "emacs", "emacsclient",
		"kak", "jed", "ne", "mcedit", "gedit"}
	colonLine = []string{"hx", "zed"}
	vscode    = []string{"code", "codium", "code-insiders"}
)

// editorArgs is the command line that opens path in the editor with the
// cursor on line, or just on the file when line is 0. The GUI editors that
// return at once are told to wait: the file is read back the moment the
// command returns.
func editorArgs(editor, path string, line int) []string {
	argv := strings.Fields(editor)
	name := filepath.Base(argv[0])
	waiting := slices.ContainsFunc(argv[1:], func(a string) bool { return a == "-w" || a == "--wait" })
	switch {
	case slices.Contains(plusLine, name):
		if line > 0 {
			argv = append(argv, "+"+strconv.Itoa(line))
		}
		return append(argv, path)
	case slices.Contains(vscode, name):
		if !waiting {
			argv = append(argv, "--wait")
		}
		if line > 0 {
			return append(argv, "--goto", path+":"+strconv.Itoa(line))
		}
		return append(argv, path)
	case name == "subl":
		if !waiting {
			argv = append(argv, "-w")
		}
		return append(argv, at(path, line))
	case slices.Contains(colonLine, name):
		if name == "zed" && !waiting {
			argv = append(argv, "--wait")
		}
		return append(argv, at(path, line))
	}
	return append(argv, path)
}

// at is path:line, or just path when there is no line.
func at(path string, line int) string {
	if line > 0 {
		return path + ":" + strconv.Itoa(line)
	}
	return path
}

// OpenEditor hands the terminal to the editor on path, cursor on line, and
// returns when the editor does. The one call in this package no test can
// make: the rest of the flow takes a runner and is driven with a fake one.
func OpenEditor(path string, line int) error {
	argv := editorArgs(Editor(), path, line)
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return cmd.Run()
}

// EditFile runs the editor on path and reports whether the file came back
// different. A byte-for-byte comparison rather than an mtime: an editor that
// writes the same content on :wq has changed nothing worth re-reading.
func EditFile(path string, line int, run func(path string, line int) error) (changed bool, err error) {
	before, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	if err := run(path, line); err != nil {
		return false, err
	}
	after, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	return !bytes.Equal(before, after), nil
}
