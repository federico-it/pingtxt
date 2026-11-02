package hub

import (
	"cmp"
	"encoding/csv"
	"encoding/json"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/federico-it/pingtxt/internal/store"
)

const (
	auditPageMax   = 200
	auditExportMax = 100_000
)

// requireAdminAny accepts an admin's session or one of their API tokens
// (read scope is enough): the audit log holds emails and addresses, so
// read-only users never see it.
func (h *Hub) requireAdminAny(fn func(http.ResponseWriter, *http.Request, *store.User)) http.HandlerFunc {
	return h.requireAuth(func(w http.ResponseWriter, r *http.Request, u *store.User) {
		if !u.IsAdmin() {
			writeErr(w, http.StatusForbidden, "only admins can do this")
			return
		}
		fn(w, r, u)
	})
}

// auditFilter reads the filters of the audit log from the query string.
func auditFilter(r *http.Request) store.AuditFilter {
	q := r.URL.Query()
	num := func(k string) int64 {
		n, _ := strconv.ParseInt(q.Get(k), 10, 64)
		return n
	}
	f := store.AuditFilter{
		Before:     num("before"),
		Since:      num("since"),
		Until:      num("until"),
		UserID:     num("user"),
		Category:   strings.TrimSpace(q.Get("category")),
		Action:     strings.TrimSpace(q.Get("action")),
		TargetType: strings.TrimSpace(q.Get("target_type")),
		TargetID:   num("target_id"),
		Problems:   q.Get("problems") == "1" || q.Get("problems") == "true",
		Query:      q.Get("q"),
		RequestID:  q.Get("request_id"),
		Limit:      int(num("limit")),
	}
	for _, s := range strings.Split(q.Get("source"), ",") {
		switch s = strings.TrimSpace(s); s {
		case store.AuditWeb, store.AuditAPI, store.AuditMCP, store.AuditApp, store.AuditSystem, store.AuditCLI:
			f.Sources = append(f.Sources, s)
		}
	}
	return f
}

func (h *Hub) listAudit(w http.ResponseWriter, r *http.Request, _ *store.User) {
	f := auditFilter(r)
	f.Limit = min(max(f.Limit, 1), auditPageMax)
	if r.URL.Query().Get("limit") == "" {
		f.Limit = 50
	}
	want := f.Limit
	f.Limit++ // one more tells whether there is a next page
	list, err := h.store.ListAudit(f)
	if err != nil {
		h.internalErr(w, err)
		return
	}
	var next int64
	if len(list) > want {
		list = list[:want]
		next = list[len(list)-1].ID
	}
	writeJSON(w, 200, map[string]any{"entries": list, "next_before": next})
}

func (h *Hub) auditUsers(w http.ResponseWriter, r *http.Request, _ *store.User) {
	list, err := h.store.AuditUsers()
	if err != nil {
		h.internalErr(w, err)
		return
	}
	writeJSON(w, 200, list)
}

// exportAudit downloads the filtered log as CSV (default) or JSON Lines.
func (h *Hub) exportAudit(w http.ResponseWriter, r *http.Request, _ *store.User) {
	f := auditFilter(r)
	f.Limit = auditExportMax
	list, err := h.store.ListAudit(f)
	if err != nil {
		h.internalErr(w, err)
		return
	}
	format := r.URL.Query().Get("format")
	auditFrom(r).Set("format", cmpOr(format, "csv"))
	auditFrom(r).Set("rows", len(list))
	name := "pingtxt-audit-" + time.Now().UTC().Format("20060102-150405")
	w.Header().Set("Cache-Control", "no-store")
	if format == "jsonl" {
		w.Header().Set("Content-Type", "application/x-ndjson")
		w.Header().Set("Content-Disposition", `attachment; filename="`+name+`.jsonl"`)
		enc := json.NewEncoder(w)
		for i := range list {
			_ = enc.Encode(&list[i])
		}
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`.csv"`)
	cw := csv.NewWriter(w)
	_ = cw.Write([]string{"id", "time", "user", "source", "credential", "via", "ip", "remote_addr", "user_agent",
		"action", "target_type", "target_id", "target_name", "result", "changes", "request_id"})
	for _, e := range list {
		_ = cw.Write([]string{
			strconv.FormatInt(e.ID, 10), time.UnixMilli(e.At).UTC().Format(time.RFC3339), csvSafe(e.UserEmail), e.Source,
			csvSafe(e.Credential), e.Via, e.IP, e.RemoteAddr, csvSafe(e.UserAgent), e.Action, e.TargetType,
			strconv.FormatInt(e.TargetID, 10), csvSafe(e.TargetName), e.Result, csvSafe(string(e.Changes)), e.RequestID,
		})
	}
	cw.Flush()
}

// csvSafe stops spreadsheets from running a cell as a formula.
func csvSafe(s string) string {
	if s != "" && strings.ContainsRune("=+-@\t\r", rune(s[0])) {
		return "'" + s
	}
	return s
}

// accountActivity lists the user's own logins, 2FA changes, tokens and app
// sign-ins of the last 30 days. Every user sees theirs, read-only ones too.
func (h *Hub) accountActivity(w http.ResponseWriter, r *http.Request, u *store.User) {
	since := time.Now().Add(-30 * 24 * time.Hour).UnixMilli()
	var out []store.AuditEntry
	for _, cat := range []string{"account", "token", "app"} {
		list, err := h.store.ListAudit(store.AuditFilter{UserID: u.ID, Category: cat, Since: since, Limit: 100})
		if err != nil {
			h.internalErr(w, err)
			return
		}
		out = append(out, list...)
	}
	// Failed logins with this account's email but no user (unknown at the time) are left out.
	slices.SortFunc(out, func(a, b store.AuditEntry) int { return cmp.Compare(b.ID, a.ID) })
	if len(out) > 100 {
		out = out[:100]
	}
	if out == nil {
		out = []store.AuditEntry{}
	}
	writeJSON(w, 200, out)
}
