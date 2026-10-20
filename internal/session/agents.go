package session

import (
	"regexp"
	"strings"
	"time"

	"github.com/gecko-term/gecko/internal/proto"
	"github.com/gecko-term/gecko/internal/vt"
)

// agentStatusOf reads an agent's screen with what that agent is known to
// draw: Codex and OpenCode by their own screens (recorded in
// testdata/agents), every other agent by the general rules of agentStatus.
// The agent-specific readers never take output alone for work: a TUI
// redraws while it waits (a blinking title, a timer), and what says it
// works is its own marker.
func agentStatusOf(name string, screen []string, sinceOutput, sinceInput time.Duration, attention bool) proto.AgentState {
	switch name {
	case "codex":
		return codexStatus(screen, attention)
	case "opencode":
		return opencodeStatus(screen, attention)
	}
	return agentStatus(screen, sinceOutput, sinceInput, attention)
}

// trimBlank drops the blank lines at the bottom of a screen.
func trimBlank(screen []string) []string {
	for len(screen) > 0 && strings.TrimSpace(screen[len(screen)-1]) == "" {
		screen = screen[:len(screen)-1]
	}
	return screen
}

func clip(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n])
	}
	return s
}

var (
	// Codex's composer: "› " at the left edge, with the placeholder or
	// what is being typed. A numbered "› 1. Yes" is a menu's selection.
	reCodexComposer = regexp.MustCompile(`^›(\s|$)`)
	reCodexOption   = regexp.MustCompile(`^›\s+[1-9][.)]\s`)
	// Its status line above the composer while it works, e.g.
	// "• Working (12s • esc to interrupt)" or "• Running ls (3s • esc to interrupt)".
	reCodexWorking = regexp.MustCompile(`^\s*[•◦]\s+(.+?)\s+\([^()]*esc to interrupt\)\s*$`)
	// "■ Conversation interrupted - use /feedback if something went wrong"
	reCodexFeedback = regexp.MustCompile(`\s+-\s+use /feedback\b.*$`)
	// The hint under a dialog that waits for an answer: "Press enter to
	// confirm or esc to cancel", "enter continue · esc quit", "Press enter
	// to continue", "enter to submit answer".
	reCodexDialog = regexp.MustCompile(`(?i)\benter\b(?:\s+to)?\s+(?:confirm|continue|submit)`)
	// An item in the transcript: what Codex said or did ("• ", "■ ",
	// "✔ ", "✗ " at the left edge).
	reCodexItem = regexp.MustCompile(`^[•■✔✗]\s+(.+)$`)
	// A shortcut at the end of an answer: "(y)", "(p)", "(esc)", "(tab)".
	reCodexKey = regexp.MustCompile(`\s*\((?:[a-z]|esc|tab|enter)\)$`)
)

// codexStatus reads Codex's screen (codex-cli 0.163): at its composer it
// is idle, unless the status line above the composer says it works; a
// dialog with answers (approval, a question, trusting a folder, signing
// in) waits for the user. While an answer streams in, the status line
// hides: the title and OSC 7501 state Codex sets say it works then.
func codexStatus(screen []string, attention bool) proto.AgentState {
	lines := trimBlank(screen)
	st := proto.AgentState{Status: "idle"}
	if len(lines) == 0 {
		return st
	}
	composer := codexComposer(lines)
	if composer < 0 {
		// A dialog with answers in the composer's place, or a screen of
		// its own (signing in, trusting a folder).
		hint := ""
		for i, n := len(lines)-1, 0; i >= 0 && n < 3; i-- {
			if l := strings.TrimSpace(lines[i]); l != "" {
				hint, n = l+" "+hint, n+1
			}
		}
		choices, question := codexChoices(lines)
		if choices != nil || reCodexDialog.MatchString(hint) {
			st.Status, st.Choices, st.Detail = "needs-input", choices, question
		} else if attention {
			st.Status = "needs-input"
		}
		return st
	}
	// The latest item since the last thing the user said.
	user := -1
	for i := composer - 1; i >= 0; i-- {
		if reCodexComposer.MatchString(lines[i]) && !reCodexOption.MatchString(lines[i]) {
			user = i
			break
		}
	}
	working := ""
	for i := max(user+1, composer-4); i < composer; i++ {
		if m := reCodexWorking.FindStringSubmatch(lines[i]); m != nil {
			working = m[1]
		}
	}
	for i := composer - 1; i > user; i-- {
		if m := reCodexItem.FindStringSubmatch(lines[i]); m != nil && !reCodexWorking.MatchString(lines[i]) {
			st.Detail = reCodexFeedback.ReplaceAllString(strings.TrimSpace(m[1]), "")
			break
		}
	}
	switch {
	case working != "":
		st.Status = "working"
		if st.Detail == "" {
			st.Detail = working
		}
	case attention:
		st.Status = "needs-input"
	}
	st.Detail = clip(st.Detail, 160)
	return st
}

