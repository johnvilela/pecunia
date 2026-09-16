package notes

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

const full = `---
title: "Plan: save #1"
priority: low
status: doing
target: 2026-12-16 # in 3 months
tags: [health, insurance]
accounts: [INTER, NUBON]
cards: []
goals: [3]
---

# Why

Because the current plan is bad.
`

func TestParse(t *testing.T) {
	t.Run("the full canonical form", func(t *testing.T) {
		d, err := Parse([]byte(full))
		if err != nil {
			t.Fatal(err)
		}
		want := Doc{
			Title: "Plan: save #1", Priority: "low", Status: "doing",
			Target: "2026-12-16", TargetPhrase: "in 3 months",
			Tags: []string{"health", "insurance"}, Accounts: []string{"INTER", "NUBON"},
			Cards: []string{}, Goals: []string{"3"},
			Body: "# Why\n\nBecause the current plan is bad.", BodyLine: 12,
		}
		if !reflect.DeepEqual(d, want) {
			t.Fatalf("Parse() =\n%#v\nwant\n%#v", d, want)
		}
	})

	t.Run("a minimal file", func(t *testing.T) {
		d, err := Parse([]byte("---\ntitle: Hello\n---\n"))
		if err != nil {
			t.Fatal(err)
		}
		if d.Title != "Hello" || d.Body != "" || d.BodyLine != 4 {
			t.Fatalf("Parse() = %#v", d)
		}
	})

	t.Run("no blank line before the body", func(t *testing.T) {
		d, err := Parse([]byte("---\ntitle: Hello\n---\nfirst line\n"))
		if err != nil {
			t.Fatal(err)
		}
		if d.Body != "first line" || d.BodyLine != 4 {
			t.Fatalf("Parse() = %#v", d)
		}
	})

	t.Run("block lists, scalars as lists, single quotes, comments, CRLF and a BOM", func(t *testing.T) {
		src := "\ufeff---\r\n" +
			"# a comment line\r\n" +
			"title: 'It''s fine'\r\n" +
			"\r\n" +
			"tags:\r\n" +
			"  - Health\r\n" +
			"  - \"two words\"\r\n" +
			"accounts: inter   # trailing comment\r\n" +
			"goals: [ 1 , 2 ]\r\n" +
			"---\r\n" +
			"body\r\n"
		d, err := Parse([]byte(src))
		if err != nil {
			t.Fatal(err)
		}
		if d.Title != "It's fine" {
			t.Errorf("title = %q", d.Title)
		}
		if !reflect.DeepEqual(d.Tags, []string{"Health", "two words"}) {
			t.Errorf("tags = %q", d.Tags)
		}
		if !reflect.DeepEqual(d.Accounts, []string{"inter"}) {
			t.Errorf("accounts = %q", d.Accounts)
		}
		if !reflect.DeepEqual(d.Goals, []string{"1", "2"}) {
			t.Errorf("goals = %q", d.Goals)
		}
		if d.Body != "body" {
			t.Errorf("body = %q", d.Body)
		}
	})

	t.Run("a phrase in the target is kept to resolve later", func(t *testing.T) {
		d, err := Parse([]byte("---\ntitle: x\ntarget: in 3 months\n---\n"))
		if err != nil {
			t.Fatal(err)
		}
		if d.Target != "in 3 months" || d.TargetPhrase != "" {
			t.Fatalf("target/phrase = %q/%q", d.Target, d.TargetPhrase)
		}
	})

	t.Run("an escaped quote does not end the quotes", func(t *testing.T) {
		d, err := Parse([]byte("---\ntitle: \"Say \\\" # hash\" # real comment\ntarget: \"2026-12-16\" # in 3 months\n---\n"))
		if err != nil {
			t.Fatal(err)
		}
		if d.Title != `Say " # hash` {
			t.Errorf("title = %q", d.Title)
		}
		if d.Target != "2026-12-16" || d.TargetPhrase != "in 3 months" {
			t.Errorf("target/phrase = %q/%q", d.Target, d.TargetPhrase)
		}
		d, err = Parse([]byte("---\ntitle: x\ntags: [\"a \\\" , b\", c]\n---\n"))
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(d.Tags, []string{`a " , b`, "c"}) {
			t.Errorf("tags = %q", d.Tags)
		}
	})

	t.Run("a hash inside a word is not a comment", func(t *testing.T) {
		d, err := Parse([]byte("---\ntitle: Issue#12\n---\n"))
		if err != nil {
			t.Fatal(err)
		}
		if d.Title != "Issue#12" {
			t.Fatalf("title = %q", d.Title)
		}
	})

	errs := []struct {
		name, src, want string
	}{
		{"no front matter", "title: x\n", "no front matter — the file must start with a line that is just ---"},
		{"never closes", "---\ntitle: x\n", "front matter never closes — missing the second ---"},
		{"a line that is not key: value", "---\ntitle: x\npriorty high\n---\n", `line 3: cannot read "priorty high" — expected key: value`},
		{"an unknown key", "---\ntitle: x\npriorty: high\n---\n", `line 3: unknown key "priorty" — known keys: title, priority, status, target, tags, accounts, cards, goals`},
		{"a duplicate key", "---\ntitle: x\ntitle: y\n---\n", `line 3: key "title" appears twice`},
		{"a list that never closes", "---\ntitle: x\ntags: [a, b\n---\n", "line 3: list never closes — missing ]"},
		{"a block item under a scalar key", "---\ntitle:\n  - x\n---\n", `line 3: cannot read "- x"`},
	}
	for _, tc := range errs {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse([]byte(tc.src))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Parse() = %v; want %q", err, tc.want)
			}
		})
	}
}

func TestParseFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "n.md")
	if err := os.WriteFile(path, []byte(full), 0o600); err != nil {
		t.Fatal(err)
	}
	d, err := ParseFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if d.Title != "Plan: save #1" {
		t.Fatalf("title = %q", d.Title)
	}
	if _, err := ParseFile(filepath.Join(t.TempDir(), "missing.md")); err == nil {
		t.Fatal("ParseFile on a missing file succeeded")
	}
}

func TestRender(t *testing.T) {
	t.Run("the canonical form round-trips", func(t *testing.T) {
		n := health()
		n.Title = "Plan: save #1"
		n.Status = StatusDoing
		n.Accounts = []string{"INTER", "NUBON"}
		n.Goals = []int64{3}
		got := string(Render(n, "# Why\n\nBecause the current plan is bad.\n\n"))
		if got != full {
			t.Fatalf("Render() =\n%s\nwant\n%s", got, full)
		}
		d, err := Parse([]byte(got))
		if err != nil {
			t.Fatal(err)
		}
		back, err := d.Note(time.Now())
		if err != nil {
			t.Fatal(err)
		}
		back.ID, back.Path, back.CreatedAt, back.UpdatedAt = n.ID, n.Path, n.CreatedAt, n.UpdatedAt
		if !reflect.DeepEqual(back, n) {
			t.Fatalf("round trip lost something:\n%#v\nwant\n%#v", back, n)
		}
	})

	t.Run("no target, no phrase, empty lists, empty body", func(t *testing.T) {
		n := Note{Title: "Hello", Priority: PriorityMedium, Status: StatusOpen}
		want := "---\ntitle: Hello\npriority: medium\nstatus: open\ntarget:\ntags: []\naccounts: []\ncards: []\ngoals: []\n---\n\n"
		if got := string(Render(n, "")); got != want {
			t.Fatalf("Render() =\n%q\nwant\n%q", got, want)
		}
	})

	t.Run("a target without a phrase has no comment", func(t *testing.T) {
		n := Note{Title: "Hello", Priority: PriorityMedium, Status: StatusOpen, Target: "2026-12-16"}
		if got := string(Render(n, "")); !strings.Contains(got, "\ntarget: 2026-12-16\n") {
			t.Fatalf("Render() = %q", got)
		}
	})

	t.Run("a phrase that is the day itself is dropped", func(t *testing.T) {
		n := Note{Title: "Hello", Priority: PriorityMedium, Status: StatusOpen, Target: "2026-12-16", TargetPhrase: "2026-12-16"}
		if got := string(Render(n, "")); !strings.Contains(got, "\ntarget: 2026-12-16\n") {
			t.Fatalf("Render() = %q", got)
		}
	})

	t.Run("titles that need quoting get it", func(t *testing.T) {
		for title, want := range map[string]string{
			"Plain":         "title: Plain\n",
			"Colon: here":   `title: "Colon: here"` + "\n",
			"Hash # here":   `title: "Hash # here"` + "\n",
			"[bracket]":     `title: "[bracket]"` + "\n",
			`say "hi"`:      `title: "say \"hi\""` + "\n",
			" padded ":      `title: " padded "` + "\n",
			"it's ok":       "title: it's ok\n",
			"Renegociar já": "title: Renegociar já\n",
		} {
			n := Note{Title: title, Priority: PriorityLow, Status: StatusOpen}
			got := string(Render(n, ""))
			if !strings.Contains(got, want) {
				t.Errorf("Render(%q) = %q; want it to contain %q", title, got, want)
			}
			d, err := Parse([]byte(got))
			if err != nil {
				t.Errorf("Parse(Render(%q)) = %v", title, err)
			} else if d.Title != title {
				t.Errorf("Parse(Render(%q)).Title = %q", title, d.Title)
			}
		}
	})
}

