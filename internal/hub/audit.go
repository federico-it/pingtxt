package hub

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net"
	"net/http"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/federico-it/pingtxt/internal/store"
)

// The audit log records who changed what, when and from where.
//
// Every write request that goes through requireAuth, requireSession or
// requireAdmin is matched against auditRoutes: the route says which action
// it is and how to load the object it changes, so the entry gets the fields
// that changed (before -> after) without code in each handler. MCP tools
// call the REST API in-process, so they are covered too and say which tool
// made the call. Logins, OAuth consents, the scheduler and the command line
// add their entries directly (auditEvent, AuditSystem).
//
// Secrets never reach the log: values of secret fields are compared through
// an HMAC with a key that lives only in memory, and shown as "********".

const (
	auditMaxChanges = 16 << 10 // bytes of JSON per entry
	auditMaxBody    = 64 << 10 // response bytes kept to find a new object's id
)

// ---------- request context ----------

type auditCtxKey struct{}
type mcpCtxKey struct{}

// mcpOrigin is set on the in-process API calls of an MCP tool.
type mcpOrigin struct {
	tool      string
	ip        string
	remote    string
	userAgent string
}

func withMCPOrigin(ctx context.Context, o mcpOrigin) context.Context {
	return context.WithValue(ctx, mcpCtxKey{}, o)
}

// auditRec is the entry being built for one request. Handlers reach it with
// auditFrom(r) to add details; all methods are safe on nil (request not audited).
type auditRec struct {
	entry  store.AuditEntry
	skip   bool
	extra  map[string]any
	items  []store.AuditEntry
	action string // overrides the route's action
}

func auditFrom(r *http.Request) *auditRec {
	rec, _ := r.Context().Value(auditCtxKey{}).(*auditRec)
	return rec
}

// Skip drops the entry (e.g. an export without secrets).
func (a *auditRec) Skip() {
	if a != nil {
		a.skip = true
	}
}

// Set adds a value to the entry's changes, under "_<key>".
func (a *auditRec) Set(key string, v any) {
	if a == nil {
		return
	}
	if a.extra == nil {
		a.extra = map[string]any{}
	}
	a.extra[key] = v
}

// Action replaces the route's action (e.g. monitor.resume).
func (a *auditRec) Action(action string) {
	if a != nil {
		a.action = action
	}
}

// Item adds one entry per object of a bulk request. When a request has
// items, they replace its own entry.
func (a *auditRec) Item(action, targetType string, id int64, name string, changes map[string]any) {
	if a == nil {
		return
	}
	e := a.entry
	e.Action, e.TargetType, e.TargetID, e.TargetName = action, targetType, id, name
	e.Changes = auditJSON(changes)
	a.items = append(a.items, e)
}

// ---------- routes ----------

// auditLoader returns an object (and its name) to compare before and after a
// request. id is the path id, or the id of the object the request created.
type auditLoader func(h *Hub, r *http.Request, u *store.User, id int64) (name string, v any, ok bool)

type auditSpec struct {
	action    string      // <object>.<verb>; "" = not audited
	typ       string      // target type
	load      auditLoader // nil = no diff
	idParam   string      // path value with the target id (default "id")
	nameParam string      // path value used as the target name
	singleton bool        // settings: load without an id
	response  bool        // keep the JSON reply in the changes (imports)
}

func (s auditSpec) deletes() bool {
	return strings.HasSuffix(s.action, ".delete") || s.action == "token.revoke" || s.action == "app.revoke"
}

var skipAudit = auditSpec{}

