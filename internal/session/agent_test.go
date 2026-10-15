package session

import (
	"strings"
	"testing"
	"time"

	"github.com/gecko-term/gecko/internal/vt"
)

func TestAgentStatus(t *testing.T) {
	term := vt.New(80, 40, 100)
	term.Write([]byte("\x1b[?1049h\x1b[H\x1b[2J╭──────╮\r\n│ > fix the bug │\r\n✻ Thinking… (3s · esc to interrupt)\r\n"))
	st := agentStatus(term.ScreenLines(), 5*time.Second, time.Hour, false)
	if st.Status != "working" || st.Detail != "Thinking… (3s · esc to interrupt)" {
		t.Fatalf("%+v %q", st, term.ScreenLines())
	}
	term.Write([]byte("\x1b[H\x1b[2JEdit file src/main.go\r\nDo you want to make this edit?\r\n❯ 1. Yes\r\n  2. No\r\n"))
	st = agentStatus(term.ScreenLines(), 5*time.Second, time.Hour, false)
	if st.Status != "needs-input" {
		t.Fatalf("%+v %q", st, term.ScreenLines())
	}
	// Claude working, as drawn by Claude Code 2.1: what it's doing, not the
	// spinner's tip or the footer.
	term.Write([]byte("\x1b[H\x1b[2J● Found two issues: the waiting state and the card text.\r\n\r\n  Running gecko capture…\r\n\r\n✢ Clauding… (1m 55s · ↓ 7.8k tokens)\r\n  ⎿  Tip: Use ctrl+v to paste images from your clipboard\r\n\r\n────────\r\n❯\u00a0\r\n────────\r\n  ⏵⏵ bypass permissions on (shift+tab to cycle) · esc to interrupt · ← 1 agent\r\n"))
	st = agentStatus(term.ScreenLines(), 5*time.Second, time.Hour, false)
	if st.Status != "working" || st.Detail != "Found two issues: the waiting state and the card text." {
		t.Fatalf("working Claude: %+v", st)
	}
	// Claude idle at its prompt: neither the footer nor a "(y/n)" in what it
	// said is a question; the card shows what it said last.
	term.Write([]byte("\x1b[H\x1b[2J● Done. A (y/n) prompt now shows Yes and No.\r\n\r\n────────\r\n❯\u00a0\r\n────────\r\n  ⏵⏵ bypass permissions on (shift+tab to cycle)\r\n"))
	st = agentStatus(term.ScreenLines(), 5*time.Second, time.Hour, false)
	if st.Status != "idle" || st.Choices != nil || st.Detail != "Done. A (y/n) prompt now shows Yes and No." {
		t.Fatalf("idle Claude: %+v", st)
	}
	// ... and stays idle when it redraws (output a moment ago, no input).
	if st = agentStatus(term.ScreenLines(), 100*time.Millisecond, time.Hour, false); st.Status != "idle" {
		t.Fatalf("idle Claude redrawing: %+v", st)
	}
	// ... even when what it said mentions a working marker.
	term.Write([]byte("\x1b[H\x1b[2J● Done: it looks for \"esc to interrupt\" in the footer.\r\n\r\n────────\r\n❯\u00a0\r\n────────\r\n  ⏵⏵ bypass permissions on (shift+tab to cycle)\r\n"))
	if st = agentStatus(term.ScreenLines(), 5*time.Second, time.Hour, false); st.Status != "idle" {
		t.Fatalf("idle Claude quoting a marker: %+v", st)
	}
}

func TestAgentChoices(t *testing.T) {
	cases := []struct {
		screen   string
		question string
		choices  string
	}{
		{ // Claude's permission prompt, in its box, with key hints
			"╭──────────────────────────────────────╮\r\n│ Edit file                            │\r\n│ Do you want to make this edit to style.css? │\r\n│ ❯ 1. Yes                             │\r\n│   2. Yes, allow all edits during this session (shift+tab) │\r\n│   3. No, and tell Claude what to do differently (esc) │\r\n╰──────────────────────────────────────╯\r\n",
			"Do you want to make this edit to style.css?",
			"1=Yes 2=Yes, allow all edits during this session 3=No, and tell Claude what to do differently",
		},
		{ // a numbered list in the conversation above doesn't count; the menu below does
			"Steps:\r\n1. Build it\r\n2. Ship it\r\nWould you like to continue?\r\n> 1) Yes\r\n  2) No\r\n",
			"Would you like to continue?",
			"1=Yes 2=No",
		},
		{"Overwrite config.json? (y/N)\r\n", "Overwrite config.json? (y/N)", "y=Yes n=No"},
		{"Done. Press Enter to continue\r\n", "Done. Press Enter to continue", "enter=Continue"},
		{"Waiting for your input\r\n", "", ""},
	}
	for _, c := range cases {
		term := vt.New(100, 30, 100)
		term.Write([]byte("\x1b[H\x1b[2J" + c.screen))
		st := agentStatus(term.ScreenLines(), 5*time.Second, time.Hour, false)
		var got []string
		for _, ch := range st.Choices {
			got = append(got, ch.Key+"="+ch.Label)
		}
		if st.Status != "needs-input" || strings.Join(got, " ") != c.choices || (c.question != "" && st.Detail != c.question) {
			t.Errorf("screen %q:\n got %s %q [%s]\nwant needs-input %q [%s]", c.screen, st.Status, st.Detail, strings.Join(got, " "), c.question, c.choices)
		}
	}
}
