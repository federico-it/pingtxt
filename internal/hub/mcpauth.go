package hub

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"html/template"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/federico-it/pingtxt/internal/store"
)

// OAuth 2.1 authorization for the MCP server and the native apps.
//
// MCP clients (Claude, Cursor...) follow the MCP authorization spec: they
// discover the hub from a 401 on /mcp, register themselves (dynamic client
// registration), send the user to the consent page with PKCE, and get an
// access token plus a refresh token.
//
// The iOS app is a built-in public client (client_id "pingtxt-ios", redirect
// pingtxt://oauth): it skips registration and runs the same flow from
// ASWebAuthenticationSession. The user signs in on the hub's own login page,
// so passwords, 2FA and single sign-on all work.
//
// The hub is both the authorization server and the resource server. Access
// tokens work on /mcp and on the REST API like API tokens: they never have
// more rights than their user (scope "read" or "write", capped by the user's
// role at every request) and never reach account endpoints. Refresh tokens
// rotate at every use; presenting an old one again revokes the grant.

const (
	oauthAuthorizePath = "/oauth/authorize"
	oauthTokenPath     = "/oauth/token"
	oauthRegisterPath  = "/oauth/register"
	oauthRevokePath    = "/oauth/revoke"

	oauthAccessPrefix  = "pma_"
	oauthRefreshPrefix = "pmr_"

	oauthAccessTTL  = time.Hour
	oauthRefreshTTL = 30 * 24 * time.Hour // renewed at every refresh
	oauthCodeTTL    = 5 * time.Minute
	oauthConsentTTL = 10 * time.Minute
	// Registration is open: clients nobody authorized are removed after a day.
	oauthUnusedClientTTL = 24 * time.Hour
	maxRedirectURIs      = 10
)

var oauthScopes = []string{ScopeRead, ScopeWrite}

// Built-in clients: native apps with a fixed client_id, created at startup
// and never cleaned up.
const (
	iosClientID    = "pingtxt-ios"
	iosRedirectURI = "pingtxt://oauth"
)

var builtinOAuthClients = []*store.OAuthClient{
	{ID: iosClientID, Name: "Pingtxt for iOS", RedirectURIs: []string{iosRedirectURI}},
}

func builtinClientIDs() []string {
	ids := make([]string, len(builtinOAuthClients))
	for i, c := range builtinOAuthClients {
		ids[i] = c.ID
	}
	return ids
}

// isNativeApp reports whether a client is one of the hub's own apps.
func isNativeApp(clientID string) bool { return slices.Contains(builtinClientIDs(), clientID) }

// ensureOAuthClients registers the built-in clients.
func (h *Hub) ensureOAuthClients() {
	for _, c := range builtinOAuthClients {
		if err := h.store.EnsureOAuthClient(&store.OAuthClient{ID: c.ID, Name: c.Name, RedirectURIs: c.RedirectURIs}); err != nil {
			h.log.Error("register oauth client", "client", c.ID, "err", err)
		}
	}
}

// oauthPending holds consent requests and authorization codes in memory:
// both live for minutes and are single use.
type oauthPending struct {
	mu       sync.Mutex
	consents map[string]*consentRequest
	codes    map[string]*authCode // by hash of the code
}

type consentRequest struct {
	userID      int64
	clientID    string
	redirectURI string
	state       string
	challenge   string
	scope       string
	expires     time.Time
}

type authCode struct {
	userID      int64
	clientID    string
	redirectURI string
	challenge   string
	scope       string
	expires     time.Time
}

func (p *oauthPending) putConsent(id string, c *consentRequest) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.consents == nil {
		p.consents = map[string]*consentRequest{}
	}
	now := time.Now()
	for k, v := range p.consents {
		if now.After(v.expires) {
			delete(p.consents, k)
		}
	}
	p.consents[id] = c
}

