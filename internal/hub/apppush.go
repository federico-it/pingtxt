package hub

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/mail"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/federico-it/pingtxt/internal/store"
)

// Push notifications to the pingtxt mobile app go through a push relay
// (github.com/federico-it/pingtxt-push-relay), which holds the Firebase key
// of the app. The hub registers once with the relay and keeps its hub token.
// The app turns its FCM token into an opaque device handle at the relay and
// gives the handle to the hub (POST /api/mobile/devices). The "app"
// notification channel sends alerts to those handles; the relay reports the
// handles that no longer work and the hub deletes them.
//
// The hub never sees FCM tokens, the relay never stores anything.

const (
	keyPush         = "push"
	defaultRelayURL = "https://push.pingtxt.com"

	relayBatch       = 100 // max handles per POST /v1/push
	maxDeviceHandle  = 1024
	maxDeviceName    = 100
	maxPushTitle     = 200
	maxPushBody      = 2000
	maxRelayResponse = 1 << 20
)

// PushSettings configure notifications to the mobile app.
type PushSettings struct {
	Enabled  bool   `json:"enabled"`
	RelayURL string `json:"relay_url"`
	// HubID and HubToken come from the relay (POST /v1/hubs). The token is
	// secret and never leaves the hub.
	HubID    string `json:"hub_id"`
	HubToken string `json:"hub_token"`
}

func (n *Notifier) pushSettings() (PushSettings, error) {
	ps := PushSettings{RelayURL: defaultRelayURL}
	if err := n.store.GetSetting(keyPush, &ps); err != nil && !errors.Is(err, store.ErrNotFound) {
		return ps, err
	}
	if ps.RelayURL == "" {
		ps.RelayURL = defaultRelayURL
	}
	return ps, nil
}

// ready reports whether the app can enroll and the hub can push.
func (ps PushSettings) ready() bool { return ps.Enabled && ps.HubID != "" && ps.HubToken != "" }

// normalizeRelayURL checks a relay base URL: https, or http on loopback
// (development).
func normalizeRelayURL(s string) (string, error) {
	s = strings.TrimRight(strings.TrimSpace(s), "/")
	if s == "" {
		return defaultRelayURL, nil
	}
	u, err := url.Parse(s)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" ||
		(u.Scheme != "https" && !(u.Scheme == "http" && isLoopback(u.Hostname()))) {
		return "", errors.New("relay url must be an https URL")
	}
	return s, nil
}

// ---------- relay client ----------

// relayError is an error answer of the relay.
type relayError struct {
	Status     int
	Code       string
	Message    string
	RetryAfter time.Duration
}

func (e *relayError) Error() string {
	msg := e.Message
	if msg == "" {
		msg = e.Code
	}
	if msg == "" {
		msg = http.StatusText(e.Status)
	}
	return fmt.Sprintf("push relay: http %d: %s", e.Status, msg)
}

