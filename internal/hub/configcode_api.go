package hub

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"sigs.k8s.io/yaml"
	yaml3 "sigs.k8s.io/yaml/goyaml.v3"

	"github.com/federico-it/pingtxt/internal/store"
)

// ---------- YAML output ----------

// keyOrder puts the identifying keys first; the rest follow alphabetically.
var keyOrder = []string{"version", "slug", "name", "title", "type", "target", "channel", "parent", "group", "tags",
	"locations", "interval_sec", "description", "kind", "config", "settings", "groups", "channels", "monitors",
	"maintenances", "status_pages"}

func keyRank(k string) int {
	if i := slices.Index(keyOrder, k); i >= 0 {
		return i
	}
	return len(keyOrder)
}

// yamlNode builds the YAML of a decoded JSON value with keys in a readable
// order and short lists of names on one line.
func yamlNode(v any) *yaml3.Node {
	switch x := v.(type) {
	case map[string]any:
		n := &yaml3.Node{Kind: yaml3.MappingNode}
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Slice(keys, func(i, j int) bool {
			ri, rj := keyRank(keys[i]), keyRank(keys[j])
			if ri != rj {
				return ri < rj
			}
			return keys[i] < keys[j]
		})
		for _, k := range keys {
			n.Content = append(n.Content, &yaml3.Node{Kind: yaml3.ScalarNode, Value: k}, yamlNode(x[k]))
		}
		return n
	case []any:
		n := &yaml3.Node{Kind: yaml3.SequenceNode}
		flow, width := true, 0
		for _, e := range x {
			c := yamlNode(e)
			if c.Kind != yaml3.ScalarNode {
				flow = false
			}
			width += len(c.Value) + 2
			n.Content = append(n.Content, c)
		}
		if flow && width < 70 {
			n.Style = yaml3.FlowStyle
		}
		return n
	}
	n := &yaml3.Node{}
	if err := n.Encode(v); err != nil {
		n = &yaml3.Node{Kind: yaml3.ScalarNode, Value: fmt.Sprint(v)}
	}
	// Whole numbers come from JSON as floats: write 60, not 6e+01.
	if f, ok := v.(float64); ok && f == float64(int64(f)) {
		n = &yaml3.Node{Kind: yaml3.ScalarNode, Tag: "!!int", Value: fmt.Sprint(int64(f))}
	}
	return n
}

// MarshalCode writes a code document as YAML, with a header comment.
func MarshalCode(d *CodeDoc, header string) ([]byte, error) {
	root := toObj(d)
	n := &yaml3.Node{Kind: yaml3.MappingNode}
	for _, k := range []string{"version", "settings", "groups", "tags", "channels", "monitors", "maintenances", "status_pages"} {
		if v, ok := root[k]; ok {
			n.Content = append(n.Content, &yaml3.Node{Kind: yaml3.ScalarNode, Value: k}, yamlNode(v))
		}
	}
	if header != "" {
		n.HeadComment = header
	}
	var b strings.Builder
	enc := yaml3.NewEncoder(&b)
	enc.SetIndent(2)
	if err := enc.Encode(n); err != nil {
		return nil, err
	}
	enc.Close()
	return []byte(b.String()), nil
}

// ExportCode is the configuration of the hub as code. Settings only when
// asked for; secrets only with secrets.
func (h *Hub) ExportCode(secrets, settings bool) (*CodeDoc, error) {
	exp, err := h.ExportConfig(secrets)
	if err != nil {
		return nil, err
	}
	c := h.ToCode(exp)
	if settings && exp.Settings != nil {
		c.Settings = map[string]json.RawMessage{}
		add := func(k string, v any) {
			b, _ := json.Marshal(stripZero(toObj(v)))
			c.Settings[k] = b
		}
		add("general", exp.Settings.General)
		add("agent_updates", exp.Settings.AgentUpdates)
		add("backups", exp.Settings.Backups)
	}
	return c, nil
}

// ---------- API ----------

const maxCodeBody = 16 << 20

func (h *Hub) readCode(w http.ResponseWriter, r *http.Request) (*CodeDoc, bool) {
	b, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxCodeBody))
	if err != nil {
		writeErr(w, 400, "invalid body")
		return nil, false
	}
	doc, err := ParseCode(b)
	if err != nil {
		writeErr(w, 400, err.Error())
		return nil, false
	}
	return doc, true
}

