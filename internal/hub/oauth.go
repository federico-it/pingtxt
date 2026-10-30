package hub

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/github"

	"github.com/federico-it/pingtxt/internal/store"
)

const (
	oauthCookie   = "pingtxt_oauth"
	oauthStateTTL = 10 * time.Minute
	googleIssuer  = "https://accounts.google.com"
)

// pendingLogin is an OAuth flow started by a browser and not finished yet.
type pendingLogin struct {
	provider string
	verifier string
	linkUser int64 // >0: link the external account to this user instead of logging in
	expires  time.Time
}

type oauthStates struct {
	mu sync.Mutex
	m  map[string]pendingLogin
}

func (s *oauthStates) put(state string, p pendingLogin) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.m == nil {
		s.m = map[string]pendingLogin{}
	}
	now := time.Now()
	for k, v := range s.m {
		if now.After(v.expires) {
			delete(s.m, k)
		}
	}
	if len(s.m) >= 1000 { // bound memory if someone hammers /start
		for k := range s.m {
			delete(s.m, k)
			break
		}
	}
	s.m[state] = p
}

func (s *oauthStates) take(state string) (pendingLogin, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.m[state]
	delete(s.m, state)
	if !ok || time.Now().After(p.expires) {
		return pendingLogin{}, false
	}
	return p, true
}

// oidcDiscovery caches the endpoints published at /.well-known/openid-configuration.
type oidcDiscovery struct {
	mu    sync.Mutex
	cache map[string]*oidcConfig
}

type oidcConfig struct {
	AuthURL     string `json:"authorization_endpoint"`
	TokenURL    string `json:"token_endpoint"`
	UserinfoURL string `json:"userinfo_endpoint"`
	fetched     time.Time
}

func (d *oidcDiscovery) reset() {
	d.mu.Lock()
	d.cache = nil
	d.mu.Unlock()
}

func (d *oidcDiscovery) get(ctx context.Context, client *http.Client, issuer string) (*oidcConfig, error) {
	d.mu.Lock()
	if c, ok := d.cache[issuer]; ok && time.Since(c.fetched) < time.Hour {
		d.mu.Unlock()
		return c, nil
	}
	d.mu.Unlock()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, issuer+"/.well-known/openid-configuration", nil)
	res, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("oidc discovery: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return nil, fmt.Errorf("oidc discovery: %s returned %d", issuer, res.StatusCode)
	}
	c := &oidcConfig{}
	if err := json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(c); err != nil {
		return nil, fmt.Errorf("oidc discovery: %w", err)
	}
	if c.AuthURL == "" || c.TokenURL == "" || c.UserinfoURL == "" {
		return nil, errors.New("oidc discovery: issuer does not publish authorization, token and userinfo endpoints")
	}
	c.fetched = time.Now()
	d.mu.Lock()
	if d.cache == nil {
		d.cache = map[string]*oidcConfig{}
	}
	d.cache[issuer] = c
	d.mu.Unlock()
	return c, nil
}

// externalUser is what we learn about the person from the provider.
type externalUser struct {
	Subject  string
	Email    string
	Verified bool
}

func (h *Hub) oauthConfig(ctx context.Context, r *http.Request, id string) (*oauth2.Config, string, error) {
	a := h.authSettings()
	p, ok := a.Providers[id]
	if !ok || !p.Enabled || p.ClientID == "" || p.ClientSecret == "" {
		return nil, "", errors.New("login provider not enabled")
	}
	cfg := &oauth2.Config{
		ClientID:     p.ClientID,
		ClientSecret: p.ClientSecret,
		RedirectURL:  h.hubURL(r) + "/api/auth/oauth/" + id + "/callback",
	}
	switch id {
	case ProviderGitHub:
		cfg.Endpoint = github.Endpoint
		cfg.Scopes = []string{"read:user", "user:email"}
		return cfg, "", nil
	case ProviderGoogle, ProviderOIDC:
		issuer := googleIssuer
		cfg.Scopes = []string{"openid", "email", "profile"}
		if id == ProviderOIDC {
			issuer = p.Issuer
			if p.Scopes != "" {
				cfg.Scopes = strings.Fields(p.Scopes)
			}
		}
		d, err := h.oidc.get(ctx, h.httpClient, issuer)
		if err != nil {
			return nil, "", err
		}
		cfg.Endpoint = oauth2.Endpoint{AuthURL: d.AuthURL, TokenURL: d.TokenURL}
		return cfg, d.UserinfoURL, nil
	}
	return nil, "", errors.New("unknown provider")
}