// relayCall sends one request to the relay and decodes a 2xx answer into out.
func (n *Notifier) relayCall(ctx context.Context, method, base, path, token string, in, out any) error {
	var body io.Reader
	if in != nil {
		body = bytes.NewReader(mustJSON(in))
	}
	req, err := http.NewRequestWithContext(ctx, method, base+path, body)
	if err != nil {
		return err
	}
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("User-Agent", "pingtxt")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := n.client.Do(req)
	if err != nil {
		return fmt.Errorf("push relay: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, maxRelayResponse))
	if resp.StatusCode >= 300 {
		re := &relayError{Status: resp.StatusCode}
		var e struct {
			Error   string `json:"error"`
			Message string `json:"message"`
		}
		if json.Unmarshal(raw, &e) == nil {
			re.Code, re.Message = e.Error, e.Message
		}
		if s, err := strconv.Atoi(resp.Header.Get("Retry-After")); err == nil && s > 0 {
			re.RetryAfter = time.Duration(s) * time.Second
		}
		return re
	}
	if out != nil {
		if err := json.Unmarshal(raw, out); err != nil {
			return fmt.Errorf("push relay: invalid answer: %w", err)
		}
	}
	return nil
}

// registerHub asks the relay for a new hub id and token.
func (n *Notifier) registerHub(ctx context.Context, relay, registrationToken string) (id, token string, err error) {
	var out struct {
		HubID    string `json:"hub_id"`
		HubToken string `json:"hub_token"`
	}
	if err := n.relayCall(ctx, http.MethodPost, relay, "/v1/hubs", registrationToken, nil, &out); err != nil {
		return "", "", err
	}
	if out.HubID == "" || out.HubToken == "" {
		return "", "", errors.New("push relay: no hub id in the answer")
	}
	return out.HubID, out.HubToken, nil
}

// pushMessage is the body of POST /v1/push.
type pushMessage struct {
	Devices    []string          `json:"devices"`
	Title      string            `json:"title"`
	Body       string            `json:"body,omitempty"`
	Priority   string            `json:"priority,omitempty"`
	ThreadID   string            `json:"thread_id,omitempty"`
	CollapseID string            `json:"collapse_id,omitempty"`
	TTL        int               `json:"ttl,omitempty"`
	Data       map[string]string `json:"data,omitempty"`
}

type pushResult struct {
	Device string `json:"device"`
	Status string `json:"status"` // sent, gone, rate_limited, rejected, failed
	Error  string `json:"error"`
}

// truncateUTF8 cuts s to at most n bytes without splitting a character.
func truncateUTF8(s string, n int) string {
	if len(s) <= n {
		return s
	}
	s = s[:n]
	for len(s) > 0 && !utf8.ValidString(s) {
		s = s[:len(s)-1]
	}
	return s
}

// appMessage turns an event into a notification for the app.
func appMessage(e Event) pushMessage {
	m := pushMessage{Priority: "normal", TTL: 3600, Data: map[string]string{"type": "monitor", "status": e.Status}}
	if e.MonitorID > 0 {
		id := strconv.FormatInt(e.MonitorID, 10)
		m.ThreadID, m.CollapseID = "monitor-"+id, "monitor-"+id
		m.Data["monitor_id"] = id
	}
	var body []string
	switch e.Kind {
	case KindCert, KindDomain:
		m.Data["type"] = e.Kind
		m.TTL = 86400
		if e.Status == ExpiryExpired {
			m.Priority = "high"
		}
		if e.MonitorID > 0 {
			m.CollapseID = e.Kind + "-" + strconv.FormatInt(e.MonitorID, 10)
		}
		// "[CERT] host: TLS certificate expires in 3 days (date)\n> monitor x (target)"
		first, rest, _ := strings.Cut(e.expiryText(), "\n")
		if _, after, ok := strings.Cut(first, "] "); ok {
			first = after
		}
		m.Title = first
		body = append(body, strings.TrimPrefix(rest, "> "))
	default:
		if e.Status == StatusDown {
			m.Priority = "high"
			m.Title = e.Monitor + " is down"
			if e.Reminder > 0 {
				m.Title = e.Monitor + " is still down"
			}
			body = append(body, e.Target)
			if e.Cause != "" {
				body = append(body, e.Cause)
			}
			if d := e.downFor(); d != "" {
				body = append(body, "Down for "+d)
			}
		} else {
			m.Title = e.Monitor + " is back up"
			line := e.Target
			if d := e.downFor(); d != "" {
				line += " · after " + d + " down"
			}
			body = append(body, line)
		}
	}
	if e.Test {
		m.Title = "[test] " + m.Title
		m.ThreadID, m.CollapseID = "test", "test"
		m.Data["test"] = "true"
	}
	m.Title = truncateUTF8(m.Title, maxPushTitle)
	m.Body = truncateUTF8(strings.Join(body, "\n"), maxPushBody)
	return m
}

// appRecipients resolves the "users" setting of an app channel (emails,
// comma separated) to user ids. nil means every user.
func (n *Notifier) appRecipients(c map[string]string) ([]int64, error) {
	emails := splitList(c["users"])
	if len(emails) == 0 {
		return nil, nil
	}
	users, err := n.store.ListUsers()
	if err != nil {
		return nil, err
	}
	ids := []int64{}
	for _, u := range users {
		for _, e := range emails {
			if strings.EqualFold(u.Email, e) {
				ids = append(ids, u.ID)
				break
			}
		}
	}
	return ids, nil
}

// splitList splits a comma (or space) separated list.
func splitList(s string) []string {
	return strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ';' || r == ' ' || r == '\n' })
}