func (p *oauthPending) takeConsent(id string) (*consentRequest, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	c, ok := p.consents[id]
	delete(p.consents, id)
	if !ok || time.Now().After(c.expires) {
		return nil, false
	}
	return c, true
}

func (p *oauthPending) putCode(hash string, c *authCode) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.codes == nil {
		p.codes = map[string]*authCode{}
	}
	now := time.Now()
	for k, v := range p.codes {
		if now.After(v.expires) {
			delete(p.codes, k)
		}
	}
	p.codes[hash] = c
}

func (p *oauthPending) takeCode(hash string) (*authCode, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	c, ok := p.codes[hash]
	delete(p.codes, hash)
	if !ok || time.Now().After(c.expires) {
		return nil, false
	}
	return c, true
}

// ---------- discovery ----------

// issuer is the public base URL of the hub, without a trailing slash.
func (h *Hub) issuer(r *http.Request) string {
	return strings.TrimRight(h.hubURL(r), "/")
}

func (h *Hub) resourceMetadataURL(r *http.Request) string {
	return h.issuer(r) + "/.well-known/oauth-protected-resource" + mcpPath
}

// mcpUnauthorized answers a request to /mcp without a valid token, pointing
// the client to the OAuth metadata (RFC 9728).
func (h *Hub) mcpUnauthorized(w http.ResponseWriter, r *http.Request, msg string, badToken bool) {
	v := `Bearer resource_metadata="` + h.resourceMetadataURL(r) + `", scope="read write"`
	if badToken {
		v += `, error="invalid_token"`
	}
	w.Header().Set("WWW-Authenticate", v)
	writeErr(w, http.StatusUnauthorized, msg)
}

// allowCORS lets browser-based MCP clients reach the OAuth endpoints. None
// of them use cookies, so any origin is fine.
func allowCORS(w http.ResponseWriter, r *http.Request) bool {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	if r.Method == http.MethodOptions {
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, MCP-Protocol-Version")
		w.Header().Set("Access-Control-Max-Age", "86400")
		w.WriteHeader(http.StatusNoContent)
		return true
	}
	return false
}

func (h *Hub) protectedResourceMetadata(w http.ResponseWriter, r *http.Request) {
	if allowCORS(w, r) {
		return
	}
	base := h.issuer(r)
	resource := base + mcpPath
	if strings.TrimRight(strings.TrimPrefix(r.URL.Path, "/.well-known/oauth-protected-resource"), "/") == "/api" {
		resource = base + "/api"
	}
	writeJSON(w, 200, map[string]any{
		"resource":                 resource,
		"resource_name":            "pingtxt",
		"authorization_servers":    []string{base},
		"scopes_supported":         oauthScopes,
		"bearer_methods_supported": []string{"header"},
	})
}

func (h *Hub) authorizationServerMetadata(w http.ResponseWriter, r *http.Request) {
	if allowCORS(w, r) {
		return
	}
	base := h.issuer(r)
	writeJSON(w, 200, map[string]any{
		"issuer":                                         base,
		"authorization_endpoint":                         base + oauthAuthorizePath,
		"token_endpoint":                                 base + oauthTokenPath,
		"registration_endpoint":                          base + oauthRegisterPath,
		"revocation_endpoint":                            base + oauthRevokePath,
		"scopes_supported":                               oauthScopes,
		"response_types_supported":                       []string{"code"},
		"response_modes_supported":                       []string{"query"},
		"grant_types_supported":                          []string{"authorization_code", "refresh_token"},
		"code_challenge_methods_supported":               []string{"S256"},
		"token_endpoint_auth_methods_supported":          []string{"none"},
		"revocation_endpoint_auth_methods_supported":     []string{"none"},
		"authorization_response_iss_parameter_supported": true,
	})
}

// ---------- dynamic client registration (RFC 7591) ----------

var uriSchemeRe = regexp.MustCompile(`^[a-z][a-z0-9+.-]*$`)