func (h *Hub) oauthStart(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("provider")
	cfg, _, err := h.oauthConfig(r.Context(), r, id)
	if err != nil {
		h.loginRedirect(w, r, err.Error())
		return
	}
	p := pendingLogin{provider: id, verifier: oauth2.GenerateVerifier(), expires: time.Now().Add(oauthStateTTL)}
	if r.URL.Query().Get("link") == "1" {
		u := h.currentUser(r)
		if u == nil {
			h.loginRedirect(w, r, "log in first to connect an account")
			return
		}
		p.linkUser = u.ID
	}
	state, _ := NewToken("")
	h.states.put(state, p)
	http.SetCookie(w, &http.Cookie{
		Name: oauthCookie, Value: state, Path: "/api/auth/oauth/", MaxAge: int(oauthStateTTL.Seconds()),
		HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: isHTTPS(r),
	})
	http.Redirect(w, r, cfg.AuthCodeURL(state, oauth2.S256ChallengeOption(p.verifier)), http.StatusFound)
}

func (h *Hub) oauthCallback(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("provider")
	q := r.URL.Query()
	http.SetCookie(w, &http.Cookie{Name: oauthCookie, Value: "", Path: "/api/auth/oauth/", MaxAge: -1, HttpOnly: true})
	c, err := r.Cookie(oauthCookie)
	state := q.Get("state")
	if err != nil || state == "" || c.Value != state {
		h.loginRedirect(w, r, "login expired or started in another browser, try again")
		return
	}
	p, ok := h.states.take(state)
	if !ok || p.provider != id {
		h.loginRedirect(w, r, "login expired, try again")
		return
	}
	if e := q.Get("error"); e != "" {
		msg := q.Get("error_description")
		if msg == "" {
			msg = e
		}
		h.finishRedirect(w, r, p, "provider: "+msg)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	ctx = context.WithValue(ctx, oauth2.HTTPClient, h.httpClient)
	cfg, userinfo, err := h.oauthConfig(ctx, r, id)
	if err != nil {
		h.finishRedirect(w, r, p, err.Error())
		return
	}
	tok, err := cfg.Exchange(ctx, q.Get("code"), oauth2.VerifierOption(p.verifier))
	if err != nil {
		h.log.Warn("oauth exchange", "provider", id, "err", err)
		h.finishRedirect(w, r, p, "could not complete login with the provider (check client id and secret)")
		return
	}
	client := cfg.Client(ctx, tok)
	var ext *externalUser
	if id == ProviderGitHub {
		ext, err = githubUser(ctx, client)
	} else {
		ext, err = oidcUser(ctx, client, userinfo)
		// The admin runs their own identity provider: trust the email it returns.
		if ext != nil && id == ProviderOIDC && ext.Email != "" {
			ext.Verified = true
		}
	}
	if err != nil {
		h.log.Warn("oauth userinfo", "provider", id, "err", err)
		h.finishRedirect(w, r, p, "could not read your profile from the provider")
		return
	}
	ext.Email = strings.ToLower(strings.TrimSpace(ext.Email))

	if p.linkUser > 0 {
		h.linkAccount(w, r, id, ext, p.linkUser)
		return
	}
	u, err := h.resolveExternalUser(id, ext)
	if err != nil {
		h.loginRedirect(w, r, err.Error())
		return
	}
	if err := h.startSession(w, r, u); err != nil {
		h.log.Error("start session", "err", err)
		h.loginRedirect(w, r, "internal error")
		return
	}
	http.Redirect(w, r, "/", http.StatusFound)
}

// resolveExternalUser finds (or creates, when sign-up is on) the local user for an external account.
func (h *Hub) resolveExternalUser(provider string, ext *externalUser) (*store.User, error) {
	if u, err := h.store.IdentityUser(provider, ext.Subject); err == nil {
		if ext.Email != "" {
			_ = h.store.LinkIdentity(provider, ext.Subject, u.ID, ext.Email)
		}
		return u, nil
	} else if !errors.Is(err, store.ErrNotFound) {
		return nil, errors.New("internal error")
	}
	if ext.Email == "" || !ext.Verified {
		return nil, errors.New("your account at the provider has no verified email")
	}
	u, err := h.store.UserByEmail(ext.Email)
	if errors.Is(err, store.ErrNotFound) {
		a := h.authSettings()
		if !a.AllowSignup || !a.emailAllowed(ext.Email) {
			return nil, fmt.Errorf("no account for %s on this instance", ext.Email)
		}
		u, err = h.store.CreateUser(ext.Email, "")
		if err == nil {
			h.log.Info("user created via sso", "email", ext.Email, "provider", provider)
		}
	}
	if err != nil {
		return nil, errors.New("internal error")
	}
	if err := h.store.LinkIdentity(provider, ext.Subject, u.ID, ext.Email); err != nil {
		return nil, errors.New("internal error")
	}
	return u, nil
}

func (h *Hub) linkAccount(w http.ResponseWriter, r *http.Request, provider string, ext *externalUser, userID int64) {
	cur := h.currentUser(r)
	if cur == nil || cur.ID != userID {
		h.loginRedirect(w, r, "session changed during login, try again")
		return
	}
	if other, err := h.store.IdentityUser(provider, ext.Subject); err == nil && other.ID != userID {
		h.settingsRedirect(w, r, "this "+provider+" account is already connected to "+other.Email)
		return
	}
	_ = h.store.UnlinkIdentity(userID, provider) // one account per provider and user
	if err := h.store.LinkIdentity(provider, ext.Subject, userID, ext.Email); err != nil {
		h.settingsRedirect(w, r, "internal error")
		return
	}
	http.Redirect(w, r, "/settings/?linked="+url.QueryEscape(provider), http.StatusFound)
}

func (h *Hub) finishRedirect(w http.ResponseWriter, r *http.Request, p pendingLogin, msg string) {
	if p.linkUser > 0 {
		h.settingsRedirect(w, r, msg)
		return
	}
	h.loginRedirect(w, r, msg)
}

func (h *Hub) loginRedirect(w http.ResponseWriter, r *http.Request, msg string) {
	http.Redirect(w, r, "/login/?error="+url.QueryEscape(msg), http.StatusFound)
}

func (h *Hub) settingsRedirect(w http.ResponseWriter, r *http.Request, msg string) {
	http.Redirect(w, r, "/settings/?error="+url.QueryEscape(msg), http.StatusFound)
}

func getJSON(ctx context.Context, client *http.Client, u string, v any) error {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	req.Header.Set("Accept", "application/json")
	res, err := client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return fmt.Errorf("%s returned %d", u, res.StatusCode)
	}
	return json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(v)
}

