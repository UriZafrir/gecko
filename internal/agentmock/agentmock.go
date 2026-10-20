// Package agentmock is a stand-in model server for running real coding
// agents (Codex, OpenCode) in tests without an account: it speaks the
// OpenAI Responses API (Codex) and Chat Completions (OpenCode's
// OpenAI-compatible providers), streaming canned answers chosen by what
// the last prompt says:
//
//   - "slow": a long answer, a few words a second, to see the agent work
//     and to interrupt it;
//   - "list files" or "run ls": a shell tool call running ls, then a short
//     answer once the tool's output comes back;
//   - "ask me": the agent's question tool, asking which of two colors,
//     then a short answer;
//   - anything else: a one line greeting.
//
// Only tests and the fixture recorder use it; Gecko itself doesn't.
package agentmock

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Server is a running stand-in.
type Server struct {
	URL string // base URL, ending in /v1
	srv *http.Server
	ln  net.Listener
	log string

	mu   sync.Mutex
	n    int
	Reqs []string // the paths requested, in order
}

// Start listens on a free local port. With logDir set, every request body
// is saved there.
func Start(logDir string) (*Server, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	s := &Server{URL: "http://" + ln.Addr().String() + "/v1", ln: ln, log: logDir}
	s.srv = &http.Server{Handler: http.HandlerFunc(s.serve)}
	go s.srv.Serve(ln)
	return s, nil
}

// Close stops the server.
func (s *Server) Close() { s.srv.Close() }

const (
	Greeting  = "Hi! How can I help you today?"
	SlowStart = "Counting slowly:"
	ListDone  = "The directory has one file: a.txt."
	Question  = "Which color should the button be?"
	Answered  = "Got it, thanks."
	Title     = "Greeting"
)