// validRedirectURI accepts https URLs, http on loopback (native apps,
// RFC 8252) and private-use schemes such as cursor:// or vscode://.
func validRedirectURI(s string) bool {
	u, err := url.Parse(s)
	if err != nil || u.Fragment != "" || len(s) > 2000 {
		return false
	}
	switch u.Scheme {
	case "https":
		return u.Host != ""
	case "http":
		return isLoopback(u.Hostname())
	case "", "javascript", "data", "file", "vbscript", "blob":
		return false
	}
	return uriSchemeRe.MatchString(u.Scheme)
}

// redirectAllowed matches a redirect URI against the registered ones. For
// loopback http URIs the port may differ (RFC 8252 section 7.3).
func redirectAllowed(c *store.OAuthClient, uri string) bool {
	if slices.Contains(c.RedirectURIs, uri) {
		return true
	}
	u, err := url.Parse(uri)
	if err != nil || u.Scheme != "http" || !isLoopback(u.Hostname()) {
		return false
	}
	for _, reg := range c.RedirectURIs {
		r, err := url.Parse(reg)
		if err == nil && r.Scheme == "http" && r.Hostname() == u.Hostname() && r.Path == u.Path && r.RawQuery == u.RawQuery {
			return true
		}
	}
	return false
}

func oauthErr(w http.ResponseWriter, code int, kind, desc string) {
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, code, map[string]string{"error": kind, "error_description": desc})
}

func (h *Hub) registerClient(w http.ResponseWriter, r *http.Request) {
	if allowCORS(w, r) {
		return
	}
	var in struct {
		RedirectURIs            []string `json:"redirect_uris"`
		ClientName              string   `json:"client_name"`
		TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
		GrantTypes              []string `json:"grant_types"`
		ResponseTypes           []string `json:"response_types"`
	}
	if readJSON(r, &in) != nil {
		oauthErr(w, 400, "invalid_client_metadata", "invalid JSON body")
		return
	}
	if len(in.RedirectURIs) == 0 || len(in.RedirectURIs) > maxRedirectURIs {
		oauthErr(w, 400, "invalid_redirect_uri", "give 1 to 10 redirect_uris")
		return
	}
	for _, u := range in.RedirectURIs {
		if !validRedirectURI(u) {
			oauthErr(w, 400, "invalid_redirect_uri", "redirect URIs must be https, http on localhost, or an app scheme: "+u)
			return
		}
	}
	for _, g := range in.GrantTypes {
		if g != "authorization_code" && g != "refresh_token" {
			oauthErr(w, 400, "invalid_client_metadata", "unsupported grant type "+g)
			return
		}
	}
	for _, t := range in.ResponseTypes {
		if t != "code" {
			oauthErr(w, 400, "invalid_client_metadata", "unsupported response type "+t)
			return
		}
	}
	name := strings.TrimSpace(in.ClientName)
	if name == "" {
		name = "MCP client"
	}
	if len(name) > 100 {
		name = name[:100]
	}
	id, _ := NewToken("pmc_")
	c := &store.OAuthClient{ID: id, Name: name, RedirectURIs: in.RedirectURIs}
	if err := h.store.CreateOAuthClient(c); err != nil {
		h.internalErr(w, err)
		return
	}
	// Clients are public (no secret): PKCE protects the code.
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, 201, map[string]any{
		"client_id":                  c.ID,
		"client_id_issued_at":        c.CreatedAt / 1000,
		"client_name":                c.Name,
		"redirect_uris":              c.RedirectURIs,
		"grant_types":                []string{"authorization_code", "refresh_token"},
		"response_types":             []string{"code"},
		"token_endpoint_auth_method": "none",
	})
}

// ---------- authorization and consent ----------

