package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ── minimal LSP client ────────────────────────────────────────────────────────
//
// Only what textDocument/formatting needs: the JSON-RPC framing, initialize,
// document sync and the formatting request. The server is started once and
// reused, because its first-time workspace scan is the slow part.

type edLSP struct {
	mu      sync.Mutex
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	stdout  *bufio.Reader
	nextID  int
	pending map[int]chan json.RawMessage
	opened  map[string]int // uri → document version
	ready   bool
	dead    bool
	warm    bool // the workspace scan has finished
}

// alive reports whether the server is still answering.
func (c *edLSP) alive() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.ready && !c.dead
}

func (c *edLSP) markDead() {
	c.mu.Lock()
	c.dead = true
	c.mu.Unlock()
}

var (
	edLSPMu   sync.Mutex
	edLSPInst *edLSP
)

// edLSPFormat formats src through lua-language-server, starting the server on
// first use.
func edLSPFormat(path, src string) (string, error) {
	c, err := edLSPClient()
	if err != nil {
		return "", err
	}
	return c.formatDocument(path, src)
}

// edHaveLuaLSP reports whether a language server is installed, independently of
// which formatter was picked - symbols can use it even when stylua formats.
func edHaveLuaLSP() bool {
	if !edUseLSP {
		return false
	}
	_, err := exec.LookPath("lua-language-server")
	return err == nil
}

// edUseLSP gates every use of the language server. Indexing a workspace and
// answering completions costs real CPU and memory, which is not always a trade
// worth making, so it can be switched off from the Options menu.
var edUseLSP = true

// edSetUseLSP turns language server support on or off, shutting the server down
// when it is no longer wanted rather than leaving it resident.
func edSetUseLSP(on bool) {
	edUseLSP = on
	if !on {
		edStopLSP()
		edClearDiagnostics()
	}
	edResetSymbolCache()
}

// edLSPDocumentSymbols asks the language server for a document's symbols and
// flattens them to line ranges. Called from a goroutine, never the render loop.
func edLSPDocumentSymbols(path, src string) ([]edSymbol, error) {
	c, err := edLSPClient()
	if err != nil {
		return nil, err
	}
	return c.documentSymbols(path, src)
}

// lspSymbol covers both shapes a server may answer with: the nested
// DocumentSymbol (range + children) and the flat SymbolInformation (location).
type lspSymbol struct {
	Name  string `json:"name"`
	Kind  int    `json:"kind"`
	Range *struct {
		Start edLSPPos `json:"start"`
		End   edLSPPos `json:"end"`
	} `json:"range"`
	Location *struct {
		Range struct {
			Start edLSPPos `json:"start"`
			End   edLSPPos `json:"end"`
		} `json:"range"`
	} `json:"location"`
	Children []lspSymbol `json:"children"`
}

func (c *edLSP) documentSymbols(path, src string) ([]edSymbol, error) {
	if !c.ready {
		return nil, fmt.Errorf("language server not ready")
	}
	uri := edDocURI(path)
	c.syncDocument(uri, src)

	raw, err := c.request("textDocument/documentSymbol", map[string]any{
		"textDocument": map[string]any{"uri": uri},
	})
	if err != nil {
		return nil, err
	}
	var syms []lspSymbol
	if err := json.Unmarshal(raw, &syms); err != nil {
		return nil, fmt.Errorf("unexpected documentSymbol reply")
	}

	var out []edSymbol
	var walk func([]lspSymbol)
	walk = func(list []lspSymbol) {
		for _, sym := range list {
			// 12 is SymbolKind.Function, 6 is Method.
			if r := sym.Range; r != nil && (sym.Kind == 12 || sym.Kind == 6) {
				out = append(out, edSymbol{sym.Name, r.Start.Line, r.End.Line})
			} else if l := sym.Location; l != nil && (sym.Kind == 12 || sym.Kind == 6) {
				out = append(out, edSymbol{sym.Name, l.Range.Start.Line, l.Range.End.Line})
			}
			walk(sym.Children)
		}
	}
	walk(syms)
	return out, nil
}

// edStopLSP shuts the language server down; safe to call when none was started.
func edStopLSP() {
	edLSPMu.Lock()
	defer edLSPMu.Unlock()
	if edLSPInst != nil {
		edLSPInst.stop()
		edLSPInst = nil
	}
}