// auditRoutes maps each write route (and a few sensitive reads) to its
// action. TestAuditCoversWriteRoutes fails when a write route is missing.
var auditRoutes = map[string]auditSpec{
	// account
	"POST /api/auth/password":                {action: "account.password_change", typ: "user"},
	"DELETE /api/auth/identities/{provider}": {action: "account.sso_unlink", typ: "user", nameParam: "provider"},
	"POST /api/auth/2fa/setup":               skipAudit, // nothing changes until enable
	"POST /api/auth/2fa/enable":              {action: "account.2fa_enable", typ: "user"},
	"POST /api/auth/2fa/disable":             {action: "account.2fa_disable", typ: "user"},
	"POST /api/auth/2fa/recovery-codes":      {action: "account.recovery_codes", typ: "user"},
	"POST /api/tokens":                       {action: "token.create", typ: "token", load: loadToken},
	"DELETE /api/tokens/{id}":                {action: "token.revoke", typ: "token", load: loadToken},
	"DELETE /api/oauth/grants/{id}":          {action: "app.revoke", typ: "app", load: loadGrant},
	// users and settings
	"POST /api/users":            {action: "user.create", typ: "user", load: loadUser},
	"PUT /api/users/{id}":        {action: "user.update", typ: "user", load: loadUser},
	"DELETE /api/users/{id}":     {action: "user.delete", typ: "user", load: loadUser},
	"DELETE /api/users/{id}/2fa": {action: "user.2fa_reset", typ: "user", load: loadUser},
	"PUT /api/settings/general": {action: "settings.general", typ: "settings", singleton: true, load: func(h *Hub, _ *http.Request, _ *store.User, _ int64) (string, any, bool) {
		return "general", h.generalSettings(), true
	}},
	"PUT /api/settings/auth": {action: "settings.auth", typ: "settings", singleton: true, load: func(h *Hub, _ *http.Request, _ *store.User, _ int64) (string, any, bool) {
		return "auth", h.authSettings(), true
	}},
	"PUT /api/settings/backups": {action: "settings.backups", typ: "settings", singleton: true, load: func(h *Hub, _ *http.Request, _ *store.User, _ int64) (string, any, bool) {
		return "backups", h.backupSettings(), true
	}},
	"PUT /api/settings/push":       {action: "settings.push", typ: "settings", singleton: true, load: loadPushSettings},
	"POST /api/settings/push/test": {action: "settings.push_test", typ: "settings", nameParam: ""},
	"PUT /api/settings/agent-updates": {action: "settings.agent_updates", typ: "settings", singleton: true, load: func(h *Hub, _ *http.Request, _ *store.User, _ int64) (string, any, bool) {
		return "agent updates", h.agentUpdateSettings(), true
	}},
	"PUT /api/telemetry": {action: "settings.telemetry", typ: "settings", singleton: true, load: func(h *Hub, _ *http.Request, _ *store.User, _ int64) (string, any, bool) {
		return "telemetry", h.telemetrySettings(), true
	}},
	"PUT /api/reports": {action: "settings.reports", typ: "settings", singleton: true, load: func(h *Hub, _ *http.Request, _ *store.User, _ int64) (string, any, bool) {
		return "reports", h.reportSettings(), true
	}},
	"POST /api/reports/send":  {action: "report.send", typ: "report"},
	"POST /api/version/check": skipAudit,
	// data
	"POST /api/backups":                {action: "backup.create", typ: "backup", response: true},
	"POST /api/backups/upload":         {action: "backup.upload", typ: "backup", response: true},
	"DELETE /api/backups/restore":      {action: "backup.restore_cancel", typ: "backup"},
	"GET /api/backups/{name}":          {action: "backup.download", typ: "backup", nameParam: "name"},
	"DELETE /api/backups/{name}":       {action: "backup.delete", typ: "backup", nameParam: "name"},
	"POST /api/backups/{name}/restore": {action: "backup.restore", typ: "backup", nameParam: "name"},
	"GET /api/config/export":           {action: "config.export", typ: "config"}, // logged only with secrets
	"POST /api/config/import":          {action: "config.import", typ: "config", response: true},
	"POST /api/import/uptime-kuma":     {action: "config.import_kuma", typ: "config", response: true},
	"GET /api/audit/export":            {action: "audit.export", typ: "audit"},
	// monitors
	"POST /api/monitors":                 {action: "monitor.create", typ: "monitor", load: loadMonitor},
	"POST /api/monitors/test":            skipAudit,
	"POST /api/monitors/bulk":            {action: "monitor.bulk", typ: "monitor"},
	"PUT /api/monitors/{id}":             {action: "monitor.update", typ: "monitor", load: loadMonitor},
	"DELETE /api/monitors/{id}":          {action: "monitor.delete", typ: "monitor", load: loadMonitor},
	"POST /api/monitors/{id}/pause":      {action: "monitor.pause", typ: "monitor", load: loadMonitor},
	"POST /api/monitors/{id}/test":       skipAudit,
	"POST /api/monitors/{id}/push-token": {action: "monitor.push_token_rotate", typ: "monitor", load: loadMonitor},
	"POST /api/monitors/{id}/expiry":     skipAudit, // runs a check, changes nothing
	"POST /api/monitors/{id}/check":      skipAudit, // runs the checks now, changes nothing
	"POST /api/incidents/{id}/ack":       {action: "incident.ack", typ: "incident", load: loadIncident},
	"DELETE /api/incidents/{id}/ack":     {action: "incident.unack", typ: "incident", load: loadIncident},
	"POST /api/maintenances":             {action: "maintenance.create", typ: "maintenance", load: loadMaintenance},
	"PUT /api/maintenances/{id}":         {action: "maintenance.update", typ: "maintenance", load: loadMaintenance},
	"DELETE /api/maintenances/{id}":      {action: "maintenance.delete", typ: "maintenance", load: loadMaintenance},
	"POST /api/groups":                   {action: "group.create", typ: "group", load: loadGroup},
	"PUT /api/groups/{id}":               {action: "group.update", typ: "group", load: loadGroup},
	"DELETE /api/groups/{id}":            {action: "group.delete", typ: "group", load: loadGroup},
	"PUT /api/groups/{id}/monitors":      {action: "group.set_monitors", typ: "group", load: loadGroupMonitors},
	"POST /api/tags":                     {action: "tag.create", typ: "tag", load: loadTag},
	"PUT /api/tags/{id}":                 {action: "tag.update", typ: "tag", load: loadTag},
	"DELETE /api/tags/{id}":              {action: "tag.delete", typ: "tag", load: loadTag},
	// agents
	"POST /api/agents":     {action: "agent.create", typ: "agent", load: loadAgent},
	"PUT /api/agents/{id}": {action: "agent.update", typ: "agent", load: loadAgent},
	"PUT /api/agents/local": {action: "agent.local_update", typ: "settings", singleton: true, load: func(h *Hub, _ *http.Request, _ *store.User, _ int64) (string, any, bool) {
		return "local", h.localAgentView(0), true
	}},
	"DELETE /api/agents/{id}":        {action: "agent.delete", typ: "agent", load: loadAgent},
	"POST /api/agents/{id}/token":    {action: "agent.token_rotate", typ: "agent", load: loadAgent},
	"POST /api/agents/{id}/update":   {action: "agent.update_now", typ: "agent", load: loadAgent},
	"POST /api/agents/{id}/monitors": {action: "agent.assign", typ: "agent", load: loadAgent, response: true},
	// notification channels
	"POST /api/notifiers":           {action: "notifier.create", typ: "notifier", load: loadNotifier},
	"PUT /api/notifiers/{id}":       {action: "notifier.update", typ: "notifier", load: loadNotifier},
	"DELETE /api/notifiers/{id}":    {action: "notifier.delete", typ: "notifier", load: loadNotifier},
	"POST /api/notifiers/test":      skipAudit, // unsaved draft
	"POST /api/notifiers/{id}/test": {action: "notifier.test", typ: "notifier", load: loadNotifierName},
	// status pages
	"POST /api/status-pages":                                              {action: "page.create", typ: "page", load: loadPage},
	"POST /api/status-pages/preview":                                      skipAudit,
	"PUT /api/status-pages/{id}":                                          {action: "page.update", typ: "page", load: loadPage},
	"DELETE /api/status-pages/{id}":                                       {action: "page.delete", typ: "page", load: loadPage},
	"POST /api/status-pages/{id}/logo":                                    {action: "page.logo_upload", typ: "page", load: loadPageName},
	"POST /api/status-pages/{id}/logo/url":                                {action: "page.logo_upload", typ: "page", load: loadPageName},
	"DELETE /api/status-pages/{id}/logo":                                  {action: "page.logo_delete", typ: "page", load: loadPageName},
	"POST /api/status-pages/{id}/incidents":                               {action: "page_incident.create", typ: "page_incident", load: loadPageIncident, idParam: "incident"},
	"PUT /api/status-pages/{id}/incidents/{incident}":                     {action: "page_incident.update", typ: "page_incident", load: loadPageIncident, idParam: "incident"},
	"DELETE /api/status-pages/{id}/incidents/{incident}":                  {action: "page_incident.delete", typ: "page_incident", load: loadPageIncident, idParam: "incident"},
	"POST /api/status-pages/{id}/incidents/{incident}/updates":            {action: "page_incident.post_update", typ: "page_incident", load: loadPageIncident, idParam: "incident", response: true},
	"DELETE /api/status-pages/{id}/incidents/{incident}/updates/{update}": {action: "page_incident.delete_update", typ: "page_incident", load: loadPageIncident, idParam: "incident"},
	"GET /api/status-pages/{id}/subscribers":                              {action: "page.subscribers_view", typ: "page", load: loadPageName},
	"DELETE /api/status-pages/{id}/subscribers/{subscriber}":              {action: "page.subscriber_delete", typ: "page", load: loadPageName},
	// the app's own phone and Live Activity tokens change often and say
	// nothing about the configuration: not logged.
}

