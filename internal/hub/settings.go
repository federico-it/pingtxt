package hub

import (
	"errors"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"github.com/federico-it/pingtxt/internal/store"
)

// Settings keys in the settings table.
const (
	keyGeneral = "general"
	keyAuth    = "auth"
)

// MonitorDefaults prefill the form for new monitors.
type MonitorDefaults struct {
	IntervalSec int `json:"interval_sec"`
	TimeoutMs   int `json:"timeout_ms"`
	Retries     int `json:"retries"`
}

// GeneralSettings are instance settings editable from the web UI.
type GeneralSettings struct {
	PublicURL       string          `json:"public_url"`
	LocalName       string          `json:"local_name"`
	LocalEnabled    bool            `json:"local_enabled"`
	RetentionDays   int             `json:"retention_days"`
	MonitorDefaults MonitorDefaults `json:"monitor_defaults"`
	Expiry          ExpirySettings  `json:"expiry"`
}

// ExpirySettings set when certificate and domain expiry alerts are sent:
// one alert when the days left cross each threshold. An empty list turns
// that kind of alert off.
type ExpirySettings struct {
	CertDays   []int `json:"cert_days"`
	DomainDays []int `json:"domain_days"`
}

var (
	defaultCertDays   = []int{14, 7, 3, 1}
	defaultDomainDays = []int{30, 14, 7, 1}
)

// normalizeDays sorts thresholds from the largest, drops duplicates and
// out-of-range values. nil (never set) means the defaults.
func normalizeDays(in, def []int) []int {
	if in == nil {
		return slices.Clone(def)
	}
	out := []int{}
	for _, d := range in {
		if d >= 1 && d <= 365 {
			out = append(out, d)
		}
	}
	slices.Sort(out)
	slices.Reverse(out)
	out = slices.Compact(out)
	return out[:min(len(out), 8)]
}

// ProviderSettings configure one OAuth / OIDC login provider.
type ProviderSettings struct {
	Enabled      bool   `json:"enabled"`
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret,omitempty"`
	// OIDC only.
	Name   string `json:"name,omitempty"`
	Issuer string `json:"issuer,omitempty"`
	Scopes string `json:"scopes,omitempty"`
}

// AuthSettings configure how people log in.
type AuthSettings struct {
	PasswordLogin  bool                        `json:"password_login"`
	AllowSignup    bool                        `json:"allow_signup"`
	AllowedDomains []string                    `json:"allowed_domains"`
	Providers      map[string]ProviderSettings `json:"providers"`
}

// Login providers, in display order.
const (
	ProviderGitHub = "github"
	ProviderGoogle = "google"
	ProviderOIDC   = "oidc"
)

var providerIDs = []string{ProviderGitHub, ProviderGoogle, ProviderOIDC}

func defaultGeneral(seed Config) GeneralSettings {
	g := GeneralSettings{
		PublicURL:       seed.PublicURL,
		LocalName:       seed.LocalName,
		LocalEnabled:    !seed.DisableLocal,
		RetentionDays:   seed.RetentionDays,
		MonitorDefaults: MonitorDefaults{IntervalSec: 60, TimeoutMs: 10000, Retries: 2},
	}
	g.normalize()
	return g
}

func defaultAuth() AuthSettings {
	return AuthSettings{PasswordLogin: true, AllowedDomains: []string{}, Providers: map[string]ProviderSettings{}}
}

func (g *GeneralSettings) normalize() {
	g.PublicURL = strings.TrimRight(strings.TrimSpace(g.PublicURL), "/")
	g.LocalName = strings.TrimSpace(g.LocalName)
	if g.LocalName == "" {
		g.LocalName = "hub"
	}
	if g.RetentionDays <= 0 {
		g.RetentionDays = 30
	}
	g.RetentionDays = min(g.RetentionDays, 3650)
	d := &g.MonitorDefaults
	if d.IntervalSec == 0 {
		d.IntervalSec = 60
	}
	d.IntervalSec = min(max(d.IntervalSec, 10), 86400)
	if d.TimeoutMs == 0 {
		d.TimeoutMs = 10000
	}
	d.TimeoutMs = min(max(d.TimeoutMs, 500), 60000)
	d.Retries = min(max(d.Retries, 1), 10)
	g.Expiry.CertDays = normalizeDays(g.Expiry.CertDays, defaultCertDays)
	g.Expiry.DomainDays = normalizeDays(g.Expiry.DomainDays, defaultDomainDays)
}

func (g *GeneralSettings) validate() error {
	if g.PublicURL != "" {
		u, err := url.Parse(g.PublicURL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return errors.New("public url must be an http(s) URL")
		}
	}
	return nil
}