// validateAppConfig checks the "users" list of an app channel.
func validateAppConfig(c map[string]string) error {
	list := splitList(c["users"])
	for _, e := range list {
		if _, err := mail.ParseAddress(e); err != nil {
			return fmt.Errorf("invalid email %q", e)
		}
	}
	c["users"] = strings.Join(list, ", ")
	return nil
}

// setupError is a problem with the hub's own setup (push off, no phones),
// not a failure of the relay: a test answers 409 instead of 502.
type setupError string

func (e setupError) Error() string { return string(e) }

// sendApp delivers an event to the phones of the channel's users. Handles the
// relay reports gone are deleted; failed ones are retried with backoff.
func (n *Notifier) sendApp(ctx context.Context, c map[string]string, e Event) error {
	ps, err := n.pushSettings()
	if err != nil {
		return err
	}
	if !ps.ready() {
		return setupError("mobile app notifications are off: turn them on in Settings, Mobile app")
	}
	ids, err := n.appRecipients(c)
	if err != nil {
		return err
	}
	devices, err := n.store.PushDevicesFor(ids)
	if err != nil {
		return err
	}
	if len(devices) == 0 {
		if ids != nil && len(ids) == 0 {
			return setupError("none of the users of this channel exists on this hub")
		}
		return setupError("no phone is signed in to the pingtxt app for this channel: sign in to this hub with the app and allow notifications")
	}
	byHandle := map[string][]*store.PushDevice{}
	pending := []string{}
	for _, d := range devices {
		if _, ok := byHandle[d.Handle]; !ok {
			pending = append(pending, d.Handle)
		}
		byHandle[d.Handle] = append(byHandle[d.Handle], d)
	}
	msg := appMessage(e)
	delays := n.pushRetry
	if delays == nil {
		delays = []time.Duration{10 * time.Second, time.Minute}
	}
	if e.Test {
		delays = nil // a test answers right away
	}
	total, sent, dropped := len(pending), 0, 0
	var lastErr error
	for attempt := 0; ; attempt++ {
		var retry []string
		var wait time.Duration
		for start := 0; start < len(pending); start += relayBatch {
			batch := pending[start:min(start+relayBatch, len(pending))]
			msg.Devices = batch
			var out struct {
				Results []pushResult `json:"results"`
			}
			err := n.relayCall(ctx, http.MethodPost, ps.RelayURL, "/v1/push", ps.HubToken, msg, &out)
			if err != nil {
				lastErr = err
				var re *relayError
				if errors.As(err, &re) && re.Status != http.StatusTooManyRequests && re.Status < 500 {
					// 400/401/403/413: retrying does not help.
					dropped += len(batch)
					continue
				}
				if errors.As(err, &re) {
					wait = max(wait, re.RetryAfter)
				}
				retry = append(retry, batch...)
				continue
			}
			for _, r := range out.Results {
				switch r.Status {
				case "sent":
					sent++
				case "gone":
					dropped++
					for _, d := range byHandle[r.Device] {
						if err := n.store.DeletePushDeviceByID(d.ID, d.Handle); err != nil {
							n.log.Error("delete push device", "err", err)
						}
					}
				case "failed":
					retry = append(retry, r.Device)
					lastErr = fmt.Errorf("push relay: %s", r.Error)
				default: // rate_limited, rejected
					dropped++
					lastErr = fmt.Errorf("push relay: %s: %s", r.Status, r.Error)
					if r.Status == "rejected" {
						n.log.Warn("push rejected", "err", r.Error)
					}
				}
			}
		}
		if len(retry) == 0 || attempt >= len(delays) {
			dropped += len(retry)
			break
		}
		pending = retry
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(max(delays[attempt], min(wait, 5*time.Minute))):
		}
	}
	if sent == 0 && total > 0 && lastErr == nil {
		return setupError("every phone of this channel is gone, sign in again with the app")
	}
	if dropped > 0 && lastErr != nil {
		if sent == 0 {
			return lastErr
		}
		return fmt.Errorf("sent to %d of %d phones: %w", sent, total, lastErr)
	}
	return nil
}