// ---------- loaders ----------

func loadMonitor(h *Hub, _ *http.Request, _ *store.User, id int64) (string, any, bool) {
	m, err := h.store.Monitor(id)
	if err != nil {
		return "", nil, false
	}
	return m.Name, m, true
}

func loadIncident(h *Hub, _ *http.Request, _ *store.User, id int64) (string, any, bool) {
	i, err := h.store.Incident(id)
	if err != nil {
		return "", nil, false
	}
	return i.MonitorName, map[string]any{"acked_by": i.AckedBy, "acked_at": i.AckedAt}, true
}

func loadMaintenance(h *Hub, _ *http.Request, _ *store.User, id int64) (string, any, bool) {
	m, err := h.store.Maintenance(id)
	if err != nil {
		return "", nil, false
	}
	return m.Name, m, true
}

func loadGroup(h *Hub, _ *http.Request, _ *store.User, id int64) (string, any, bool) {
	g, err := h.store.Group(id)
	if err != nil {
		return "", nil, false
	}
	return g.Name, g, true
}

// loadGroupMonitors compares the monitors of a group.
func loadGroupMonitors(h *Hub, _ *http.Request, _ *store.User, id int64) (string, any, bool) {
	g, err := h.store.Group(id)
	if err != nil {
		return "", nil, false
	}
	list, err := h.store.MonitorsInGroups([]int64{id})
	if err != nil {
		return "", nil, false
	}
	names := make([]string, 0, len(list))
	for _, m := range list {
		names = append(names, m.Name)
	}
	sort.Strings(names)
	return g.Name, map[string]any{"monitors": names}, true
}