func (a *AuthSettings) normalize() {
	if a.Providers == nil {
		a.Providers = map[string]ProviderSettings{}
	}
	for k := range a.Providers {
		if !slices.Contains(providerIDs, k) {
			delete(a.Providers, k)
		}
	}
	doms := []string{}
	for _, d := range a.AllowedDomains {
		d = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(d), "@"))
		if d != "" {
			doms = append(doms, d)
		}
	}
	slices.Sort(doms)
	a.AllowedDomains = slices.Compact(doms)
	for k, p := range a.Providers {
		p.ClientID = strings.TrimSpace(p.ClientID)
		p.ClientSecret = strings.TrimSpace(p.ClientSecret)
		p.Name = strings.TrimSpace(p.Name)
		p.Issuer = strings.TrimRight(strings.TrimSpace(p.Issuer), "/")
		p.Scopes = strings.Join(strings.Fields(strings.ReplaceAll(p.Scopes, ",", " ")), " ")
		a.Providers[k] = p
	}
}

func (a *AuthSettings) validate() error {
	for _, id := range providerIDs {
		p, ok := a.Providers[id]
		if !ok || !p.Enabled {
			continue
		}
		if p.ClientID == "" || p.ClientSecret == "" {
			return errors.New(providerLabel(id, p) + ": client id and client secret are required")
		}
		if id == ProviderOIDC {
			u, err := url.Parse(p.Issuer)
			if err != nil || u.Scheme != "https" && !(u.Scheme == "http" && isLoopback(u.Hostname())) || u.Host == "" {
				return errors.New("oidc: issuer must be an https URL")
			}
		}
	}
	if !a.PasswordLogin && len(a.enabledProviders()) == 0 {
		return errors.New("enable at least one login provider before turning off password login")
	}
	return nil
}

func isLoopback(host string) bool {
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}

func (a *AuthSettings) enabledProviders() []string {
	out := []string{}
	for _, id := range providerIDs {
		if p, ok := a.Providers[id]; ok && p.Enabled && p.ClientID != "" && p.ClientSecret != "" {
			out = append(out, id)
		}
	}
	return out
}

// passwordLoginAllowed is true unless an admin turned it off and a provider can replace it.
func (a *AuthSettings) passwordLoginAllowed() bool {
	return a.PasswordLogin || len(a.enabledProviders()) == 0
}

func providerLabel(id string, p ProviderSettings) string {
	switch id {
	case ProviderGitHub:
		return "GitHub"
	case ProviderGoogle:
		return "Google"
	}
	if p.Name != "" {
		return p.Name
	}
	return "SSO"
}

// emailAllowed checks the sign-up domain allow list (empty list allows any domain).
func (a *AuthSettings) emailAllowed(email string) bool {
	if len(a.AllowedDomains) == 0 {
		return true
	}
	at := strings.LastIndexByte(email, '@')
	return at > 0 && slices.Contains(a.AllowedDomains, strings.ToLower(email[at+1:]))
}