// authorize shows the consent page (GET) or records the user's answer (POST).
func (h *Hub) authorize(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; frame-ancestors 'none'")
	if r.Method == http.MethodPost {
		h.authorizeAnswer(w, r)
		return
	}
	q := r.URL.Query()
	client, err := h.store.OAuthClient(q.Get("client_id"))
	if err != nil {
		consentError(w, http.StatusBadRequest, "Unknown application. Remove the connection from your assistant and add it again.")
		return
	}
	redirectURI := q.Get("redirect_uri")
	if len(client.RedirectURIs) == 1 && redirectURI == "" {
		redirectURI = client.RedirectURIs[0]
	}
	if !redirectAllowed(client, redirectURI) {
		// Never redirect to an address the client did not register.
		consentError(w, http.StatusBadRequest, "The redirect address does not match the application's registration.")
		return
	}
	fail := func(kind, desc string) {
		redirectWith(w, r, redirectURI, url.Values{"error": {kind}, "error_description": {desc}, "state": {q.Get("state")}, "iss": {h.issuer(r)}})
	}
	if q.Get("response_type") != "code" {
		fail("unsupported_response_type", "only response_type=code is supported")
		return
	}
	challenge := q.Get("code_challenge")
	if challenge == "" || q.Get("code_challenge_method") != "S256" {
		fail("invalid_request", "PKCE with code_challenge_method=S256 is required")
		return
	}
	if res := q.Get("resource"); res != "" {
		// Tokens work on the MCP server and the REST API alike.
		u, err := url.Parse(res)
		if err != nil || !slices.Contains([]string{"", strings.Trim(mcpPath, "/"), "api"}, strings.Trim(u.Path, "/")) {
			fail("invalid_target", "this server only issues tokens for "+h.issuer(r)+mcpPath+" and "+h.issuer(r)+"/api")
			return
		}
	}
	scope := ScopeRead
	for _, s := range strings.Fields(q.Get("scope")) {
		switch s {
		case ScopeWrite:
			scope = ScopeWrite
		case ScopeRead:
		default:
			// Unknown scopes (e.g. "openid") are ignored, not fatal.
		}
	}

	u := h.currentUser(r)
	if u == nil {
		http.Redirect(w, r, "/login?next="+url.QueryEscape(r.URL.RequestURI()), http.StatusFound)
		return
	}
	if !u.IsAdmin() {
		scope = ScopeRead
	}
	id, _ := NewToken("")
	h.oauth.putConsent(id, &consentRequest{
		userID: u.ID, clientID: client.ID, redirectURI: redirectURI, state: q.Get("state"),
		challenge: challenge, scope: scope, expires: time.Now().Add(oauthConsentTTL),
	})
	redirectHost := redirectURI
	if p, err := url.Parse(redirectURI); err == nil {
		redirectHost = p.Scheme + "://" + p.Host
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = consentPage.Execute(w, map[string]any{
		"Client": client.Name, "Redirect": redirectHost, "Email": u.Email, "Request": id,
		"Write": scope == ScopeWrite, "Admin": u.IsAdmin(), "Native": isNativeApp(client.ID),
	})
}

func (h *Hub) authorizeAnswer(w http.ResponseWriter, r *http.Request) {
	if o := r.Header.Get("Origin"); o != "" && o != "null" {
		if u, err := url.Parse(o); err != nil || u.Host != r.Host {
			consentError(w, http.StatusForbidden, "Cross-site request refused.")
			return
		}
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<14)
	if err := r.ParseForm(); err != nil {
		consentError(w, http.StatusBadRequest, "Invalid form.")
		return
	}
	req, ok := h.oauth.takeConsent(r.PostForm.Get("request"))
	u := h.currentUser(r)
	if !ok || u == nil || u.ID != req.userID {
		consentError(w, http.StatusBadRequest, "This request expired. Start the connection again from your assistant.")
		return
	}
	iss := h.issuer(r)
	if r.PostForm.Get("action") != "allow" {
		redirectWith(w, r, req.redirectURI, url.Values{"error": {"access_denied"}, "error_description": {"the user denied access"}, "state": {req.state}, "iss": {iss}})
		return
	}
	scope := ScopeRead
	if r.PostForm.Get("scope") == ScopeWrite && u.IsAdmin() {
		scope = ScopeWrite
	}
	code, hash := NewToken("")
	h.oauth.putCode(hash, &authCode{
		userID: u.ID, clientID: req.clientID, redirectURI: req.redirectURI,
		challenge: req.challenge, scope: scope, expires: time.Now().Add(oauthCodeTTL),
	})
	h.log.Info("oauth app authorized", "user", u.Email, "client", req.clientID, "scope", scope)
	redirectWith(w, r, req.redirectURI, url.Values{"code": {code}, "state": {req.state}, "iss": {iss}})
}

func redirectWith(w http.ResponseWriter, r *http.Request, uri string, v url.Values) {
	u, _ := url.Parse(uri)
	q := u.Query()
	for k, vals := range v {
		if len(vals) > 0 && vals[0] != "" {
			q.Set(k, vals[0])
		}
	}
	u.RawQuery = q.Encode()
	http.Redirect(w, r, u.String(), http.StatusFound)
}

// ---------- tokens ----------

func (h *Hub) tokenEndpoint(w http.ResponseWriter, r *http.Request) {
	if allowCORS(w, r) {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<16)
	if err := r.ParseForm(); err != nil {
		oauthErr(w, 400, "invalid_request", "send an application/x-www-form-urlencoded body")
		return
	}
	f := r.PostForm
	switch f.Get("grant_type") {
	case "authorization_code":
		c, ok := h.oauth.takeCode(HashToken(f.Get("code")))
		if !ok {
			oauthErr(w, 400, "invalid_grant", "unknown or expired authorization code")
			return
		}
		if c.clientID != f.Get("client_id") {
			oauthErr(w, 400, "invalid_grant", "the code was issued to another client")
			return
		}
		if ru := f.Get("redirect_uri"); ru != "" && ru != c.redirectURI {
			oauthErr(w, 400, "invalid_grant", "redirect_uri does not match")
			return
		}
		sum := sha256.Sum256([]byte(f.Get("code_verifier")))
		if subtle.ConstantTimeCompare([]byte(base64.RawURLEncoding.EncodeToString(sum[:])), []byte(c.challenge)) != 1 {
			oauthErr(w, 400, "invalid_grant", "PKCE verification failed")
			return
		}
		if _, err := h.store.UserByID(c.userID); err != nil {
			oauthErr(w, 400, "invalid_grant", "the user no longer exists")
			return
		}
		access, accessHash := NewToken(oauthAccessPrefix)
		refresh, refreshHash := NewToken(oauthRefreshPrefix)
		now := time.Now()
		g := &store.OAuthGrant{
			UserID: c.userID, ClientID: c.clientID, Scope: c.scope,
			AccessHash: accessHash, AccessExpires: now.Add(oauthAccessTTL).UnixMilli(),
			RefreshHash: refreshHash, RefreshExpires: now.Add(oauthRefreshTTL).UnixMilli(),
		}
		if err := h.store.CreateOAuthGrant(g); err != nil {
			h.internalErr(w, err)
			return
		}
		writeTokens(w, access, refresh, g.Scope)
	case "refresh_token":
		hash := HashToken(f.Get("refresh_token"))
		g, err := h.store.OAuthGrantByRefresh(hash)
		if errors.Is(err, store.ErrNotFound) {
			// A refresh token that was already rotated: someone else may
			// hold it, so the whole grant ends (OAuth 2.1 section 4.3.1).
			if old, err := h.store.OAuthGrantByPreviousRefresh(hash); err == nil && old.ClientID == f.Get("client_id") {
				_ = h.store.DeleteOAuthGrantByID(old.ID)
				h.log.Warn("oauth refresh token reused, grant revoked", "client", old.ClientID, "grant", old.ID)
			}
		}
		if err != nil || g.ClientID != f.Get("client_id") || g.RefreshExpires <= time.Now().UnixMilli() {
			oauthErr(w, 400, "invalid_grant", "unknown or expired refresh token")
			return
		}
		// Refresh tokens rotate: the old one stops working.
		access, accessHash := NewToken(oauthAccessPrefix)
		refresh, refreshHash := NewToken(oauthRefreshPrefix)
		now := time.Now()
		if err := h.store.RotateOAuthGrant(g.ID, g.RefreshHash, accessHash, now.Add(oauthAccessTTL).UnixMilli(), refreshHash, now.Add(oauthRefreshTTL).UnixMilli()); err != nil {
			oauthErr(w, 400, "invalid_grant", "refresh token already used")
			return
		}
		writeTokens(w, access, refresh, g.Scope)
	default:
		oauthErr(w, 400, "unsupported_grant_type", "use authorization_code or refresh_token")
	}
}

func writeTokens(w http.ResponseWriter, access, refresh, scope string) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	writeJSON(w, 200, map[string]any{
		"access_token": access, "token_type": "Bearer", "expires_in": int(oauthAccessTTL.Seconds()),
		"refresh_token": refresh, "scope": scope,
	})
}