// ---------- settings API (admins) ----------

type pushSettingsView struct {
	Enabled    bool   `json:"enabled"`
	RelayURL   string `json:"relay_url"`
	HubID      string `json:"hub_id"`
	Registered bool   `json:"registered"`
	Devices    int    `json:"devices"`
}

func (h *Hub) pushSettingsView() (pushSettingsView, error) {
	ps, err := h.notify.pushSettings()
	if err != nil {
		return pushSettingsView{}, err
	}
	n, err := h.store.CountPushDevices()
	if err != nil {
		return pushSettingsView{}, err
	}
	return pushSettingsView{Enabled: ps.Enabled, RelayURL: ps.RelayURL, HubID: ps.HubID, Registered: ps.HubToken != "", Devices: n}, nil
}

func (h *Hub) getPushSettings(w http.ResponseWriter, r *http.Request, _ *store.User) {
	v, err := h.pushSettingsView()
	if err != nil {
		h.internalErr(w, err)
		return
	}
	writeJSON(w, 200, v)
}

// putPushSettings saves the relay and the switch. Turning the feature on
// registers the hub with the relay the first time; another relay means a
// new registration, and the phones enroll again.
func (h *Hub) putPushSettings(w http.ResponseWriter, r *http.Request, _ *store.User) {
	var in struct {
		Enabled           bool   `json:"enabled"`
		RelayURL          string `json:"relay_url"`
		RegistrationToken string `json:"registration_token"`
	}
	if readJSON(r, &in) != nil {
		writeErr(w, 400, "invalid body")
		return
	}
	relay, err := normalizeRelayURL(in.RelayURL)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	h.pushMu.Lock()
	defer h.pushMu.Unlock()
	ps, err := h.notify.pushSettings()
	if err != nil {
		h.internalErr(w, err)
		return
	}
	reset := relay != ps.RelayURL && ps.HubToken != ""
	ps.Enabled, ps.RelayURL = in.Enabled, relay
	if reset {
		ps.HubID, ps.HubToken = "", ""
	}
	if ps.Enabled && ps.HubToken == "" {
		ps.HubID, ps.HubToken, err = h.notify.registerHub(r.Context(), relay, strings.TrimSpace(in.RegistrationToken))
		if err != nil {
			writeErr(w, 502, err.Error())
			return
		}
	}
	h.savePushSettings(w, ps, reset)
}

// registerPush registers the hub again: a new hub id, every phone enrolls again.
func (h *Hub) registerPush(w http.ResponseWriter, r *http.Request, _ *store.User) {
	var in struct {
		RegistrationToken string `json:"registration_token"`
	}
	_ = readJSON(r, &in) // the body is optional
	h.pushMu.Lock()
	defer h.pushMu.Unlock()
	ps, err := h.notify.pushSettings()
	if err != nil {
		h.internalErr(w, err)
		return
	}
	id, token, err := h.notify.registerHub(r.Context(), ps.RelayURL, strings.TrimSpace(in.RegistrationToken))
	if err != nil {
		writeErr(w, 502, err.Error())
		return
	}
	ps.HubID, ps.HubToken, ps.Enabled = id, token, true
	h.savePushSettings(w, ps, true)
}

func (h *Hub) savePushSettings(w http.ResponseWriter, ps PushSettings, dropDevices bool) {
	if err := h.store.SetSetting(keyPush, ps); err != nil {
		h.internalErr(w, err)
		return
	}
	if dropDevices {
		if err := h.store.DeleteAllPushDevices(); err != nil {
			h.internalErr(w, err)
			return
		}
	}
	v, err := h.pushSettingsView()
	if err != nil {
		h.internalErr(w, err)
		return
	}
	writeJSON(w, 200, v)
}