func (s *Server) serve(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	s.mu.Lock()
	s.n++
	n := s.n
	s.Reqs = append(s.Reqs, r.Method+" "+r.URL.Path)
	s.mu.Unlock()
	if s.log != "" {
		_ = os.WriteFile(filepath.Join(s.log, fmt.Sprintf("%03d%s.json", n, strings.ReplaceAll(r.URL.Path, "/", "_"))), body, 0o644)
	}
	switch {
	case strings.HasSuffix(r.URL.Path, "/models"):
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"object":"list","data":[{"id":"mock-model","object":"model","created":0,"owned_by":"mock"}],"models":[]}`)
	case strings.HasSuffix(r.URL.Path, "/responses"):
		s.responses(w, r, body)
	case strings.HasSuffix(r.URL.Path, "/chat/completions"):
		s.chat(w, r, body)
	default:
		http.NotFound(w, r)
	}
}

// turn is what the next answer should be.
type turn struct {
	prompt   string // the last thing the user said
	toolDone bool   // a tool's output came back after it
	shell    string // the shell tool on offer, if any
	ask      string // the question tool on offer, if any
}

func (t turn) kind() string {
	p := strings.ToLower(t.prompt)
	switch {
	case strings.Contains(p, "title"):
		// Codex asks for a title for the conversation, quoting the prompt.
		return "title"
	case strings.Contains(p, "slow"):
		return "slow"
	case (strings.Contains(p, "list files") || strings.Contains(p, "run ls")) && !t.toolDone && t.shell != "":
		return "shell"
	case strings.Contains(p, "list files") || strings.Contains(p, "run ls"):
		return "listed"
	case strings.Contains(p, "ask me") && !t.toolDone && t.ask != "":
		return "ask"
	case strings.Contains(p, "ask me"):
		return "answered"
	}
	return "hi"
}

// call is the tool call to make, if the answer is one.
func (t turn) call() (name string, args map[string]any) {
	why := "List the files in this folder"
	opts := []any{map[string]any{"label": "Blue", "description": "Calm and familiar"}, map[string]any{"label": "Green", "description": "Fresh and bright"}}
	switch t.kind() {
	case "shell":
		switch t.shell {
		// Codex: asking to run outside the sandbox, so it asks the user first.
		case "shell":
			return t.shell, map[string]any{"command": []string{"bash", "-lc", "ls"}, "with_escalated_permissions": true, "justification": why}
		case "shell_command":
			return t.shell, map[string]any{"command": "ls", "sandbox_permissions": "require_escalated", "justification": why}
		case "exec_command":
			return t.shell, map[string]any{"cmd": "ls", "sandbox_permissions": "require_escalated", "justification": why}
		}
		return t.shell, map[string]any{"command": "ls", "description": "Lists files in the current directory"}
	case "ask":
		return t.ask, map[string]any{"questions": []any{map[string]any{"id": "color", "header": "Color", "question": Question, "options": opts}}}
	}
	return "", nil
}

// text returns what a message's content says, as a string or as parts.
func text(c any) string {
	switch c := c.(type) {
	case string:
		return c
	case []any:
		var b strings.Builder
		for _, p := range c {
			if m, ok := p.(map[string]any); ok {
				if s, ok := m["text"].(string); ok {
					b.WriteString(s)
				}
			}
		}
		return b.String()
	}
	return ""
}

// stream sends server-sent events, flushing each.
type stream struct {
	w http.ResponseWriter
	r *http.Request
}

func newStream(w http.ResponseWriter, r *http.Request) *stream {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(200)
	return &stream{w, r}
}

func (s *stream) send(event string, data any) bool {
	b, _ := json.Marshal(data)
	if event != "" {
		fmt.Fprintf(s.w, "event: %s\n", event)
	}
	fmt.Fprintf(s.w, "data: %s\n\n", b)
	if f, ok := s.w.(http.Flusher); ok {
		f.Flush()
	}
	return s.r.Context().Err() == nil
}

func (s *stream) sleep(d time.Duration) bool {
	select {
	case <-time.After(d):
		return true
	case <-s.r.Context().Done():
		return false
	}
}

// words is what an answer says, in pieces to stream, with the pause
// before each.
func words(kind string) ([]string, time.Duration) {
	switch kind {
	case "slow":
		w := []string{SlowStart}
		for i := 1; i <= 120; i++ {
			w = append(w, fmt.Sprintf(" %d", i))
		}
		return w, 500 * time.Millisecond
	case "listed":
		return strings.SplitAfter(ListDone, " "), 50 * time.Millisecond
	case "title":
		return []string{Title}, 0
	case "answered":
		return strings.SplitAfter(Answered, " "), 50 * time.Millisecond
	}
	return strings.SplitAfter(Greeting, " "), 50 * time.Millisecond
}

// responses answers the OpenAI Responses API, as Codex calls it.
func (s *Server) responses(w http.ResponseWriter, r *http.Request, body []byte) {
	var req struct {
		Input []map[string]any `json:"input"`
		Tools []map[string]any `json:"tools"`
	}
	_ = json.Unmarshal(body, &req)
	var t turn
	for _, it := range req.Input {
		typ, _ := it["type"].(string)
		switch {
		case typ == "message" && it["role"] == "user":
			// Codex sends its own context (AGENTS.md, environment) as user
			// messages in angle brackets; the prompt is the plain one.
			if p := strings.TrimSpace(text(it["content"])); p != "" && !strings.HasPrefix(p, "<") && !strings.HasPrefix(p, "#") {
				t.prompt, t.toolDone = p, false
			}
		case strings.HasSuffix(typ, "_call_output"):
			t.toolDone = true
		}
	}
	for _, tl := range req.Tools {
		switch n, _ := tl["name"].(string); n {
		case "shell", "shell_command", "exec_command":
			t.shell = n
		case "request_user_input":
			t.ask = n
		}
	}
	st := newStream(w, r)
	id := fmt.Sprintf("resp_%d", time.Now().UnixNano())
	if !st.send("response.created", map[string]any{"type": "response.created", "response": map[string]any{"id": id}}) {
		return
	}
	done := func() {
		st.send("response.completed", map[string]any{"type": "response.completed", "response": map[string]any{
			"id": id, "usage": map[string]any{"input_tokens": 100, "input_tokens_details": map[string]any{"cached_tokens": 0},
				"output_tokens": 10, "output_tokens_details": map[string]any{"reasoning_tokens": 0}, "total_tokens": 110}}})
	}
	if name, args := t.call(); name != "" {
		a, _ := json.Marshal(args)
		item := map[string]any{"type": "function_call", "id": "fc_1", "call_id": fmt.Sprintf("call_%d", time.Now().UnixNano()), "name": name, "arguments": string(a)}
		st.send("response.output_item.added", map[string]any{"type": "response.output_item.added", "output_index": 0, "item": item})
		st.send("response.output_item.done", map[string]any{"type": "response.output_item.done", "output_index": 0, "item": item})
		done()
		return
	}
	parts, pause := words(t.kind())
	msg := map[string]any{"type": "message", "role": "assistant", "id": "msg_1", "status": "in_progress", "content": []any{}}
	st.send("response.output_item.added", map[string]any{"type": "response.output_item.added", "output_index": 0, "item": msg})
	all := ""
	for _, p := range parts {
		if !st.sleep(pause) {
			return
		}
		all += p
		if !st.send("response.output_text.delta", map[string]any{"type": "response.output_text.delta", "item_id": "msg_1", "output_index": 0, "content_index": 0, "delta": p}) {
			return
		}
	}
	msg["status"] = "completed"
	msg["content"] = []any{map[string]any{"type": "output_text", "text": all, "annotations": []any{}}}
	st.send("response.output_item.done", map[string]any{"type": "response.output_item.done", "output_index": 0, "item": msg})
	done()
}

// chat answers Chat Completions, as OpenCode's OpenAI-compatible
// providers call it.
func (s *Server) chat(w http.ResponseWriter, r *http.Request, body []byte) {
	var req struct {
		Messages []map[string]any `json:"messages"`
		Tools    []struct {
			Function struct {
				Name string `json:"name"`
			} `json:"function"`
		} `json:"tools"`
		Stream bool `json:"stream"`
	}
	_ = json.Unmarshal(body, &req)
	var t turn
	system := ""
	for _, m := range req.Messages {
		switch m["role"] {
		case "system":
			system += text(m["content"])
		case "user":
			t.prompt, t.toolDone = text(m["content"]), false
		case "tool":
			t.toolDone = true
		}
	}
	for _, tl := range req.Tools {
		switch tl.Function.Name {
		case "bash":
			t.shell = "bash"
		case "question":
			t.ask = "question"
		}
	}
	id := fmt.Sprintf("chatcmpl-%d", time.Now().UnixNano())
	chunk := func(delta map[string]any, finish any) map[string]any {
		return map[string]any{"id": id, "object": "chat.completion.chunk", "created": time.Now().Unix(), "model": "mock-model",
			"choices": []any{map[string]any{"index": 0, "delta": delta, "finish_reason": finish}}}
	}
	// OpenCode asks for a short title for the session: no tools, a system
	// prompt about titles.
	isTitle := len(req.Tools) == 0 && strings.Contains(strings.ToLower(system), "title")
	if !req.Stream {
		w.Header().Set("Content-Type", "application/json")
		answer := Greeting
		if isTitle {
			answer = Title
		}
		json.NewEncoder(w).Encode(map[string]any{"id": id, "object": "chat.completion", "created": time.Now().Unix(), "model": "mock-model",
			"choices": []any{map[string]any{"index": 0, "message": map[string]any{"role": "assistant", "content": answer}, "finish_reason": "stop"}},
			"usage":   map[string]any{"prompt_tokens": 100, "completion_tokens": 10, "total_tokens": 110}})
		return
	}
	st := newStream(w, r)
	end := func(finish string) {
		c := chunk(map[string]any{}, finish)
		c["usage"] = map[string]any{"prompt_tokens": 100, "completion_tokens": 10, "total_tokens": 110}
		st.send("", c)
		fmt.Fprint(w, "data: [DONE]\n\n")
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
	}
	if isTitle {
		st.send("", chunk(map[string]any{"role": "assistant", "content": Title}, nil))
		end("stop")
		return
	}
	if name, args := t.call(); name != "" {
		a, _ := json.Marshal(args)
		st.send("", chunk(map[string]any{"role": "assistant", "tool_calls": []any{map[string]any{"index": 0,
			"id": fmt.Sprintf("call_%d", time.Now().UnixNano()), "type": "function",
			"function": map[string]any{"name": name, "arguments": string(a)}}}}, nil))
		end("tool_calls")
		return
	}
	parts, pause := words(t.kind())
	first := true
	for _, p := range parts {
		if !st.sleep(pause) {
			return
		}
		d := map[string]any{"content": p}
		if first {
			d["role"], first = "assistant", false
		}
		if !st.send("", chunk(d, nil)) {
			return
		}
	}
	end("stop")
}