// revokeEndpoint ends a grant from either of its tokens (RFC 7009).
func (h *Hub) revokeEndpoint(w http.ResponseWriter, r *http.Request) {
	if allowCORS(w, r) {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<16)
	_ = r.ParseForm()
	hash := HashToken(r.PostForm.Get("token"))
	g, err := h.store.OAuthGrantByRefresh(hash)
	if err != nil {
		g, err = h.store.OAuthGrantByAccess(hash)
	}
	if err == nil && g.ClientID == r.PostForm.Get("client_id") {
		_ = h.store.DeleteOAuthGrantByID(g.ID)
	}
	w.WriteHeader(http.StatusOK)
}

// oauthAccessToken resolves an OAuth access token to a user and an
// equivalent API token (for scope checks).
func (h *Hub) oauthAccessToken(t string) (*store.User, *store.APIToken, error) {
	g, err := h.store.OAuthGrantByAccess(HashToken(t))
	if err != nil {
		return nil, nil, errors.New("invalid token")
	}
	now := time.Now().UnixMilli()
	if g.AccessExpires <= now {
		return nil, nil, errors.New("token expired")
	}
	u, err := h.store.UserByID(g.UserID)
	if err != nil {
		return nil, nil, errors.New("invalid token")
	}
	if now-g.LastUsedAt > time.Minute.Milliseconds() {
		_ = h.store.TouchOAuthGrant(g.ID, now)
	}
	return u, &store.APIToken{UserID: u.ID, Name: g.ClientName, Scope: g.Scope, GrantID: g.ID}, nil
}