// testPush checks the hub token at the relay.
func (h *Hub) testPush(w http.ResponseWriter, r *http.Request, _ *store.User) {
	ps, err := h.notify.pushSettings()
	if err != nil {
		h.internalErr(w, err)
		return
	}
	if ps.HubToken == "" {
		writeErr(w, 409, "this hub is not registered with the push relay yet")
		return
	}
	var out struct {
		HubID string `json:"hub_id"`
		FCM   bool   `json:"fcm"`
	}
	if err := h.notify.relayCall(r.Context(), http.MethodGet, ps.RelayURL, "/v1/hubs/me", ps.HubToken, nil, &out); err != nil {
		writeErr(w, 502, err.Error())
		return
	}
	if !out.FCM {
		writeErr(w, 502, "the push relay has no Firebase credentials, it cannot deliver notifications")
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "hub_id": out.HubID})
}

// ---------- API for the app ----------

// mobileConfig tells the app where to enroll its phone.
func (h *Hub) mobileConfig(w http.ResponseWriter, r *http.Request, _ *store.User) {
	ps, err := h.notify.pushSettings()
	if err != nil {
		h.internalErr(w, err)
		return
	}
	v := map[string]any{"enabled": ps.ready(), "relay_url": "", "hub_id": ""}
	if ps.ready() {
		v["relay_url"], v["hub_id"] = ps.RelayURL, ps.HubID
	}
	writeJSON(w, 200, v)
}

// requirePersonal accepts any signed-in user, also read-only ones and
// read-only tokens: phones are the user's own, not instance data.
func (h *Hub) requirePersonal(fn func(http.ResponseWriter, *http.Request, *store.User, *store.APIToken)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u, tok, err := h.authenticate(r)
		if err != nil {
			writeErr(w, http.StatusUnauthorized, err.Error())
			return
		}
		fn(w, r, u, tok)
	}
}

func (h *Hub) listMobileDevices(w http.ResponseWriter, r *http.Request, u *store.User, _ *store.APIToken) {
	list, err := h.store.UserPushDevices(u.ID)
	if err != nil {
		h.internalErr(w, err)
		return
	}
	writeJSON(w, 200, list)
}

// saveMobileDevice stores the device handle of the app's sign-in. Only OAuth
// access tokens can do it: the device lives as long as that sign-in.
func (h *Hub) saveMobileDevice(w http.ResponseWriter, r *http.Request, u *store.User, tok *store.APIToken) {
	if tok == nil || tok.GrantID == 0 {
		writeErr(w, 403, "only the pingtxt app can register a phone (sign in with OAuth)")
		return
	}
	var in struct {
		Device string `json:"device"`
		Name   string `json:"name"`
	}
	if readJSON(r, &in) != nil {
		writeErr(w, 400, "invalid body")
		return
	}
	in.Device = strings.TrimSpace(in.Device)
	if !strings.HasPrefix(in.Device, "prd_") || len(in.Device) > maxDeviceHandle {
		writeErr(w, 400, "device must be a handle from the push relay (prd_...)")
		return
	}
	in.Name = truncateUTF8(strings.TrimSpace(in.Name), maxDeviceName)
	if in.Name == "" {
		in.Name = "Phone"
	}
	ps, err := h.notify.pushSettings()
	if err != nil {
		h.internalErr(w, err)
		return
	}
	if !ps.ready() {
		writeErr(w, 409, "push notifications are off on this hub")
		return
	}
	id, err := h.store.SavePushDevice(u.ID, tok.GrantID, in.Device, in.Name)
	if err != nil {
		h.internalErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]int64{"id": id})
}

func (h *Hub) deleteMobileDevice(w http.ResponseWriter, r *http.Request, u *store.User, _ *store.APIToken) {
	id, ok := pathID(r)
	if !ok {
		writeErr(w, 400, "invalid id")
		return
	}
	if err := h.store.DeletePushDevice(u.ID, id); errors.Is(err, store.ErrNotFound) {
		writeErr(w, 404, "device not found")
		return
	} else if err != nil {
		h.internalErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}