// codexComposer finds Codex's composer: the last "› " line at the left
// edge with only its footer under it (no menu, no dialog hint). It is -1
// when a dialog takes its place; the user's earlier messages, also "› ",
// have the conversation under them.
func codexComposer(lines []string) int {
	for i := len(lines) - 1; i >= 0 && i >= len(lines)-16; i-- {
		if reCodexOption.MatchString(lines[i]) || reOption.MatchString(lines[i]) || reCodexDialog.MatchString(lines[i]) {
			return -1
		}
		if reCodexComposer.MatchString(lines[i]) {
			n := 0
			for _, l := range lines[i+1:] {
				if strings.TrimSpace(l) != "" {
					n++
				}
			}
			if n <= 8 {
				return i
			}
			return -1
		}
	}
	return -1
}

// codexChoices reads the answers of a Codex dialog and its question: the
// nearest sentence above the menu that asks one ("Would you like to run
// the following command?", "Trust this folder?"), or else the first line
// of the paragraph above the menu.
func codexChoices(lines []string) ([]proto.Choice, string) {
	choices, _ := askChoices(lines)
	if choices == nil {
		return nil, ""
	}
	// The labels in full (askChoices shortens them), without the key that
	// picks them or the note next to them ("Blue      Calm and familiar").
	menu := -1
	full := map[string]string{}
	for i, l := range lines {
		if m := reOption.FindStringSubmatch(l); m != nil {
			if m[1] == "1" {
				menu, full = i, map[string]string{}
			}
			full[m[1]] = m[2]
		}
	}
	for i := range choices {
		l, _, _ := strings.Cut(strings.TrimSpace(full[choices[i].Key]), "  ")
		l = reCodexKey.ReplaceAllString(reKeyHint.ReplaceAllString(strings.TrimSpace(l), ""), "")
		if r := []rune(l); len(r) > 60 {
			l = string(r[:60]) + "…"
		}
		choices[i].Label = l
	}
	question := ""
	for i := menu - 1; i >= 0 && i >= menu-12; i-- {
		l := strings.TrimSpace(lines[i])
		if q, _, ok := strings.Cut(l, "?"); ok && l != "" && l[0] >= 'A' && l[0] <= 'Z' {
			question = q + "?"
			break
		}
	}
	if question == "" {
		top := ""
		for i := menu - 1; i >= 0; i-- {
			l := strings.TrimSpace(lines[i])
			if l == "" {
				if top != "" {
					break
				}
				continue
			}
			top = l
		}
		question = top
	}
	return choices, clip(question, 160)
}

var (
	// OpenCode's prompt box ends in a half-block rule ("╹▀▀▀…"); its
	// footer is under it.
	reOCBoxEnd = regexp.MustCompile(`^\s*╹▀{8,}`)
	// The footer while it works: a moving bar and "esc interrupt" (after
	// one Esc, "esc again to interrupt"; on a narrow screen the words wrap).
	reOCWorking = regexp.MustCompile(`[⬝■]{8}|\besc\s+(again\s+to\s+)?interrupt\b`)
	// A dialog in the prompt's place: "△ Permission required",
	// "△ Always allow".
	reOCDialog = regexp.MustCompile(`^┃\s+△\s+(.+)$`)
	// A question's hint: "↑↓ select  enter submit  esc dismiss".
	reOCQuestion = regexp.MustCompile(`(?i)\benter submit\b`)
	// The line under each answer: "▣  Build · Claude Sonnet · 1.3s".
	reOCMeta = regexp.MustCompile(`^▣\s`)
	// A reasoning step, folded: "+ Thought: 1.0s".
	reOCThought = regexp.MustCompile(`^[+-] Thought: `)
	// The key hints after a dialog's buttons: "ctrl+f fullscreen",
	// "⇆ select", "enter confirm" (cut short on a narrow screen).
	reOCKeyHint = regexp.MustCompile(`(?i)^(ctrl\+|⇆|enter\b|esc\b|tab\b)|\bselect\b|\bfullscreen\b`)
	reOCGap     = regexp.MustCompile(`\s{2,}`)
	// Where the sidebar's text starts on a line that has its own.
	reOCSidebar = regexp.MustCompile(`\S\s{8,}\S`)
)

// opencodeLine is a line of OpenCode's screen without what its sidebar
// (wide screens) puts to the right: cut at width cells, then at a long gap.
func opencodeLine(l string, width int) string {
	if width > 0 {
		w := 0
		for i, r := range l {
			if w += vt.RuneWidth(r); w > width {
				l = l[:i]
				break
			}
		}
	}
	lead := len(l) - len(strings.TrimLeft(l, " "))
	if loc := reOCSidebar.FindStringIndex(l[lead:]); loc != nil {
		l = l[:lead+loc[0]+1]
	}
	return strings.TrimRight(l, " ")
}