func loadTag(h *Hub, _ *http.Request, _ *store.User, id int64) (string, any, bool) {
	t, err := h.store.Tag(id)
	if err != nil {
		return "", nil, false
	}
	return t.Name, t, true
}

func loadAgent(h *Hub, _ *http.Request, _ *store.User, id int64) (string, any, bool) {
	a, err := h.store.Agent(id)
	if err != nil {
		return "", nil, false
	}
	return agentLabel(a), a, true
}

func loadNotifier(h *Hub, _ *http.Request, _ *store.User, id int64) (string, any, bool) {
	n, err := h.store.Notifier(id)
	if err != nil {
		return "", nil, false
	}
	return n.Name, n, true
}

func loadNotifierName(h *Hub, r *http.Request, u *store.User, id int64) (string, any, bool) {
	name, _, ok := loadNotifier(h, r, u, id)
	return name, nil, ok
}

func loadPage(h *Hub, _ *http.Request, _ *store.User, id int64) (string, any, bool) {
	p, err := h.store.StatusPage(id)
	if err != nil {
		return "", nil, false
	}
	return p.Title, p, true
}

func loadPageName(h *Hub, r *http.Request, u *store.User, id int64) (string, any, bool) {
	name, _, ok := loadPage(h, r, u, id)
	return name, nil, ok
}

func loadPageIncident(h *Hub, r *http.Request, _ *store.User, id int64) (string, any, bool) {
	pageID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		return "", nil, false
	}
	i, err := h.store.PageIncident(pageID, id)
	if err != nil {
		return "", nil, false
	}
	v := *i
	v.Updates = nil
	return i.Title, v, true
}

func loadUser(h *Hub, _ *http.Request, _ *store.User, id int64) (string, any, bool) {
	u, err := h.store.UserByID(id)
	if err != nil {
		return "", nil, false
	}
	return u.Email, map[string]any{"email": u.Email, "role": u.Role, "two_factor": h.store.TwoFactorEnabled(u.ID)}, true
}

func loadToken(h *Hub, _ *http.Request, u *store.User, id int64) (string, any, bool) {
	list, err := h.store.ListAPITokens(u.ID)
	if err != nil {
		return "", nil, false
	}
	for _, t := range list {
		if t.ID == id {
			return t.Name + " …" + t.Hint, map[string]any{"name": t.Name, "scope": t.Scope, "expires_at": t.ExpiresAt}, true
		}
	}
	return "", nil, false
}

func loadGrant(h *Hub, _ *http.Request, u *store.User, id int64) (string, any, bool) {
	list, err := h.store.ListOAuthGrants(u.ID)
	if err != nil {
		return "", nil, false
	}
	for _, g := range list {
		if g.ID == id {
			return g.ClientName, map[string]any{"client": g.ClientName, "scope": g.Scope}, true
		}
	}
	return "", nil, false
}

func loadPushSettings(h *Hub, _ *http.Request, _ *store.User, _ int64) (string, any, bool) {
	v, err := h.pushSettingsView()
	if err != nil {
		return "", nil, false
	}
	return "push", map[string]any{"enabled": v.Enabled, "relay_url": v.RelayURL}, true
}

// ---------- recording ----------