// cleanOAuth drops expired grants and clients nobody authorized.
func (h *Hub) cleanOAuth(now time.Time) {
	if err := h.store.DeleteExpiredOAuthGrants(now.UnixMilli()); err != nil {
		h.log.Error("clean oauth grants", "err", err)
	}
	if err := h.store.DeleteUnusedOAuthClients(now.Add(-oauthUnusedClientTTL).UnixMilli(), builtinClientIDs()...); err != nil {
		h.log.Error("clean oauth clients", "err", err)
	}
}

// ---------- connected apps (settings) ----------

func (h *Hub) listGrants(w http.ResponseWriter, r *http.Request, u *store.User) {
	list, err := h.store.ListOAuthGrants(u.ID)
	if err != nil {
		h.internalErr(w, err)
		return
	}
	// The phone that gets push notifications through each sign-in of the app.
	devices, err := h.store.UserPushDevices(u.ID)
	if err != nil {
		h.internalErr(w, err)
		return
	}
	type grantView struct {
		*store.OAuthGrant
		PushDevice string `json:"push_device,omitempty"`
	}
	out := make([]grantView, len(list))
	for i, g := range list {
		out[i].OAuthGrant = g
		for _, d := range devices {
			if d.GrantID == g.ID {
				out[i].PushDevice = d.Name
			}
		}
	}
	writeJSON(w, 200, out)
}

