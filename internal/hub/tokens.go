package hub

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/federico-it/pingtxt/internal/store"
)

// Personal API tokens let scripts call the API with "Authorization: Bearer ptk_...".
//
// Scopes:
//   - read:  GET requests only (monitors, incidents, agents, notifiers...)
//   - write: everything a token can reach, including creating and deleting.
//
// A token never has more rights than its owner: the owner's role is checked
// at every request, so the write token of a user who becomes read-only only reads.
//
// Tokens never reach account endpoints (password, 2FA, tokens, users and
// instance settings): those need a browser session, so a leaked token cannot
// lock the owner out or mint more tokens.
const (
	ScopeRead  = "read"
	ScopeWrite = "write"

	apiTokenPrefix = "ptk_"
	maxTokens      = 50
)

// authenticate finds the user of a request: an API token when an
// Authorization header is present, the session cookie otherwise.
func (h *Hub) authenticate(r *http.Request) (*store.User, *store.APIToken, error) {
	if v := r.Header.Get("Authorization"); v != "" {
		t := bearer(r)
		if !strings.HasPrefix(t, apiTokenPrefix) {
			return nil, nil, errors.New("invalid token")
		}
		tok, err := h.store.APITokenByHash(HashToken(t))
		if err != nil {
			return nil, nil, errors.New("invalid token")
		}
		now := time.Now().UnixMilli()
		if tok.ExpiresAt > 0 && tok.ExpiresAt <= now {
			return nil, nil, errors.New("token expired")
		}
		u, err := h.store.UserByID(tok.UserID)
		if err != nil {
			return nil, nil, errors.New("invalid token")
		}
		if now-tok.LastUsedAt > time.Minute.Milliseconds() {
			_ = h.store.TouchAPIToken(tok.ID, now)
		}
		return u, tok, nil
	}
	u := h.currentUser(r)
	if u == nil {
		return nil, nil, errors.New("unauthorized")
	}
	return u, nil, nil
}

// requireAuth accepts a session or an API token with a scope that allows the request.
func (h *Hub) requireAuth(fn func(http.ResponseWriter, *http.Request, *store.User)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u, tok, err := h.authenticate(r)
		if err != nil {
			writeErr(w, http.StatusUnauthorized, err.Error())
			return
		}
		if !readOnlyMethod(r.Method) {
			if tok != nil && tok.Scope != ScopeWrite {
				writeErr(w, http.StatusForbidden, "this token is read-only")
				return
			}
			if !u.IsAdmin() {
				writeErr(w, http.StatusForbidden, errReadOnlyUser)
				return
			}
		}
		fn(w, r, u)
	}
}

const errReadOnlyUser = "your account is read-only, ask an admin for changes"

func readOnlyMethod(m string) bool { return m == http.MethodGet || m == http.MethodHead }

// requireSession accepts only a browser session: used for account and instance settings.
func (h *Hub) requireSession(fn func(http.ResponseWriter, *http.Request, *store.User)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u, tok, err := h.authenticate(r)
		if err != nil {
			writeErr(w, http.StatusUnauthorized, err.Error())
			return
		}
		if tok != nil {
			writeErr(w, http.StatusForbidden, "API tokens cannot manage accounts or settings, log in from the browser")
			return
		}
		fn(w, r, u)
	}
}

// requireAdmin accepts only a browser session of an admin: instance settings and users.
func (h *Hub) requireAdmin(fn func(http.ResponseWriter, *http.Request, *store.User)) http.HandlerFunc {
	return h.requireSession(func(w http.ResponseWriter, r *http.Request, u *store.User) {
		if !u.IsAdmin() {
			writeErr(w, http.StatusForbidden, "only admins can do this")
			return
		}
		fn(w, r, u)
	})
}

func (h *Hub) listTokens(w http.ResponseWriter, r *http.Request, u *store.User) {
	list, err := h.store.ListAPITokens(u.ID)
	if err != nil {
		h.internalErr(w, err)
		return
	}
	writeJSON(w, 200, list)
}

func (h *Hub) createToken(w http.ResponseWriter, r *http.Request, u *store.User) {
	var in struct {
		Name          string `json:"name"`
		Scope         string `json:"scope"`
		ExpiresInDays int    `json:"expires_in_days"`
	}
	if readJSON(r, &in) != nil {
		writeErr(w, 400, "invalid body")
		return
	}
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" || len(in.Name) > 100 {
		writeErr(w, 400, "name is required (max 100 characters)")
		return
	}
	if in.Scope != ScopeRead && in.Scope != ScopeWrite {
		writeErr(w, 400, "scope must be read or write")
		return
	}
	if in.Scope == ScopeWrite && !u.IsAdmin() {
		writeErr(w, 400, "read-only users can only create read tokens")
		return
	}
	if in.ExpiresInDays < 0 || in.ExpiresInDays > 3650 {
		writeErr(w, 400, "expiry must be between 0 (never) and 3650 days")
		return
	}
	list, err := h.store.ListAPITokens(u.ID)
	if err != nil {
		h.internalErr(w, err)
		return
	}
	if len(list) >= maxTokens {
		writeErr(w, 400, "too many tokens, revoke some first")
		return
	}
	secret, hash := NewToken(apiTokenPrefix)
	t := &store.APIToken{UserID: u.ID, Name: in.Name, TokenHash: hash, Hint: secret[len(secret)-4:], Scope: in.Scope}
	if in.ExpiresInDays > 0 {
		t.ExpiresAt = time.Now().Add(time.Duration(in.ExpiresInDays) * 24 * time.Hour).UnixMilli()
	}
	if err := h.store.CreateAPIToken(t); err != nil {
		h.internalErr(w, err)
		return
	}
	// The secret is shown once: only its hash is stored.
	writeJSON(w, 201, map[string]any{"token": t, "secret": secret})
}

func (h *Hub) deleteToken(w http.ResponseWriter, r *http.Request, u *store.User) {
	id, ok := pathID(r)
	if !ok {
		writeErr(w, 400, "invalid id")
		return
	}
	if err := h.store.DeleteAPIToken(u.ID, id); errors.Is(err, store.ErrNotFound) {
		writeErr(w, 404, "token not found")
		return
	} else if err != nil {
		h.internalErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}