var githubAPI = "https://api.github.com"

func githubUser(ctx context.Context, client *http.Client) (*externalUser, error) {
	var u struct {
		ID int64 `json:"id"`
	}
	if err := getJSON(ctx, client, githubAPI+"/user", &u); err != nil {
		return nil, err
	}
	if u.ID == 0 {
		return nil, errors.New("github: missing user id")
	}
	ext := &externalUser{Subject: fmt.Sprint(u.ID)}
	var emails []struct {
		Email    string `json:"email"`
		Primary  bool   `json:"primary"`
		Verified bool   `json:"verified"`
	}
	if err := getJSON(ctx, client, githubAPI+"/user/emails", &emails); err != nil {
		return nil, err
	}
	for _, e := range emails {
		if e.Primary && e.Verified {
			ext.Email, ext.Verified = e.Email, true
		}
	}
	return ext, nil
}

func oidcUser(ctx context.Context, client *http.Client, userinfo string) (*externalUser, error) {
	var u struct {
		Sub           string `json:"sub"`
		Email         string `json:"email"`
		EmailVerified any    `json:"email_verified"` // bool, or "true" with some providers
	}
	if err := getJSON(ctx, client, userinfo, &u); err != nil {
		return nil, err
	}
	if u.Sub == "" {
		return nil, errors.New("userinfo: missing sub")
	}
	verified := false
	switch v := u.EmailVerified.(type) {
	case bool:
		verified = v
	case string:
		verified = v == "true"
	}
	return &externalUser{Subject: u.Sub, Email: u.Email, Verified: verified}, nil
}

// ---------- account endpoints ----------

func (h *Hub) unlinkIdentity(w http.ResponseWriter, r *http.Request, u *store.User) {
	provider := r.PathValue("provider")
	ids, err := h.store.UserIdentities(u.ID)
	if err != nil {
		h.internalErr(w, err)
		return
	}
	if u.PasswordHash == "" && len(ids) <= 1 {
		writeErr(w, 400, "set a password first, or you will not be able to log in")
		return
	}
	if err := h.store.UnlinkIdentity(u.ID, provider); err != nil {
		h.internalErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

type userView struct {
	*store.User
	HasPassword bool              `json:"has_password"`
	TwoFactor   bool              `json:"two_factor"`
	Identities  []*store.Identity `json:"identities"`
}

func (h *Hub) userView(u *store.User) userView {
	ids, _ := h.store.UserIdentities(u.ID)
	if ids == nil {
		ids = []*store.Identity{}
	}
	return userView{User: u, HasPassword: u.PasswordHash != "", TwoFactor: h.store.TwoFactorEnabled(u.ID), Identities: ids}
}

func (h *Hub) listUsers(w http.ResponseWriter, r *http.Request, _ *store.User) {
	users, err := h.store.ListUsers()
	if err != nil {
		h.internalErr(w, err)
		return
	}
	out := make([]userView, 0, len(users))
	for _, u := range users {
		out = append(out, h.userView(u))
	}
	writeJSON(w, 200, out)
}

func (h *Hub) deleteUser(w http.ResponseWriter, r *http.Request, me *store.User) {
	id, ok := pathID(r)
	if !ok {
		writeErr(w, 400, "invalid id")
		return
	}
	if id == me.ID {
		writeErr(w, 400, "you cannot delete your own account")
		return
	}
	if err := h.store.DeleteUser(id); err != nil {
		h.internalErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}
