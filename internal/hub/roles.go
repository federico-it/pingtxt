package hub

import (
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"golang.org/x/crypto/bcrypt"

	"github.com/federico-it/pingtxt/internal/store"
)

// Roles:
//   - admin:  changes everything (monitors, agents, settings, users...).
//   - viewer: read-only. Sees dashboards, incidents and history, can manage
//     its own password, 2FA and read tokens, but no write request goes through
//     (see requireAuth) and secrets in monitors and notification channels are hidden.

// createUser adds an account. Without a password the user logs in with single sign-on
// (an account with the same verified email is matched on first login).
func (h *Hub) createUser(w http.ResponseWriter, r *http.Request, me *store.User) {
	var in struct {
		Email    string `json:"email"`
		Password string `json:"password"`
		Role     string `json:"role"`
	}
	if readJSON(r, &in) != nil {
		writeErr(w, 400, "invalid body")
		return
	}
	in.Email = strings.ToLower(strings.TrimSpace(in.Email))
	if !strings.Contains(in.Email, "@") || len(in.Email) > 254 {
		writeErr(w, 400, "a valid email is required")
		return
	}
	if !store.ValidRole(in.Role) {
		writeErr(w, 400, "role must be admin or viewer")
		return
	}
	if in.Password != "" && len(in.Password) < 8 {
		writeErr(w, 400, "password must be at least 8 characters")
		return
	}
	if _, err := h.store.UserByEmail(in.Email); err == nil {
		writeErr(w, 409, "a user with this email already exists")
		return
	} else if !errors.Is(err, store.ErrNotFound) {
		h.internalErr(w, err)
		return
	}
	hash := ""
	if in.Password != "" {
		b, _ := bcrypt.GenerateFromPassword([]byte(in.Password), bcrypt.DefaultCost)
		hash = string(b)
	}
	u, err := h.store.CreateUser(in.Email, hash, in.Role)
	if err != nil {
		h.internalErr(w, err)
		return
	}
	h.log.Info("user created", "user", u.Email, "role", u.Role, "by", me.Email)
	writeJSON(w, 201, h.userView(u))
}

// updateUser changes the role of another user.
func (h *Hub) updateUser(w http.ResponseWriter, r *http.Request, me *store.User) {
	id, ok := pathID(r)
	if !ok {
		writeErr(w, 400, "invalid id")
		return
	}
	var in struct {
		Role string `json:"role"`
	}
	if readJSON(r, &in) != nil || !store.ValidRole(in.Role) {
		writeErr(w, 400, "role must be admin or viewer")
		return
	}
	// Admins cannot demote themselves: there is always at least one admin left.
	if id == me.ID {
		writeErr(w, 400, "you cannot change your own role")
		return
	}
	if err := h.store.SetUserRole(id, in.Role); errors.Is(err, store.ErrNotFound) {
		writeErr(w, 404, "user not found")
		return
	} else if err != nil {
		h.internalErr(w, err)
		return
	}
	u, err := h.store.UserByID(id)
	if err != nil {
		h.internalErr(w, err)
		return
	}
	h.log.Info("user role changed", "user", u.Email, "role", u.Role, "by", me.Email)
	writeJSON(w, 200, h.userView(u))
}

// MakeAdmin gives the admin role to the user with this email (recovery from the command line).
func (h *Hub) MakeAdmin(email string) error {
	u, err := h.store.UserByEmail(strings.ToLower(strings.TrimSpace(email)))
	if err != nil {
		return fmt.Errorf("user %s: %w", email, err)
	}
	return h.store.SetUserRole(u.ID, store.RoleAdmin)
}

// ---------- secrets hidden from read-only users ----------

const redacted = "********"

// Passwords in "scheme://user:pass@host" and scheme-less "user:pass@tcp(host)" targets.
// The match runs to the last "@", so a password containing "@" is hidden too.
var (
	credentialsInURL = regexp.MustCompile(`^([a-z][a-z0-9+.-]*://[^:@/\s]*:)(\S*)@`)
	credentialsInDSN = regexp.MustCompile(`^([^:@/\s]*:)(\S*)@`)
)

// redactTarget hides the password in a connection string (databases, redis, smtp...).
func redactTarget(t string) string {
	re := credentialsInDSN
	if strings.Contains(t, "://") {
		re = credentialsInURL
	}
	return re.ReplaceAllString(t, "${1}"+redacted+"@")
}

// redactMonitor removes what a read-only user should not see: credentials in
// the target, request headers and body, and the push token.
func redactMonitor(m *store.Monitor) {
	m.Target = redactTarget(m.Target)
	for k := range m.Headers {
		m.Headers[k] = redacted
	}
	if m.Body != "" {
		m.Body = redacted
	}
	m.PushToken = ""
}

// redactNotifier keeps only the channel name and type: configs hold webhook URLs and bot tokens.
func redactNotifier(n *store.Notifier) {
	n.Config = map[string]string{}
}