func TestRenderDraft(t *testing.T) {
	n := Note{Title: "Get a better health care", Priority: PriorityLow, Status: StatusOpen,
		TargetPhrase: "in 3 months", Tags: []string{"health"}}
	src, line := RenderDraft(n)
	got := string(src)
	for _, want := range []string{
		"title: Get a better health care\n",
		"priority: low ",
		"# low | medium | high | critical",
		"target: in 3 months ",
		"tags: [health] ",
		"accounts: [] ",
		"goals: [] ",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("draft lacks %q:\n%s", want, got)
		}
	}
	if line != 11 || strings.Count(got, "\n") != 11 || !strings.HasSuffix(got, "---\n\n") {
		t.Errorf("body line = %d over %d lines; want 11 over 11 with an empty last line:\n%s", line, strings.Count(got, "\n"), got)
	}
	d, err := Parse(src)
	if err != nil {
		t.Fatalf("the draft does not parse: %v", err)
	}
	if d.Title != n.Title || d.Priority != "low" || d.Target != "in 3 months" || d.Body != "" || d.BodyLine != 11 {
		t.Errorf("Parse(draft) = %#v", d)
	}
}

func TestDocNote(t *testing.T) {
	now := time.Date(2026, 9, 16, 12, 0, 0, 0, time.Local)

	t.Run("defaults and normalisation", func(t *testing.T) {
		d := Doc{Title: "  Hello  ", Priority: " HIGH ", Status: "", Target: "in 3 months",
			Tags: []string{"Health", "health", "b,c"}, Accounts: []string{" inter ", "INTER", ""},
			Cards: []string{"nucrd"}, Goals: []string{" 3 "}}
		n, err := d.Note(now)
		if err != nil {
			t.Fatal(err)
		}
		want := Note{Title: "Hello", Priority: PriorityHigh, Status: StatusOpen,
			Target: "2026-12-16", TargetPhrase: "in 3 months",
			Tags: []string{"bc", "health"}, Accounts: []string{"INTER"},
			Cards: []string{"NUCRD"}, Goals: []int64{3}}
		if !reflect.DeepEqual(n, want) {
			t.Fatalf("Note() =\n%#v\nwant\n%#v", n, want)
		}
	})

	t.Run("an ISO target keeps the phrase from its comment", func(t *testing.T) {
		d := Doc{Title: "x", Target: "2026-12-16", TargetPhrase: "in 3 months"}
		n, err := d.Note(now)
		if err != nil {
			t.Fatal(err)
		}
		if n.Target != "2026-12-16" || n.TargetPhrase != "in 3 months" {
			t.Fatalf("target/phrase = %q/%q", n.Target, n.TargetPhrase)
		}
	})

	t.Run("a bad target names the grammar", func(t *testing.T) {
		_, err := Doc{Title: "x", Target: "soon"}.Note(now)
		if err == nil || !strings.Contains(err.Error(), "target:") {
			t.Fatalf("Note() = %v", err)
		}
	})

	t.Run("a goal that is not a number", func(t *testing.T) {
		_, err := Doc{Title: "x", Goals: []string{"laptop"}}.Note(now)
		if err == nil || !strings.Contains(err.Error(), `goals: "laptop" is not a goal id`) {
			t.Fatalf("Note() = %v", err)
		}
	})

	t.Run("validation runs", func(t *testing.T) {
		_, err := Doc{Title: "x", Priority: "urgent"}.Note(now)
		if err == nil || !strings.Contains(err.Error(), "not a priority") {
			t.Fatalf("Note() = %v", err)
		}
	})
}