// loadSettings reads the settings from the database. On first start it seeds
// them from the legacy environment variables (cfg), so old setups keep working.
func (h *Hub) loadSettings() error {
	g := defaultGeneral(h.cfg)
	if err := h.store.GetSetting(keyGeneral, &g); errors.Is(err, store.ErrNotFound) {
		if err := h.store.SetSetting(keyGeneral, g); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	g.normalize()
	a := defaultAuth()
	if err := h.store.GetSetting(keyAuth, &a); err != nil && !errors.Is(err, store.ErrNotFound) {
		return err
	}
	a.normalize()
	h.smu.Lock()
	h.general, h.auth = g, a
	h.smu.Unlock()
	return nil
}

func (h *Hub) generalSettings() GeneralSettings {
	h.smu.RLock()
	defer h.smu.RUnlock()
	return h.general
}

func (h *Hub) authSettings() AuthSettings {
	h.smu.RLock()
	defer h.smu.RUnlock()
	a := h.auth
	a.Providers = make(map[string]ProviderSettings, len(h.auth.Providers))
	for k, v := range h.auth.Providers {
		a.Providers[k] = v
	}
	a.AllowedDomains = slices.Clone(h.auth.AllowedDomains)
	return a
}

func (h *Hub) localEnabled() bool { return h.generalSettings().LocalEnabled }
func (h *Hub) localName() string  { return h.generalSettings().LocalName }

// EnablePasswordLogin turns password login back on (recovery when SSO is broken).
func (h *Hub) EnablePasswordLogin() error {
	if err := h.loadSettings(); err != nil {
		return err
	}
	a := h.authSettings()
	a.PasswordLogin = true
	if err := h.store.SetSetting(keyAuth, a); err != nil {
		return err
	}
	h.smu.Lock()
	h.auth = a
	h.smu.Unlock()
	return nil
}

// ---------- API ----------

type providerView struct {
	ProviderSettings
	ID           string `json:"id"`
	Label        string `json:"label"`
	SecretSet    bool   `json:"client_secret_set"`
	CallbackURL  string `json:"callback_url"`
	ClientSecret string `json:"client_secret"` // always empty: secrets are write-only
}

type settingsView struct {
	General   GeneralSettings `json:"general"`
	Auth      authView        `json:"auth"`
	DetectURL string          `json:"detected_url"`
}

type authView struct {
	PasswordLogin  bool           `json:"password_login"`
	AllowSignup    bool           `json:"allow_signup"`
	AllowedDomains []string       `json:"allowed_domains"`
	Providers      []providerView `json:"providers"`
}

func (h *Hub) settingsResponse(r *http.Request) settingsView {
	a := h.authSettings()
	v := settingsView{
		General:   h.generalSettings(),
		DetectURL: requestURL(r),
		Auth:      authView{PasswordLogin: a.PasswordLogin, AllowSignup: a.AllowSignup, AllowedDomains: a.AllowedDomains},
	}
	for _, id := range providerIDs {
		p := a.Providers[id]
		v.Auth.Providers = append(v.Auth.Providers, providerView{
			ProviderSettings: ProviderSettings{Enabled: p.Enabled, ClientID: p.ClientID, Name: p.Name, Issuer: p.Issuer, Scopes: p.Scopes},
			ID:               id,
			Label:            providerLabel(id, p),
			SecretSet:        p.ClientSecret != "",
			CallbackURL:      h.hubURL(r) + "/api/auth/oauth/" + id + "/callback",
		})
	}
	return v
}

func (h *Hub) getSettings(w http.ResponseWriter, r *http.Request, _ *store.User) {
	writeJSON(w, 200, h.settingsResponse(r))
}

func (h *Hub) putGeneralSettings(w http.ResponseWriter, r *http.Request, _ *store.User) {
	var g GeneralSettings
	if readJSON(r, &g) != nil {
		writeErr(w, 400, "invalid body")
		return
	}
	g.normalize()
	if err := g.validate(); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	if err := h.store.SetSetting(keyGeneral, g); err != nil {
		h.internalErr(w, err)
		return
	}
	h.smu.Lock()
	h.general = g
	h.smu.Unlock()
	writeJSON(w, 200, h.settingsResponse(r))
}

func (h *Hub) putAuthSettings(w http.ResponseWriter, r *http.Request, _ *store.User) {
	var in struct {
		PasswordLogin  bool                        `json:"password_login"`
		AllowSignup    bool                        `json:"allow_signup"`
		AllowedDomains []string                    `json:"allowed_domains"`
		Providers      map[string]ProviderSettings `json:"providers"`
	}
	if readJSON(r, &in) != nil {
		writeErr(w, 400, "invalid body")
		return
	}
	cur := h.authSettings()
	a := AuthSettings{PasswordLogin: in.PasswordLogin, AllowSignup: in.AllowSignup, AllowedDomains: in.AllowedDomains, Providers: in.Providers}
	if a.Providers == nil {
		a.Providers = map[string]ProviderSettings{}
	}
	// Secrets are write-only: an empty secret keeps the stored one.
	for id, p := range a.Providers {
		if strings.TrimSpace(p.ClientSecret) == "" {
			p.ClientSecret = cur.Providers[id].ClientSecret
			a.Providers[id] = p
		}
	}
	a.normalize()
	if err := a.validate(); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	if err := h.store.SetSetting(keyAuth, a); err != nil {
		h.internalErr(w, err)
		return
	}
	h.smu.Lock()
	h.auth = a
	h.smu.Unlock()
	h.oidc.reset()
	writeJSON(w, 200, h.settingsResponse(r))
}

// authOptions is public: the login page uses it to render provider buttons.
func (h *Hub) authOptions(w http.ResponseWriter, r *http.Request) {
	a := h.authSettings()
	list := []map[string]string{}
	for _, id := range a.enabledProviders() {
		list = append(list, map[string]string{"id": id, "label": providerLabel(id, a.Providers[id])})
	}
	writeJSON(w, 200, map[string]any{"password_login": a.passwordLoginAllowed(), "providers": list})
}