func (c *edLSP) start() error {
	bin, err := exec.LookPath("lua-language-server")
	if err != nil {
		return err
	}
	root, err := os.Getwd()
	if err != nil {
		return err
	}

	c.cmd = exec.Command(bin)
	if c.stdin, err = c.cmd.StdinPipe(); err != nil {
		return err
	}
	stdout, err := c.cmd.StdoutPipe()
	if err != nil {
		return err
	}
	c.stdout = bufio.NewReader(stdout)
	c.cmd.Stderr = nil // keep the server's chatter out of our stdout
	if err := c.cmd.Start(); err != nil {
		return err
	}
	go c.readLoop()

	if _, err := c.request("initialize", map[string]any{
		"processId": os.Getpid(),
		"rootUri":   edPathToURI(root),
		"capabilities": map[string]any{
			"textDocument": map[string]any{
				"formatting":      map[string]any{"dynamicRegistration": false},
				"synchronization": map[string]any{"dynamicRegistration": false},
				"documentSymbol":  map[string]any{"hierarchicalDocumentSymbolSupport": true},
				"hover":           map[string]any{"contentFormat": []string{"plaintext", "markdown"}},
				"signatureHelp":   map[string]any{"dynamicRegistration": false},
				"completion": map[string]any{
					"completionItem": map[string]any{"snippetSupport": false},
				},
			},
		},
	}); err != nil {
		_ = c.cmd.Process.Kill()
		return err
	}
	c.notify("initialized", map[string]any{})
	c.ready = true
	return nil
}

func (c *edLSP) stop() {
	if c.cmd == nil || c.cmd.Process == nil {
		return
	}
	c.notify("exit", nil)
	_ = c.stdin.Close()
	_ = c.cmd.Process.Kill()
	_, _ = c.cmd.Process.Wait()
}

func (c *edLSP) send(msg any) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, err := io.WriteString(c.stdin, fmt.Sprintf("Content-Length: %d\r\n\r\n", len(data))); err != nil {
		c.dead = true
		return err
	}
	if _, err = c.stdin.Write(data); err != nil {
		c.dead = true
	}
	return err
}

func (c *edLSP) notify(method string, params any) {
	_ = c.send(map[string]any{"jsonrpc": "2.0", "method": method, "params": params})
}

func (c *edLSP) request(method string, params any) (json.RawMessage, error) {
	c.mu.Lock()
	c.nextID++
	id := c.nextID
	ch := make(chan json.RawMessage, 1)
	c.pending[id] = ch
	c.mu.Unlock()

	if err := c.send(map[string]any{
		"jsonrpc": "2.0", "id": id, "method": method, "params": params,
	}); err != nil {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return nil, err
	}

	select {
	case res := <-ch:
		return res, nil
	case <-time.After(20 * time.Second):
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return nil, fmt.Errorf("%s timed out", method)
	}
}