func (h *Hub) deleteGrant(w http.ResponseWriter, r *http.Request, u *store.User) {
	id, ok := pathID(r)
	if !ok {
		writeErr(w, 400, "invalid id")
		return
	}
	if err := h.store.DeleteOAuthGrant(u.ID, id); errors.Is(err, store.ErrNotFound) {
		writeErr(w, 404, "connection not found")
		return
	} else if err != nil {
		h.internalErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

// ---------- pages ----------

func consentError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(code)
	_ = consentPage.Execute(w, map[string]any{"Error": msg})
}

var consentPage = template.Must(template.New("consent").Parse(`<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>pingtxt · authorize</title>
<style>
:root{--bg:#fafafa;--fg:#171717;--muted:#737373;--border:#d4d4d4;--accent:#16a34a;--warn:#ca8a04}
@media (prefers-color-scheme:dark){:root{--bg:#0a0a0a;--fg:#e5e5e5;--muted:#8a8a8a;--border:#2e2e2e;--accent:#4ade80;--warn:#facc15}}
*{box-sizing:border-box}body{margin:0;min-height:100vh;display:flex;align-items:center;justify-content:center;padding:16px;background:var(--bg);color:var(--fg);font:13px/1.6 ui-monospace,SFMono-Regular,Menlo,Consolas,monospace}
main{width:100%;max-width:420px;border:1px dashed var(--border);padding:20px}
h1{font-size:14px;margin:0 0 12px}.p{color:var(--accent)}p{margin:0 0 12px;color:var(--muted)}b{color:var(--fg);font-weight:600}
label{display:flex;gap:8px;align-items:flex-start;padding:8px;border:1px dashed var(--border);margin-bottom:8px;cursor:pointer}
label.off{opacity:.5;cursor:not-allowed}label small{display:block;color:var(--muted)}
.row{display:flex;gap:8px;margin-top:16px}button{font:inherit;padding:6px 14px;border:1px solid var(--border);background:transparent;color:var(--fg);cursor:pointer}
button.allow{border-color:var(--accent);color:var(--accent)}.w{color:var(--warn)}
</style></head><body><main>
<h1><span class="p">$</span> pingtxt authorize</h1>
{{if .Error}}<p>{{.Error}}</p>{{else}}
{{if .Native}}<p><b>{{.Client}}</b> wants to sign in to this hub as <b>{{.Email}}</b>.</p>
<p>After you allow it, you go back to the app. Only continue if you started this from the app.</p>
{{else}}<p><b>{{.Client}}</b> wants to use pingtxt through MCP as <b>{{.Email}}</b>.</p>
<p>After you allow it, you go back to <b>{{.Redirect}}</b>. Only continue if you started this from your AI assistant.</p>{{end}}
<form method="post" action="/oauth/authorize">
<input type="hidden" name="request" value="{{.Request}}">
<label><input type="radio" name="scope" value="read" {{if not .Write}}checked{{end}}><span>read only<small>monitors, incidents, results, maintenance</small></span></label>
<label {{if not .Admin}}class="off"{{end}}><input type="radio" name="scope" value="write" {{if .Write}}checked{{end}} {{if not .Admin}}disabled{{end}}><span>read + <span class="w">write</span><small>{{if .Admin}}also create, edit, pause and delete monitors and maintenance{{else}}your account is read-only{{end}}</small></span></label>
<div class="row"><button class="allow" name="action" value="allow">allow</button><button name="action" value="deny">deny</button></div>
</form>
<p style="margin-top:16px">Revoke it any time in settings → api tokens → connected apps.</p>
{{end}}
</main></body></html>`))