// audited runs fn for an authenticated request and records it when the
// route is in auditRoutes and the request is a write or a sensitive read.
func (h *Hub) audited(w http.ResponseWriter, r *http.Request, u *store.User, tok *store.APIToken, fn func(http.ResponseWriter, *http.Request)) {
	spec, ok := auditRoutes[r.Pattern]
	if !ok {
		if readOnlyMethod(r.Method) {
			fn(w, r)
			return
		}
		// A write route nobody added to auditRoutes: still logged.
		spec = auditSpec{action: "request", typ: ""}
	}
	if spec.action == "" {
		fn(w, r)
		return
	}
	rec := &auditRec{entry: h.auditActor(r, u, tok)}
	rec.entry.Action = spec.action
	rec.entry.TargetType = spec.typ
	if spec.action == "request" {
		rec.entry.TargetName = r.Pattern
		if rec.entry.TargetName == "" {
			rec.entry.TargetName = r.Method + " " + r.URL.Path
		}
	}
	if spec.typ == "user" && spec.load == nil {
		rec.entry.TargetID, rec.entry.TargetName = u.ID, u.Email
	}
	if spec.nameParam != "" {
		rec.entry.TargetName = r.PathValue(spec.nameParam)
	}
	param := spec.idParam
	if param == "" {
		param = "id"
	}
	id, _ := strconv.ParseInt(r.PathValue(param), 10, 64)
	var before any
	if spec.load != nil && (id > 0 || spec.singleton) {
		if name, v, ok := spec.load(h, r, u, id); ok {
			rec.entry.TargetID, rec.entry.TargetName, before = id, name, v
		}
	}

	sw := &auditWriter{ResponseWriter: w, keep: spec.response || (spec.load != nil && id == 0 && !spec.singleton)}
	fn(sw, r.WithContext(context.WithValue(r.Context(), auditCtxKey{}, rec)))
	if rec.skip {
		return
	}
	code := sw.code
	if code == 0 {
		code = http.StatusOK
	}
	switch {
	case code == http.StatusForbidden:
		rec.entry.Result = store.AuditDenied
	case code >= 400:
		return // invalid input or an error: nothing changed
	default:
		rec.entry.Result = store.AuditOK
	}
	if rec.action != "" {
		rec.entry.Action = rec.action
	}

	changes := map[string]any{}
	if rec.entry.Result == store.AuditOK {
		if id == 0 && sw.keep && !spec.singleton {
			id = responseID(sw.body.Bytes())
		}
		var after any
		if spec.load != nil && !spec.deletes() && (id > 0 || spec.singleton) {
			if name, v, ok := spec.load(h, r, u, id); ok {
				rec.entry.TargetID, rec.entry.TargetName, after = id, name, v
			}
		}
		if before != nil || after != nil {
			changes = auditDiff(before, after)
		}
		if rec.entry.Action == "monitor.pause" {
			if m, ok := after.(*store.Monitor); ok && !m.Paused {
				rec.entry.Action = "monitor.resume"
			}
		}
		if spec.response && sw.body.Len() > 0 {
			var v any
			if json.Unmarshal(sw.body.Bytes(), &v) == nil {
				changes["_result"] = redactValue(v)
			}
		}
	}
	for k, v := range rec.extra {
		changes["_"+k] = v
	}
	rec.entry.Changes = auditJSON(changes)
	if len(rec.items) > 0 {
		for i := range rec.items {
			rec.items[i].Result = rec.entry.Result
			h.saveAudit(&rec.items[i])
		}
		return
	}
	h.saveAudit(&rec.entry)
}

// auditDenied records a write refused by the auth middleware (read-only
// user, read token, token on a browser-only route).
func (h *Hub) auditDenied(r *http.Request, u *store.User, tok *store.APIToken) {
	spec, ok := auditRoutes[r.Pattern]
	if readOnlyMethod(r.Method) && !ok {
		return
	}
	if ok && spec.action == "" {
		return
	}
	e := h.auditActor(r, u, tok)
	e.Action, e.TargetType, e.Result = spec.action, spec.typ, store.AuditDenied
	if !ok {
		e.Action, e.TargetName = "request", r.Pattern
	}
	if id, err := strconv.ParseInt(r.PathValue(cmpOr(spec.idParam, "id")), 10, 64); err == nil && spec.load != nil {
		if name, _, ok := spec.load(h, r, u, id); ok {
			e.TargetID, e.TargetName = id, name
		}
	}
	h.saveAudit(&e)
}