// getCode serves the configuration as YAML. ?secrets=1 (browser session
// only) includes passwords and tokens, ?settings=1 the instance settings.
func (h *Hub) getCode(w http.ResponseWriter, r *http.Request, u *store.User) {
	q := r.URL.Query()
	secrets := q.Get("secrets") == "1" || q.Get("secrets") == "true"
	if !secrets {
		auditFrom(r).Skip()
	} else if _, tok, _ := h.authenticate(r); tok != nil {
		writeErr(w, http.StatusForbidden, "API tokens cannot export secrets, log in from the browser")
		return
	}
	doc, err := h.ExportCode(secrets, q.Get("settings") == "1" || q.Get("settings") == "true")
	if err != nil {
		h.internalErr(w, err)
		return
	}
	header := "pingtxt configuration as code, exported " + time.Now().UTC().Format(time.RFC3339) +
		"\nApply it with POST /api/config/code/apply (see https://pingtxt.com/docs/administration#configuration-as-code)."
	if !secrets {
		header += "\nSecrets are left out: the hub keeps the ones it has."
	}
	b, err := MarshalCode(doc, header)
	if err != nil {
		h.internalErr(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if q.Get("download") == "1" {
		w.Header().Set("Content-Disposition", `attachment; filename="pingtxt.yaml"`)
	}
	w.Write(b)
}

// planCodeAPI answers what applying the body would change, without changing
// anything. Read-only API tokens of admins can call it (CI checks).
func (h *Hub) planCodeAPI(w http.ResponseWriter, r *http.Request, u *store.User) {
	doc, ok := h.readCode(w, r)
	if !ok {
		return
	}
	plan, err := h.PlanCode(doc, h.codeOpts(r))
	if err != nil {
		h.internalErr(w, err)
		return
	}
	writeJSON(w, 200, plan)
}

// applyCodeAPI makes the hub match the body. 422 with the plan when the
// file has errors; nothing is changed then.
func (h *Hub) applyCodeAPI(w http.ResponseWriter, r *http.Request, u *store.User) {
	doc, ok := h.readCode(w, r)
	if !ok {
		return
	}
	if h.codeSync.managed() {
		writeErr(w, http.StatusConflict, "this hub is managed by its configuration file ("+h.codeSync.path+"): change the file instead")
		return
	}
	res, err := h.ApplyCode(doc, h.codeOpts(r))
	if err != nil {
		if res != nil && res.Import != nil && res.Import.Backup != "" {
			writeErr(w, 500, "apply failed: "+err.Error()+". restore "+res.Import.Backup+" to undo the partial change")
			return
		}
		h.internalErr(w, err)
		return
	}
	if len(res.Plan.Errors) > 0 {
		writeJSON(w, http.StatusUnprocessableEntity, res)
		return
	}
	writeJSON(w, 200, res)
}

func (h *Hub) codeOpts(r *http.Request) CodeOptions {
	q := r.URL.Query()
	_, tok, _ := h.authenticate(r)
	return CodeOptions{
		Prune:     q.Get("prune") == "1" || q.Get("prune") == "true",
		AllowAuth: tok == nil,
		Host:      r.Host,
	}
}

// requireAdminRead lets admins in with a session or any API token, also a
// read-only one: for requests that change nothing (a plan is a POST).
func (h *Hub) requireAdminRead(fn func(http.ResponseWriter, *http.Request, *store.User)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u, tok, err := h.authenticate(r)
		if err != nil {
			writeErr(w, http.StatusUnauthorized, err.Error())
			return
		}
		if !u.IsAdmin() {
			h.auditDenied(r, u, tok)
			writeErr(w, http.StatusForbidden, "only admins can do this")
			return
		}
		h.audited(w, r, u, tok, func(w http.ResponseWriter, r *http.Request) { fn(w, r, u) })
	}
}

// ---------- the hub's own configuration file ----------

// codeSync applies the file named by PINGTXT_CONFIG_FILE at start and every
// time it changes.
type codeSync struct {
	path  string
	prune bool

	mu      sync.Mutex
	hash    string
	status  CodeSyncStatus
	checked time.Time
}

// CodeSyncStatus is the state of the configuration file, for the UI.
type CodeSyncStatus struct {
	Managed   bool      `json:"managed"`
	File      string    `json:"file,omitempty"`
	Prune     bool      `json:"prune"`
	AppliedAt int64     `json:"applied_at,omitempty"` // last apply that changed or confirmed the hub (ms)
	CheckedAt int64     `json:"checked_at,omitempty"`
	Error     string    `json:"error,omitempty"`
	Plan      *CodePlan `json:"plan,omitempty"` // of the last apply
}

func (s *codeSync) managed() bool { return s != nil && s.path != "" }

const codeSyncEvery = 30 * time.Second

// envRef matches ${NAME} in the configuration file.
var envRef = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

// expandEnv replaces ${NAME} with the environment variable NAME; a variable
// that is not set is an error, so a secret is never applied empty.
func expandEnv(b []byte) ([]byte, error) {
	var missing []string
	out := envRef.ReplaceAllFunc(b, func(m []byte) []byte {
		name := string(envRef.FindSubmatch(m)[1])
		v, ok := os.LookupEnv(name)
		if !ok {
			missing = append(missing, name)
		}
		return []byte(v)
	})
	if len(missing) > 0 {
		slices.Sort(missing)
		return nil, fmt.Errorf("environment variables not set: %s", strings.Join(slices.Compact(missing), ", "))
	}
	return out, nil
}

// syncCodeFile applies the configuration file when its content changed since
// the last time (or force).
func (h *Hub) syncCodeFile(force bool) {
	s := h.codeSync
	if !s.managed() {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	s.status.CheckedAt = now.UnixMilli()
	raw, err := os.ReadFile(s.path)
	if err != nil {
		s.fail(h, "read: "+err.Error())
		return
	}
	b, err := expandEnv(raw)
	if err != nil {
		s.fail(h, err.Error())
		return
	}
	sum := sha256.Sum256(b)
	hash := hex.EncodeToString(sum[:])
	if hash == s.hash && !force {
		return
	}
	doc, err := ParseCode(b)
	if err != nil {
		s.hash = hash // do not retry the same broken file every 30 seconds
		s.fail(h, err.Error())
		return
	}
	res, err := h.ApplyCode(doc, CodeOptions{Prune: s.prune, AllowAuth: true, Host: hostOf(h.generalSettings().PublicURL)})
	if err != nil {
		s.fail(h, "apply: "+err.Error())
		return
	}
	s.hash = hash
	s.status.Plan = res.Plan
	if len(res.Plan.Errors) > 0 {
		s.status.Error = "the file has errors, nothing was changed: " + strings.Join(res.Plan.Errors, "; ")
		h.log.Error("configuration file not applied", "file", s.path, "errors", res.Plan.Errors)
		return
	}
	s.status.Error = ""
	s.status.AppliedAt = now.UnixMilli()
	if len(res.Plan.Changes) > 0 {
		h.log.Info("configuration file applied", "file", s.path, "changes", len(res.Plan.Changes))
		h.AuditSystem(store.AuditSystem, "config.code_sync", "config", 0, s.path, map[string]any{"changes": res.Plan.Changes})
	}
}

func (s *codeSync) fail(h *Hub, msg string) {
	s.status.Error = msg
	h.log.Error("configuration file", "file", s.path, "err", msg)
}

func hostOf(u string) string {
	if _, rest, ok := strings.Cut(u, "://"); ok {
		u = rest
	}
	host, _, _ := strings.Cut(u, "/")
	return host
}

func (h *Hub) codeSyncLoop(ctx context.Context) {
	if !h.codeSync.managed() {
		return
	}
	h.syncCodeFile(true)
	t := time.NewTicker(codeSyncEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			h.syncCodeFile(false)
		}
	}
}

func (h *Hub) getCodeSync(w http.ResponseWriter, r *http.Request, _ *store.User) {
	s := h.codeSync
	if !s.managed() {
		writeJSON(w, 200, CodeSyncStatus{})
		return
	}
	s.mu.Lock()
	st := s.status
	s.mu.Unlock()
	st.Managed, st.File, st.Prune = true, s.path, s.prune
	writeJSON(w, 200, st)
}

// postCodeSync applies the configuration file now, also when it did not
// change (to undo changes made in the UI).
func (h *Hub) postCodeSync(w http.ResponseWriter, r *http.Request, u *store.User) {
	if !h.codeSync.managed() {
		writeErr(w, 409, "no configuration file: set PINGTXT_CONFIG_FILE")
		return
	}
	h.syncCodeFile(true)
	h.getCodeSync(w, r, u)
}

// ---------- MCP ----------

// codeCall posts the "yaml" argument to a configuration as code endpoint.
func (c *mcpCall) codeCall(path string) (any, error) {
	j, err := yaml.YAMLToJSON([]byte(c.str("yaml")))
	if err != nil {
		return nil, fmt.Errorf("invalid YAML: %w", err)
	}
	if p, _ := c.boolean("prune"); p {
		path += "?prune=1"
	}
	return c.do("POST", path, json.RawMessage(j))
}

// text calls a REST endpoint that answers with text (YAML).
func (c *mcpCall) text(path string) (string, error) {
	req, err := http.NewRequestWithContext(c.ctx, "GET", "http://"+c.caller.host+path, http.NoBody)
	if err != nil {
		return "", err
	}
	req.Host = c.caller.host
	req.RemoteAddr = c.caller.remote
	req.Header.Set("Authorization", c.caller.auth)
	rec := &apiRecorder{header: http.Header{}}
	c.api.ServeHTTP(rec, req)
	if rec.code >= 400 {
		var e struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(rec.body.Bytes(), &e) == nil && e.Error != "" {
			return "", errors.New(e.Error)
		}
		return "", fmt.Errorf("API error %d", rec.code)
	}
	return rec.body.String(), nil
}