// opencodeStatus reads OpenCode's screen (opencode 1.19): with its
// prompt box showing it works when the footer under the box shows its
// bar and "esc interrupt", and is idle otherwise; a permission dialog or
// a question in the box's place waits for the user.
func opencodeStatus(screen []string, attention bool) proto.AgentState {
	raw := trimBlank(screen)
	st := proto.AgentState{Status: "idle"}
	// The main column: as wide as the prompt box's rule, where it shows.
	width := 0
	box := -1
	for i, below := len(raw)-1, 0; i >= 0 && below <= 4; i-- {
		if strings.TrimSpace(raw[i]) != "" {
			below++
		}
		if reOCBoxEnd.MatchString(raw[i]) {
			box = i
			for _, r := range strings.TrimRight(raw[i], " ") {
				width += vt.RuneWidth(r)
			}
			break
		}
	}
	lines := make([]string, len(raw))
	for i, l := range raw {
		lines[i] = strings.TrimSpace(opencodeLine(l, width))
	}
	if box < 0 {
		// A dialog in the box's place.
		for i := len(lines) - 1; i >= 0 && i >= len(lines)-20; i-- {
			if m := reOCDialog.FindStringSubmatch(lines[i]); m != nil {
				st.Status = "needs-input"
				st.Choices, st.Detail = opencodeButtons(lines[i+1:]), strings.TrimSpace(m[1])
				for _, l := range lines[i+1:] {
					l = strings.TrimSpace(strings.TrimLeft(l, "┃"))
					if l != "" && !strings.HasPrefix(l, "# ") {
						st.Detail += ": " + l
						break
					}
				}
				st.Detail = clip(st.Detail, 160)
				return st
			}
		}
		tail := lines[max(0, len(lines)-20):]
		for _, l := range tail {
			if reOCQuestion.MatchString(l) {
				st.Status = "needs-input"
				st.Choices, st.Detail = askChoices(tail)
				st.Detail = clip(st.Detail, 160)
				return st
			}
		}
	}
	if box >= 0 {
		for _, l := range lines[box+1:] {
			if reOCWorking.MatchString(l) {
				st.Status = "working"
			}
		}
	}
	if st.Status == "idle" && attention {
		st.Status = "needs-input"
	}
	// What it said or did last: the conversation's lines, not the boxes
	// (the user's messages, the prompt) nor the line under each answer.
	end := box
	if end < 0 {
		end = len(lines)
	}
	text := func(l string) bool {
		// The logo on the home screen is block elements only.
		return l != "" && !strings.HasPrefix(l, "┃") && !strings.HasPrefix(l, "╹") && !reOCMeta.MatchString(l) &&
			!reOCThought.MatchString(l) && !reJunk.MatchString(l) && strings.Trim(l, " ▀▄█") != ""
	}
	// The first line of the last answer (its paragraphs and lists run
	// up to a box or a reasoning step), or that the last one was
	// interrupted ("▣  Build · Kimi K3 · interrupted").
	for i := end - 1; i >= 0; i-- {
		if reOCMeta.MatchString(lines[i]) && strings.HasSuffix(lines[i], "· interrupted") {
			st.Detail = "Interrupted"
			break
		}
		if !text(lines[i]) {
			continue
		}
		top := i
		for j := i - 1; j >= 0 && (lines[j] == "" || text(lines[j])); j-- {
			if lines[j] != "" {
				top = j
			}
		}
		st.Detail = clip(lines[top], 160)
		break
	}
	return st
}

// opencodeButtons reads a dialog's buttons, e.g. "Allow once   Allow
// always   Reject": Enter takes the first (selected) one, Right moves to
// the next.
func opencodeButtons(lines []string) []proto.Choice {
	for _, l := range lines {
		l = strings.TrimSpace(strings.TrimLeft(l, "┃"))
		var labels []string
		for _, q := range reOCGap.Split(l, -1) {
			if q = strings.TrimSpace(q); reOCKeyHint.MatchString(q) {
				break
			} else if q != "" {
				labels = append(labels, q)
			}
		}
		if len(labels) < 2 || !strings.Contains(l, "   ") {
			continue
		}
		var cs []proto.Choice
		for i, lb := range labels {
			key := "enter"
			if i > 0 {
				key = strings.Repeat("\x1b[C", i) + "\r"
			}
			cs = append(cs, proto.Choice{Key: key, Label: lb})
		}
		return cs
	}
	return nil
}