func cmpOr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// auditMCPDenied records a write tool refused to a read-only caller.
func (h *Hub) auditMCPDenied(c *mcpCaller, tool string) {
	r, _ := http.NewRequestWithContext(withMCPOrigin(context.Background(), mcpOrigin{tool: tool, ip: c.ip, remote: c.remote, userAgent: c.agent}), http.MethodPost, "/mcp", nil)
	e := h.auditActor(r, c.user, c.token)
	e.Action, e.Result = "mcp.tool", store.AuditDenied
	h.saveAudit(&e)
}

// auditEvent records an event outside the API wrappers (logins, consents).
func (h *Hub) auditEvent(r *http.Request, u *store.User, tok *store.APIToken, action, result string, changes map[string]any) {
	e := h.auditActor(r, u, tok)
	e.Action, e.Result = action, result
	if u != nil {
		e.TargetType, e.TargetID, e.TargetName = "user", u.ID, u.Email
	}
	e.Changes = auditJSON(changes)
	h.saveAudit(&e)
}

// auditLogin records a login attempt. u is nil when the email is unknown.
func (h *Hub) auditLogin(r *http.Request, u *store.User, email, method, result string) {
	e := h.auditActor(r, u, nil)
	e.Action, e.Result = "account.login", result
	if u == nil {
		e.UserEmail = email
	} else {
		e.TargetType, e.TargetID, e.TargetName = "user", u.ID, u.Email
	}
	e.Changes = auditJSON(map[string]any{"_method": method})
	h.saveAudit(&e)
}

// auditGrant records an OAuth sign-in of an app (MCP client or the iOS app)
// or its end, as done by the app itself.
func (h *Hub) auditGrant(r *http.Request, g *store.OAuthGrant, action string) {
	u, err := h.store.UserByID(g.UserID)
	if err != nil {
		return
	}
	name := g.ClientName
	if name == "" {
		if c, err := h.store.OAuthClient(g.ClientID); err == nil {
			name = c.Name
		}
	}
	tok := &store.APIToken{Name: name, GrantID: max(g.ID, 1), Native: isNativeApp(g.ClientID), Scope: g.Scope}
	e := h.auditActor(r, u, tok)
	e.Action, e.Result = action, store.AuditOK
	e.TargetType, e.TargetID, e.TargetName = "app", g.ID, name
	e.Changes = auditJSON(map[string]any{"_scope": g.Scope})
	h.saveAudit(&e)
}

// AuditSystem records something the hub did by itself (scheduled backups,
// automatic agent updates) or a command line recovery flag.
func (h *Hub) AuditSystem(source, action, targetType string, targetID int64, targetName string, changes map[string]any) {
	extra := make(map[string]any, len(changes))
	for k, v := range changes {
		extra["_"+k] = v
	}
	e := store.AuditEntry{
		Source: source, Action: action, Result: store.AuditOK,
		TargetType: targetType, TargetID: targetID, TargetName: targetName,
		Changes: auditJSON(extra), RequestID: newRequestID(),
	}
	h.saveAudit(&e)
}

func (h *Hub) saveAudit(e *store.AuditEntry) {
	if e.At == 0 {
		e.At = time.Now().UnixMilli()
	}
	if e.RequestID == "" {
		e.RequestID = newRequestID()
	}
	if err := h.store.AddAudit(e); err != nil {
		h.log.Error("audit log", "action", e.Action, "err", err)
	}
}

// auditActor fills who made a request and from where.
func (h *Hub) auditActor(r *http.Request, u *store.User, tok *store.APIToken) store.AuditEntry {
	e := store.AuditEntry{
		Source:     store.AuditWeb,
		IP:         requestIP(r),
		RemoteAddr: r.RemoteAddr,
		UserAgent:  r.UserAgent(),
		RequestID:  newRequestID(),
	}
	if u != nil {
		e.UserID, e.UserEmail = u.ID, u.Email
	}
	if tok != nil {
		switch {
		case tok.GrantID > 0 && tok.Native:
			e.Source = store.AuditApp
		case tok.GrantID > 0:
			e.Source = store.AuditMCP
		default:
			e.Source = store.AuditAPI
		}
		e.Credential = tok.Name
		if tok.Hint != "" {
			e.Credential += " …" + tok.Hint
		}
	}
	if o, ok := r.Context().Value(mcpCtxKey{}).(mcpOrigin); ok {
		if e.Source == store.AuditAPI {
			e.Source = store.AuditMCP
		}
		e.Via, e.IP, e.RemoteAddr, e.UserAgent = o.tool, o.ip, o.remote, o.userAgent
	}
	// One id for every entry of a request, also across MCP batches.
	if rec := auditFrom(r); rec != nil {
		e.RequestID = rec.entry.RequestID
	}
	return e
}