func (c *edLSP) readLoop() {
	defer c.markDead()
	for {
		length := 0
		for {
			line, err := c.stdout.ReadString('\n')
			if err != nil {
				return
			}
			line = strings.TrimRight(line, "\r\n")
			if line == "" {
				break
			}
			if v, ok := strings.CutPrefix(line, "Content-Length: "); ok {
				length, _ = strconv.Atoi(v)
			}
		}
		if length == 0 {
			continue
		}
		body := make([]byte, length)
		if _, err := io.ReadFull(c.stdout, body); err != nil {
			return
		}

		var msg struct {
			ID     *int            `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
			Result json.RawMessage `json:"result"`
			Error  *struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.Unmarshal(body, &msg); err != nil {
			continue
		}

		switch {
		case msg.ID == nil:
			// A notification. Diagnostics arrive this way, unasked for, after
			// the server has looked at a document.
			if msg.Method == "textDocument/publishDiagnostics" {
				edStoreDiagnostics(msg.Params)
			}
			continue

		case msg.Method != "":
			// A request from the server, such as workspace/configuration.
			// Answer with null rather than leaving it waiting on us; the
			// defaults are what we want anyway.
			_ = c.send(map[string]any{"jsonrpc": "2.0", "id": *msg.ID, "result": nil})
			continue
		}

		if msg.Error != nil {
			msg.Result = nil
		}

		c.mu.Lock()
		ch, ok := c.pending[*msg.ID]
		delete(c.pending, *msg.ID)
		c.mu.Unlock()
		if ok {
			ch <- msg.Result
		}
	}
}

// syncDocument opens the URI on first use and sends full-text changes after.
func (c *edLSP) syncDocument(uri, text string) {
	c.mu.Lock()
	version, open := c.opened[uri]
	version++
	c.opened[uri] = version
	c.mu.Unlock()

	item := map[string]any{"uri": uri, "version": version}
	if !open {
		c.notify("textDocument/didOpen", map[string]any{
			"textDocument": map[string]any{
				"uri": uri, "languageId": "lua", "version": version, "text": text,
			},
		})
		return
	}
	c.notify("textDocument/didChange", map[string]any{
		"textDocument":   item,
		"contentChanges": []any{map[string]any{"text": text}},
	})
}

type edLSPEdit struct {
	Range struct {
		Start edLSPPos `json:"start"`
		End   edLSPPos `json:"end"`
	} `json:"range"`
	NewText string `json:"newText"`
}

type edLSPPos struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}

func (c *edLSP) formatDocument(path, src string) (string, error) {
	if !c.ready {
		return "", fmt.Errorf("language server not ready")
	}
	uri := edDocURI(path)
	c.syncDocument(uri, src)

	raw, err := c.request("textDocument/formatting", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"options": map[string]any{
			"tabSize":      edDefaultIndent,
			"insertSpaces": true,
		},
	})
	if err != nil {
		return "", err
	}

	var edits []edLSPEdit
	if err := json.Unmarshal(raw, &edits); err != nil {
		return "", fmt.Errorf("unexpected formatting reply")
	}
	if len(edits) == 0 {
		return src, nil // already formatted
	}
	return edApplyLSPEdits(src, edits)
}

// edApplyLSPEdits applies TextEdits back-to-front so earlier ranges keep their
// original offsets. Positions are treated as rune offsets, which matches the
// protocol's UTF-16 units for the ASCII that Lua source is in practice.
func edApplyLSPEdits(src string, edits []edLSPEdit) (string, error) {
	lines := strings.Split(strings.ReplaceAll(src, "\r\n", "\n"), "\n")

	ordered := make([]edLSPEdit, len(edits))
	copy(ordered, edits)
	// Insertion sort by descending start position: edit counts are tiny.
	for i := 1; i < len(ordered); i++ {
		for j := i; j > 0 && edLSPPosLess(ordered[j-1].Range.Start, ordered[j].Range.Start); j-- {
			ordered[j-1], ordered[j] = ordered[j], ordered[j-1]
		}
	}

	for _, e := range ordered {
		s, err := edLSPOffset(lines, e.Range.Start)
		if err != nil {
			return "", err
		}
		t, err := edLSPOffset(lines, e.Range.End)
		if err != nil {
			return "", err
		}
		if s > t {
			return "", fmt.Errorf("inverted edit range")
		}
		text := strings.Join(lines, "\n")
		r := []rune(text)
		merged := string(r[:s]) + strings.ReplaceAll(e.NewText, "\r\n", "\n") + string(r[t:])
		lines = strings.Split(merged, "\n")
	}
	return strings.Join(lines, "\n"), nil
}

func edLSPPosLess(a, b edLSPPos) bool {
	if a.Line != b.Line {
		return a.Line < b.Line
	}
	return a.Character < b.Character
}

// edLSPOffset converts an LSP position to a rune offset into the joined text,
// clamping positions past the end of a line or of the document. Servers spell
// "to the end of the document" as a line one past the last, so that has to
// land on the final offset rather than fail.
func edLSPOffset(lines []string, p edLSPPos) (int, error) {
	if p.Line < 0 || p.Character < 0 {
		return 0, fmt.Errorf("negative edit position")
	}
	off := 0
	for i, ln := range lines {
		n := len([]rune(ln))
		if i == p.Line {
			return off + min(p.Character, n), nil
		}
		off += n + 1 // the newline that joins this line to the next
	}
	// Past the last line: back out the newline counted for it, which the
	// joined text does not have.
	return max(off-1, 0), nil
}

func edPathToURI(abs string) string {
	p := filepath.ToSlash(abs)
	if !strings.HasPrefix(p, "/") {
		p = "/" + p // Windows drive letters
	}
	return "file://" + (&url.URL{Path: p}).EscapedPath()
}

// ── completion, hover and signature help ──────────────────────────────────────
//
// These three back the editor's assistance popups. Each runs on a goroutine and
// hands its answer back through a channel, so the render loop never waits on
// the server.

// edCompItem is one completion candidate.
type edCompItem struct {
	label  string // shown in the list
	insert string // what actually goes into the buffer
	detail string // type or origin, shown dimmed
}

// edLSPComplete asks for the completions available at a position.
func edLSPComplete(path, src string, line, char int) ([]edCompItem, error) {
	c, err := edLSPClient()
	if err != nil {
		return nil, err
	}
	uri := edDocURI(path)
	c.syncDocument(uri, src)
	c.awaitWorkspace(uri, 20*time.Second)

	raw, err := c.request("textDocument/completion", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     map[string]any{"line": line, "character": char},
	})
	if err != nil {
		return nil, err
	}

	// The reply is either a bare item array or a CompletionList wrapping one.
	var list struct {
		Items []json.RawMessage `json:"items"`
	}
	items := []json.RawMessage{}
	if err := json.Unmarshal(raw, &list); err == nil && list.Items != nil {
		items = list.Items
	} else if err := json.Unmarshal(raw, &items); err != nil {
		return nil, fmt.Errorf("unexpected completion reply")
	}

	out := make([]edCompItem, 0, len(items))
	for _, rawItem := range items {
		var it struct {
			Label      string `json:"label"`
			InsertText string `json:"insertText"`
			Detail     string `json:"detail"`
			TextEdit   *struct {
				NewText string `json:"newText"`
			} `json:"textEdit"`
			InsertTextFormat int `json:"insertTextFormat"`
		}
		if err := json.Unmarshal(rawItem, &it); err != nil || it.Label == "" {
			continue
		}
		insert := it.Label
		switch {
		case it.TextEdit != nil && it.TextEdit.NewText != "":
			insert = it.TextEdit.NewText
		case it.InsertText != "":
			insert = it.InsertText
		}
		if it.InsertTextFormat == 2 {
			insert = edStripSnippet(insert)
		}
		out = append(out, edCompItem{label: it.Label, insert: insert, detail: it.Detail})
	}
	return out, nil
}

// edStripSnippet reduces an LSP snippet to plain text: "${1:name}" becomes
// "name" and "$0" disappears. The editor has no tab-stop machinery.
func edStripSnippet(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		if s[i] != '$' {
			if s[i] == '\\' && i+1 < len(s) {
				b.WriteByte(s[i+1])
				i += 2
				continue
			}
			b.WriteByte(s[i])
			i++
			continue
		}
		i++ // past '$'
		if i < len(s) && s[i] == '{' {
			end := strings.IndexByte(s[i:], '}')
			if end < 0 {
				break
			}
			body := s[i+1 : i+end]
			if colon := strings.IndexByte(body, ':'); colon >= 0 {
				b.WriteString(body[colon+1:])
			}
			i += end + 1
			continue
		}
		for i < len(s) && s[i] >= '0' && s[i] <= '9' { // a bare $1 placeholder
			i++
		}
	}
	return b.String()
}

// edLSPHover asks for the documentation of the symbol at a position.
func edLSPHover(path, src string, line, char int) (string, error) {
	c, err := edLSPClient()
	if err != nil {
		return "", err
	}
	uri := edDocURI(path)
	c.syncDocument(uri, src)
	c.awaitWorkspace(uri, 20*time.Second)

	raw, err := c.request("textDocument/hover", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     map[string]any{"line": line, "character": char},
	})
	if err != nil {
		return "", err
	}
	if text := edHoverText(raw); !edLSPLoading(text) {
		return text, nil
	}
	return "", nil
}

// edHoverText digs the text out of a Hover reply, which may carry a string, a
// {language,value} pair, a MarkupContent, or an array of any of those.
func edHoverText(raw json.RawMessage) string {
	var wrapper struct {
		Contents json.RawMessage `json:"contents"`
	}
	if err := json.Unmarshal(raw, &wrapper); err != nil || len(wrapper.Contents) == 0 {
		return ""
	}
	return strings.TrimSpace(edMarkupText(wrapper.Contents))
}

func edMarkupText(raw json.RawMessage) string {
	var str string
	if err := json.Unmarshal(raw, &str); err == nil {
		return str
	}
	var obj struct {
		Value string `json:"value"`
		Kind  string `json:"kind"`
	}
	if err := json.Unmarshal(raw, &obj); err == nil && obj.Value != "" {
		return obj.Value
	}
	var list []json.RawMessage
	if err := json.Unmarshal(raw, &list); err == nil {
		parts := make([]string, 0, len(list))
		for _, item := range list {
			if text := edMarkupText(item); text != "" {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, "\n")
	}
	return ""
}

// edLSPSignature asks for the signature of the call surrounding a position. It
// returns the label plus the span of the parameter being typed, so the strip
// can point at it.
func edLSPSignature(path, src string, line, char int) (label string, from, to int, err error) {
	c, err := edLSPClient()
	if err != nil {
		return "", -1, -1, err
	}
	uri := edDocURI(path)
	c.syncDocument(uri, src)
	c.awaitWorkspace(uri, 20*time.Second)

	raw, err := c.request("textDocument/signatureHelp", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     map[string]any{"line": line, "character": char},
	})
	if err != nil {
		return "", -1, -1, err
	}

	var help struct {
		Signatures []struct {
			Label      string `json:"label"`
			Parameters []struct {
				Label json.RawMessage `json:"label"`
			} `json:"parameters"`
		} `json:"signatures"`
		ActiveSignature int `json:"activeSignature"`
		ActiveParameter int `json:"activeParameter"`
	}
	if err := json.Unmarshal(raw, &help); err != nil || len(help.Signatures) == 0 {
		return "", -1, -1, nil
	}
	i := help.ActiveSignature
	if i < 0 || i >= len(help.Signatures) {
		i = 0
	}
	sig := help.Signatures[i]

	from, to = -1, -1
	if p := help.ActiveParameter; p >= 0 && p < len(sig.Parameters) {
		// A parameter label is either offsets into the signature, or the
		// parameter text itself, which then has to be located.
		var span [2]int
		var text string
		if err := json.Unmarshal(sig.Parameters[p].Label, &span); err == nil {
			from, to = span[0], span[1]
		} else if err := json.Unmarshal(sig.Parameters[p].Label, &text); err == nil && text != "" {
			if at := strings.Index(sig.Label, text); at >= 0 {
				from = len([]rune(sig.Label[:at]))
				to = from + len([]rune(text))
			}
		}
	}
	return sig.Label, from, to, nil
}

// awaitWorkspace blocks until the server has finished indexing. Formatting and
// document symbols are syntactic and answer straight away, but hover,
// completion and signature help are semantic: until the scan finishes
// lua-language-server replies to them with a "Workspace loading" placeholder.
// There is no notification for this in the protocol, so probe for it once and
// remember the answer.
func (c *edLSP) awaitWorkspace(uri string, timeout time.Duration) {
	c.mu.Lock()
	warm := c.warm
	c.mu.Unlock()
	if warm {
		return
	}

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		raw, err := c.request("textDocument/hover", map[string]any{
			"textDocument": map[string]any{"uri": uri},
			"position":     map[string]any{"line": 0, "character": 0},
		})
		if err != nil {
			return
		}
		if !edLSPLoading(edHoverText(raw)) {
			break
		}
		time.Sleep(150 * time.Millisecond)
	}

	c.mu.Lock()
	c.warm = true
	c.mu.Unlock()
}

// edLSPLoading reports whether a reply is the server's "still indexing" filler.
func edLSPLoading(text string) bool {
	return strings.HasPrefix(text, "Workspace loading")
}

// edWarmLSP starts the language server and lets it finish indexing in the
// background, so the first completion the user asks for is not the one that
// waits for the scan.
func edWarmLSP(path, src string) {
	if !edHaveLuaLSP() {
		return
	}
	go func() {
		c, err := edLSPClient()
		if err != nil {
			return
		}
		uri := edDocURI(path)
		c.syncDocument(uri, src)
		c.awaitWorkspace(uri, 30*time.Second)
	}()
}

// edLSPClient returns the shared language server, starting it on first use and
// replacing it if it has since died - a crashed server should not disable
// assistance for the rest of the session.
func edLSPClient() (*edLSP, error) {
	// The single place every request passes through, so the Options switch is
	// enforced here rather than relying on each caller to ask first.
	if !edUseLSP {
		return nil, fmt.Errorf("language server is switched off")
	}

	edLSPMu.Lock()
	defer edLSPMu.Unlock()

	if edLSPInst != nil {
		if edLSPInst.alive() {
			return edLSPInst, nil
		}
		edLSPInst.stop()
		edLSPInst = nil
	}

	c := &edLSP{
		pending: make(map[int]chan json.RawMessage),
		opened:  make(map[string]int),
	}
	if err := c.start(); err != nil {
		return nil, err
	}
	edLSPInst = c
	return c, nil
}

// edDocURI is the URI a document is known to the server by.
func edDocURI(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	return edPathToURI(abs)
}

// ── diagnostics ───────────────────────────────────────────────────────────────
//
// Syntax errors and lints are not requested: the server publishes them for a
// document whenever it has looked at it. They land here from the read loop and
// the editor picks them up on its next frame.

type edDiagnostic struct {
	line     int // 0-based
	from, to int // rune columns on that line; to is exclusive
	severity int // 1 error, 2 warning, 3 information, 4 hint
	message  string
	source   string
}

// isError reports whether a diagnostic is a hard error rather than a lint.
func (d edDiagnostic) isError() bool { return d.severity == 1 }

var edDiags struct {
	sync.Mutex
	byURI map[string][]edDiagnostic
	seq   int // bumped on every publish, so the editor can spot new ones
}

// edStoreDiagnostics decodes a publishDiagnostics notification.
func edStoreDiagnostics(params json.RawMessage) {
	var note struct {
		URI         string `json:"uri"`
		Diagnostics []struct {
			Range struct {
				Start edLSPPos `json:"start"`
				End   edLSPPos `json:"end"`
			} `json:"range"`
			Severity int             `json:"severity"`
			Message  string          `json:"message"`
			Source   string          `json:"source"`
			Code     json.RawMessage `json:"code"`
		} `json:"diagnostics"`
	}
	if err := json.Unmarshal(params, &note); err != nil || note.URI == "" {
		return
	}

	out := make([]edDiagnostic, 0, len(note.Diagnostics))
	for _, d := range note.Diagnostics {
		severity := d.Severity
		if severity == 0 {
			severity = 1 // the protocol lets it be omitted, meaning error
		}
		to := d.Range.End.Character
		if d.Range.End.Line != d.Range.Start.Line {
			to = -1 // spans lines; underline to the end of the first one
		}
		out = append(out, edDiagnostic{
			line:     d.Range.Start.Line,
			from:     d.Range.Start.Character,
			to:       to,
			severity: severity,
			message:  strings.TrimSpace(strings.ReplaceAll(d.Message, "\n", " ")),
			source:   d.Source,
		})
	}

	edDiags.Lock()
	defer edDiags.Unlock()
	if edDiags.byURI == nil {
		edDiags.byURI = map[string][]edDiagnostic{}
	}
	edDiags.byURI[note.URI] = out
	edDiags.seq++
}

// edClearDiagnostics drops everything published so far.
func edClearDiagnostics() {
	edDiags.Lock()
	defer edDiags.Unlock()
	edDiags.byURI = nil
	edDiags.seq++
}

// edDiagnosticsFor returns the diagnostics published for a path, and the
// publish counter so the caller can tell when they change.
func edDiagnosticsFor(path string) ([]edDiagnostic, int) {
	edDiags.Lock()
	defer edDiags.Unlock()
	return edDiags.byURI[edDocURI(path)], edDiags.seq
}

// edPublishSeq is how many times any document's diagnostics have been replaced.
func edPublishSeq() int {
	edDiags.Lock()
	defer edDiags.Unlock()
	return edDiags.seq
}

// edPushDocument tells the server about the current text so it can re-check it.
// Diagnostics come back on their own, as a notification.
func edPushDocument(path, src string) {
	if !edHaveLuaLSP() {
		return
	}
	go func() {
		c, err := edLSPClient()
		if err != nil {
			return
		}
		c.syncDocument(edDocURI(path), src)
	}()
}