func newRequestID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return "r_" + hex.EncodeToString(b)
}

// auditWriter captures the status and, when asked, the start of the body.
type auditWriter struct {
	http.ResponseWriter
	code int
	keep bool
	body bytes.Buffer
}

func (w *auditWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (w *auditWriter) WriteHeader(code int) {
	if w.code == 0 {
		w.code = code
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *auditWriter) Write(b []byte) (int, error) {
	if w.code == 0 {
		w.code = http.StatusOK
	}
	if w.keep && w.body.Len() < auditMaxBody {
		w.body.Write(b[:min(len(b), auditMaxBody-w.body.Len())])
	}
	return w.ResponseWriter.Write(b)
}

// responseID finds the id of the object a create request made: "id" at the
// top of the reply, or in an object inside it ({"agent": {...}, "secret": ...}).
func responseID(b []byte) int64 {
	var m map[string]any
	if json.Unmarshal(b, &m) != nil {
		return 0
	}
	if id, ok := m["id"].(float64); ok {
		return int64(id)
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if o, ok := m[k].(map[string]any); ok {
			if id, ok := o["id"].(float64); ok {
				return int64(id)
			}
		}
	}
	return 0
}

// ---------- client address ----------

// Headers proxies put the client address in, in the order they are read.
var clientIPHeaders = []string{"CF-Connecting-IP", "True-Client-IP", "X-Real-IP", "X-Forwarded-For", "Forwarded"}

var forwardedForRe = regexp.MustCompile(`(?i)for="?\[?([^\]",;]+)`)

// requestIP is the first public address found in the proxy headers, or the
// address of the connection. Headers can be forged by the client, so the
// entry keeps the connection address too (RemoteAddr).
func requestIP(r *http.Request) string {
	for _, name := range clientIPHeaders {
		for _, v := range r.Header.Values(name) {
			var cands []string
			if name == "Forwarded" {
				for _, m := range forwardedForRe.FindAllStringSubmatch(v, -1) {
					cands = append(cands, m[1])
				}
			} else {
				cands = strings.Split(v, ",")
			}
			for _, c := range cands {
				if ip := parseHostIP(c); ip != nil && isPublicIP(ip) {
					return ip.String()
				}
			}
		}
	}
	if ip := parseHostIP(r.RemoteAddr); ip != nil {
		return ip.String()
	}
	return r.RemoteAddr
}

// parseHostIP reads "1.2.3.4", "1.2.3.4:5678", "[2001:db8::1]:443" or "2001:db8::1".
func parseHostIP(s string) net.IP {
	s = strings.TrimSpace(s)
	if ip := net.ParseIP(strings.Trim(s, "[]")); ip != nil {
		return ip
	}
	if host, _, err := net.SplitHostPort(s); err == nil {
		return net.ParseIP(host)
	}
	return nil
}

func isPublicIP(ip net.IP) bool { return ip.IsGlobalUnicast() && publicIP(ip) }

// ---------- diff and secrets ----------

var auditKey = func() []byte {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return b
}()

const secretMark = "\x00secret:"

// A mark as it appears in encoded JSON (json.Marshal escapes the NUL).
var secretMarkRe = regexp.MustCompile(`\\u0000secret:[0-9a-f]{16}`)

// fingerprint stands in for a secret: equal secrets give equal marks, so a
// diff tells whether one changed, but the value cannot be recovered (the key
// is random and never stored).
func fingerprint(v string) string {
	if v == "" {
		return ""
	}
	m := hmac.New(sha256.New, auditKey)
	m.Write([]byte(v))
	return secretMark + hex.EncodeToString(m.Sum(nil))[:16]
}

// Field names whose value is a credential, at any depth.
var auditSecretKeyRe = regexp.MustCompile(`(?i)(^|_)(password|passwd|secret|token|api_key|apikey|private_key|hash|bot_token)(_|$)`)

// Fields that change on their own (status, last seen...): never compared.
var auditVolatile = map[string]bool{
	"status": true, "status_since": true, "last_seen": true, "last_seen_at": true, "last_used_at": true,
	"updated_at": true, "created_at": true, "version": true, "hostname": true, "platform": true,
	"remote_addr": true, "ipv6": true, "online": true, "connected": true, "id": true,
}

// auditTree turns a value into JSON-like maps with secrets replaced by marks.
func auditTree(v any) map[string]any {
	if v == nil {
		return nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	var m map[string]any
	if json.Unmarshal(b, &m) != nil {
		return nil
	}
	typ, _ := m["type"].(string)
	redactMap(m, typ, "")
	return m
}

func redactMap(m map[string]any, typ, parent string) {
	for k, v := range m {
		switch x := v.(type) {
		case string:
			switch {
			case auditSecretKeyRe.MatchString(k):
				m[k] = fingerprint(x)
			case parent == "" && k == "body" && x != "":
				m[k] = fingerprint(x) // request bodies of monitors often carry credentials
			case parent == "" && k == "target":
				m[k] = markTarget(x)
			case parent == "headers" && secretHeaderRe.MatchString(k):
				m[k] = fingerprint(x)
			case parent == "config" && isNotifierSecret(typ, k):
				m[k] = fingerprint(x)
			}
		case map[string]any:
			redactMap(x, typ, k)
		}
	}
}

func isNotifierSecret(typ, key string) bool {
	for _, s := range notifierSecrets[typ] {
		if s == key {
			return true
		}
	}
	return false
}

// markTarget replaces the password in a connection string with its mark.
func markTarget(t string) string {
	re := credentialsInDSN
	if strings.Contains(t, "://") {
		re = credentialsInURL
	}
	if m := re.FindStringSubmatchIndex(t); m != nil {
		return t[:m[4]] + fingerprint(t[m[4]:m[5]]) + t[m[5]:]
	}
	return t
}

// redactValue hides secret fields of a reply kept in the log.
func redactValue(v any) any {
	switch x := v.(type) {
	case map[string]any:
		redactMap(x, "", "_")
		return x
	case []any:
		for i := range x {
			x[i] = redactValue(x[i])
		}
	}
	return v
}

// auditDiff returns {"field": [before, after]} for the fields that differ.
// Nested objects are flattened with dots ("config.url"); lists compare whole.
// A created object (before nil) lists its non-empty fields, a deleted one
// (after nil) the fields it had.
func auditDiff(before, after any) map[string]any {
	b, a := flatten(auditTree(before)), flatten(auditTree(after))
	out := map[string]any{}
	for k, bv := range b {
		av, ok := a[k]
		if after == nil {
			if !isEmpty(bv) {
				out[k] = []any{bv, nil}
			}
			continue
		}
		if !ok || !reflect.DeepEqual(av, bv) {
			out[k] = []any{bv, av}
		}
	}
	for k, av := range a {
		if _, ok := b[k]; ok {
			continue
		}
		if before == nil && isEmpty(av) {
			continue
		}
		out[k] = []any{nil, av}
	}
	return out
}

func flatten(m map[string]any) map[string]any {
	out := map[string]any{}
	var walk func(prefix string, m map[string]any)
	walk = func(prefix string, m map[string]any) {
		for k, v := range m {
			if prefix == "" && auditVolatile[k] {
				continue
			}
			key := prefix + k
			if sub, ok := v.(map[string]any); ok && len(sub) > 0 && k != "headers" {
				walk(key+".", sub)
				continue
			}
			out[key] = v
		}
	}
	walk("", m)
	return out
}

func isEmpty(v any) bool {
	switch x := v.(type) {
	case nil:
		return true
	case string:
		return x == ""
	case float64:
		return x == 0
	case bool:
		return !x
	case []any:
		return len(x) == 0
	case map[string]any:
		return len(x) == 0
	}
	return false
}

// auditJSON encodes changes for storage: secret marks become "********",
// and an oversized diff keeps short values only.
func auditJSON(changes map[string]any) json.RawMessage {
	if len(changes) == 0 {
		return nil
	}
	b, err := json.Marshal(changes)
	if err != nil {
		return nil
	}
	b = secretMarkRe.ReplaceAll(b, []byte(redacted))
	if len(b) <= auditMaxChanges {
		return b
	}
	short := map[string]any{}
	for k, v := range changes {
		short[k] = shorten(v)
	}
	if b2, err := json.Marshal(short); err == nil {
		b2 = secretMarkRe.ReplaceAll(b2, []byte(redacted))
		if len(b2) <= auditMaxChanges {
			return b2
		}
	}
	keys := make([]string, 0, len(changes))
	for k := range changes {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	b3, _ := json.Marshal(map[string]any{"_truncated": true, "_fields": keys})
	return b3
}

func shorten(v any) any {
	switch x := v.(type) {
	case string:
		if len(x) > 200 {
			return x[:200] + "…"
		}
	case []any:
		out := make([]any, len(x))
		for i := range x {
			out[i] = shorten(x[i])
		}
		if len(out) > 20 {
			return append(out[:20], "…")
		}
		return out
	case map[string]any:
		b, _ := json.Marshal(x)
		if len(b) > 400 {
			return "{…}"
		}
	}
	return v
}
