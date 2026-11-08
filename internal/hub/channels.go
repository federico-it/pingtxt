package hub

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// Channels that talk to a service API: Microsoft Teams, PagerDuty,
// Opsgenie, Pushover, Gotify, Matrix and Twilio (SMS).

// Default API addresses of the hosted services. Notifier.endpoints
// overrides them in tests.
var serviceEndpoints = map[string]string{
	"pagerduty":    "https://events.pagerduty.com",
	"pagerduty-eu": "https://events.eu.pagerduty.com",
	"opsgenie":     "https://api.opsgenie.com",
	"opsgenie-eu":  "https://api.eu.opsgenie.com",
	"pushover":     "https://api.pushover.net",
	"twilio":       "https://api.twilio.com",
}

func (n *Notifier) endpoint(name string) string {
	if u, ok := n.endpoints[name]; ok {
		return u
	}
	return serviceEndpoints[name]
}

// regional is the endpoint of a service in the configured region (us or eu).
func (n *Notifier) regional(service string, c map[string]string) string {
	if c["region"] == "eu" {
		return n.endpoint(service + "-eu")
	}
	return n.endpoint(service)
}

// link is the page to open from an alert: the diagnostics of the incident,
// else the monitor in the hub (with a public URL set), else "".
func (n *Notifier) link(e Event) string {
	if e.DiagnosticsURL != "" {
		return e.DiagnosticsURL
	}
	if e.MonitorID == 0 || n.publicURL == nil {
		return ""
	}
	return monitorURL(publicBase(n.publicURL()), e.MonitorID)
}

// resolves reports whether the event ends a problem (back up, no longer
// degraded, certificate or domain renewed, agent back online).
func (e Event) resolves() bool {
	return !e.Alert() && e.Status != StatusDegraded && e.Kind != KindAck
}

// alertKey identifies the problem an event belongs to, so incident tools
// (PagerDuty, Opsgenie) group its reminders and close it on recovery.
func (e Event) alertKey() string {
	if e.Test {
		return "pingtxt-test-" + strconv.FormatInt(e.At, 10)
	}
	switch e.Kind {
	case KindCert:
		return "pingtxt-cert-" + e.Subject
	case KindDomain:
		return "pingtxt-domain-" + e.Subject
	case KindAgent:
		return "pingtxt-agent-" + strconv.FormatInt(e.AgentID, 10)
	case KindDegraded:
		return "pingtxt-monitor-" + strconv.FormatInt(e.MonitorID, 10) + "-degraded"
	}
	return "pingtxt-monitor-" + strconv.FormatInt(e.MonitorID, 10)
}

// source is what the event is about: the target, domain or agent host.
func (e Event) source() string {
	switch {
	case e.Subject != "":
		return e.Subject
	case e.Target != "":
		return e.Target
	case e.Agent != "":
		return e.Agent
	}
	return "pingtxt"
}

// severity ranks the event: critical (down, agent offline), error
// (expired), warning (expiring, degraded) or info (resolved).
func (e Event) severity() string {
	switch {
	case e.Status == StatusDown || e.Status == AgentOffline:
		return "critical"
	case e.Status == ExpiryExpired:
		return "error"
	case e.Alert() || e.Status == StatusDegraded:
		return "warning"
	}
	return "info"
}

// shortText is the first line of the event text and its cause, for
// channels with little room (SMS, titles).
func (e Event) shortText(max int) string {
	lines := strings.Split(e.Text(), "\n")
	s := lines[0]
	if len(lines) > 1 && strings.HasPrefix(lines[1], "> ") {
		s += "\n" + strings.TrimPrefix(lines[1], "> ")
	}
	return truncate(s, max)
}

// truncate cuts s to at most max characters, ending with "…" when cut.
func truncate(s string, max int) string {
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	r := []rune(s)
	return string(r[:max-1]) + "…"
}

// ---------- Microsoft Teams ----------

// sendTeams posts an Adaptive Card to a Teams incoming webhook (a Workflows
// "post to a channel when a webhook request is received" URL, or an older
// connector URL).
func (n *Notifier) sendTeams(ctx context.Context, c map[string]string, e Event) error {
	color := "Good"
	switch e.severity() {
	case "critical", "error":
		color = "Attention"
	case "warning":
		color = "Warning"
	}
	body := []map[string]any{{"type": "TextBlock", "text": e.Title(), "weight": "Bolder", "size": "Medium", "color": color, "wrap": true}}
	// One block per line: Teams joins single newlines inside a block.
	for i, l := range strings.Split(e.Text(), "\n") {
		b := map[string]any{"type": "TextBlock", "text": l, "wrap": true}
		if i > 0 {
			b["spacing"] = "None"
		}
		if strings.HasPrefix(l, "> ") || strings.HasPrefix(l, "  ") {
			b["text"] = strings.TrimPrefix(l, "> ")
			b["isSubtle"] = true
		}
		body = append(body, b)
	}
	card := map[string]any{
		"$schema": "http://adaptivecards.io/schemas/adaptive-card.json",
		"type":    "AdaptiveCard",
		"version": "1.4",
		"body":    body,
		"msteams": map[string]any{"width": "Full"},
	}
	if u := n.link(e); u != "" {
		card["actions"] = []map[string]any{{"type": "Action.OpenUrl", "title": "Open in pingtxt", "url": u}}
	}
	msg := map[string]any{
		"type": "message",
		"attachments": []map[string]any{{
			"contentType": "application/vnd.microsoft.card.adaptive",
			"content":     card,
		}},
	}
	return n.post(ctx, c["url"], "application/json", mustJSON(msg), nil)
}

// ---------- PagerDuty ----------

// sendPagerDuty sends the event to the PagerDuty Events API v2: problems
// trigger an incident (reminders join it), recoveries resolve it. A test
// triggers and resolves at once.
func (n *Notifier) sendPagerDuty(ctx context.Context, c map[string]string, e Event) error {
	u := n.regional("pagerduty", c) + "/v2/enqueue"
	key := e.alertKey()
	if e.resolves() {
		return n.post(ctx, u, "application/json", mustJSON(map[string]any{
			"routing_key": c["routing_key"], "event_action": "resolve", "dedup_key": key,
		}), nil)
	}
	if e.Kind == KindAck {
		// Acknowledged in pingtxt: acknowledge the PagerDuty incident too.
		return n.post(ctx, u, "application/json", mustJSON(map[string]any{
			"routing_key": c["routing_key"], "event_action": "acknowledge", "dedup_key": key,
		}), nil)
	}
	details := map[string]any{"text": e.Text()}
	if e.Cause != "" {
		details["cause"] = e.Cause
	}
	if e.DownSince != 0 {
		details["down_since"] = time.UnixMilli(e.DownSince).UTC().Format(time.RFC3339)
	}
	if e.Reminder > 0 {
		details["reminder"] = e.Reminder
	}
	ev := map[string]any{
		"routing_key":  c["routing_key"],
		"event_action": "trigger",
		"dedup_key":    key,
		"client":       "pingtxt",
		"payload": map[string]any{
			"summary":        truncate(strings.TrimPrefix(e.Title(), "pingtxt: ")+": "+e.shortText(900), 1024),
			"source":         e.source(),
			"severity":       e.severity(),
			"timestamp":      time.UnixMilli(e.At).UTC().Format(time.RFC3339),
			"component":      e.Monitor,
			"class":          e.notifyEvent(),
			"custom_details": details,
		},
	}
	if l := n.link(e); l != "" {
		ev["links"] = []map[string]string{{"href": l, "text": "Open in pingtxt"}}
	}
	if err := n.post(ctx, u, "application/json", mustJSON(ev), nil); err != nil {
		return err
	}
	if e.Test {
		return n.post(ctx, u, "application/json", mustJSON(map[string]any{
			"routing_key": c["routing_key"], "event_action": "resolve", "dedup_key": key,
		}), nil)
	}
	return nil
}

// ---------- Opsgenie ----------

// sendOpsgenie creates an alert (reminders are counted by Opsgenie on the
// same alias) and closes it on recovery. A test creates and closes it.
func (n *Notifier) sendOpsgenie(ctx context.Context, c map[string]string, e Event) error {
	base := n.regional("opsgenie", c) + "/v2/alerts"
	hdr := map[string]string{"Authorization": "GenieKey " + c["api_key"]}
	key := e.alertKey()
	closeAlert := func() error {
		u := base + "/" + url.PathEscape(key) + "/close?identifierType=alias"
		return n.post(ctx, u, "application/json", mustJSON(map[string]string{"source": "pingtxt", "note": truncate(e.Text(), 25000)}), hdr)
	}
	if e.resolves() {
		return closeAlert()
	}
	if e.Kind == KindAck {
		// Acknowledged in pingtxt: acknowledge the Opsgenie alert too.
		u := base + "/" + url.PathEscape(key) + "/acknowledge?identifierType=alias"
		return n.post(ctx, u, "application/json", mustJSON(map[string]string{"source": "pingtxt", "user": e.AckBy, "note": truncate(e.Text(), 25000)}), hdr)
	}
	priority := "P3"
	switch e.severity() {
	case "critical":
		priority = "P1"
	case "error":
		priority = "P2"
	}
	details := map[string]string{"target": e.Target}
	if l := n.link(e); l != "" {
		details["link"] = l
	}
	if e.Cause != "" {
		details["cause"] = truncate(e.Cause, 500)
	}
	alert := map[string]any{
		"message":     truncate(strings.TrimPrefix(e.Title(), "pingtxt: "), 130),
		"alias":       key,
		"description": truncate(e.Text(), 15000),
		"priority":    priority,
		"source":      "pingtxt",
		"entity":      truncate(e.Monitor, 512),
		"tags":        []string{"pingtxt", e.notifyEvent()},
		"details":     details,
	}
	if err := n.post(ctx, base, "application/json", mustJSON(alert), hdr); err != nil {
		return err
	}
	if e.Test {
		return closeAlert()
	}
	return nil
}

// ---------- Pushover ----------

// sendPushover sends a message to a Pushover user or group, with high
// priority for problems (it bypasses quiet hours).
func (n *Notifier) sendPushover(ctx context.Context, c map[string]string, e Event) error {
	f := url.Values{
		"token":   {c["app_token"]},
		"user":    {c["user_key"]},
		"title":   {truncate(e.Title(), 250)},
		"message": {truncate(e.Text(), 1024)},
	}
	if e.Alert() {
		f.Set("priority", "1")
	} else {
		f.Set("priority", "0")
	}
	if c["device"] != "" {
		f.Set("device", c["device"])
	}
	if l := n.link(e); l != "" {
		f.Set("url", l)
		f.Set("url_title", "Open in pingtxt")
	}
	return n.post(ctx, n.endpoint("pushover")+"/1/messages.json", "application/x-www-form-urlencoded", []byte(f.Encode()), nil)
}

// ---------- Gotify ----------

// sendGotify posts a message to a Gotify server with an application token.
// Priority 8 (problems) makes the Android app ring, 5 (degraded) notifies,
// 2 (recoveries) stays silent.
func (n *Notifier) sendGotify(ctx context.Context, c map[string]string, e Event) error {
	priority := 2
	switch e.severity() {
	case "critical", "error":
		priority = 8
	case "warning":
		priority = 5
	}
	msg := map[string]any{"title": e.Title(), "message": e.Text(), "priority": priority}
	if l := n.link(e); l != "" {
		msg["extras"] = map[string]any{"client::notification": map[string]any{"click": map[string]string{"url": l}}}
	}
	u := strings.TrimRight(c["url"], "/") + "/message"
	return n.post(ctx, u, "application/json", mustJSON(msg), map[string]string{"X-Gotify-Key": c["token"]})
}

// ---------- Matrix ----------

// sendMatrix sends a text message to a Matrix room as the user of the
// access token (a bot account that joined the room). A room alias
// (#room:server) is looked up first.
func (n *Notifier) sendMatrix(ctx context.Context, c map[string]string, e Event) error {
	hs := strings.TrimRight(c["url"], "/")
	hdr := map[string]string{"Authorization": "Bearer " + c["access_token"]}
	room := strings.TrimSpace(c["room_id"])
	if strings.HasPrefix(room, "#") {
		var out struct {
			RoomID string `json:"room_id"`
		}
		if err := n.getJSON(ctx, hs+"/_matrix/client/v3/directory/room/"+url.PathEscape(room), hdr, &out); err != nil {
			return fmt.Errorf("room alias: %w", err)
		}
		if out.RoomID == "" {
			return errors.New("room alias: no room id")
		}
		room = out.RoomID
	}
	text := e.Text()
	html := "<b>" + htmlEscape(e.Title()) + "</b><br><pre>" + htmlEscape(text) + "</pre>"
	if l := n.link(e); l != "" {
		text += "\n" + l
		html += `<a href="` + htmlEscape(l) + `">Open in pingtxt</a>`
	}
	msg := map[string]string{"msgtype": "m.text", "body": text, "format": "org.matrix.custom.html", "formatted_body": html}
	u := hs + "/_matrix/client/v3/rooms/" + url.PathEscape(room) + "/send/m.room.message/" + txnID()
	return n.request(ctx, http.MethodPut, u, "application/json", mustJSON(msg), hdr)
}

func htmlEscape(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;").Replace(s)
}

// txnID is a unique Matrix transaction id, so a retried request is not
// posted twice.
func txnID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return "pingtxt" + strconv.FormatInt(time.Now().UnixMilli(), 10) + hex.EncodeToString(b)
}

// ---------- Twilio (SMS) ----------

// sendTwilio texts the event to each number with the Twilio Messages API.
// from is a phone number or a messaging service SID (MG...).
func (n *Notifier) sendTwilio(ctx context.Context, c map[string]string, e Event) error {
	sid := c["account_sid"]
	u := n.endpoint("twilio") + "/2010-04-01/Accounts/" + url.PathEscape(sid) + "/Messages.json"
	auth := "Basic " + basicAuth(sid, c["auth_token"])
	body := e.shortText(300)
	var errs []error
	for _, to := range splitList(c["to"]) {
		f := url.Values{"To": {to}, "Body": {body}}
		if from := c["from"]; strings.HasPrefix(from, "MG") {
			f.Set("MessagingServiceSid", from)
		} else {
			f.Set("From", from)
		}
		if err := n.post(ctx, u, "application/x-www-form-urlencoded", []byte(f.Encode()), map[string]string{"Authorization": auth}); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", to, err))
		}
	}
	return errors.Join(errs...)
}

// ---------- validation ----------

// validateServiceConfig checks the settings of the channels above. It
// reports false for other types.
func validateServiceConfig(typ string, c map[string]string) (bool, error) {
	for k, v := range c {
		c[k] = strings.TrimSpace(v)
	}
	need := func(keys ...string) error {
		for _, k := range keys {
			if c[k] == "" {
				return fmt.Errorf("%s is required", strings.ReplaceAll(k, "_", " "))
			}
		}
		return nil
	}
	region := func() error {
		if c["region"] == "" || c["region"] == "us" {
			delete(c, "region")
			return nil
		}
		if c["region"] != "eu" {
			return errors.New("region must be us or eu")
		}
		return nil
	}
	switch typ {
	case "pagerduty":
		if err := need("routing_key"); err != nil {
			return true, err
		}
		return true, region()
	case "opsgenie":
		if err := need("api_key"); err != nil {
			return true, err
		}
		return true, region()
	case "pushover":
		return true, need("app_token", "user_key")
	case "gotify":
		if err := validHTTPURL(c["url"]); err != nil {
			return true, err
		}
		return true, need("token")
	case "matrix":
		if err := validHTTPURL(c["url"]); err != nil {
			return true, errors.New("a valid homeserver url is required")
		}
		if err := need("access_token", "room_id"); err != nil {
			return true, err
		}
		if r := c["room_id"]; (r[0] != '!' && r[0] != '#') || !strings.Contains(r, ":") {
			return true, errors.New("room must be a room id (!abc:example.org) or alias (#room:example.org)")
		}
		return true, nil
	case "twilio":
		if err := need("account_sid", "auth_token", "from", "to"); err != nil {
			return true, err
		}
		if !strings.HasPrefix(c["account_sid"], "AC") {
			return true, errors.New("account sid must start with AC")
		}
		for _, to := range splitList(c["to"]) {
			if !phoneRe.MatchString(to) {
				return true, fmt.Errorf("%q is not a phone number in E.164 format (+391234567890)", to)
			}
		}
		if f := c["from"]; !strings.HasPrefix(f, "MG") && !phoneRe.MatchString(f) {
			return true, errors.New("from must be a phone number in E.164 format or a messaging service sid (MG...)")
		}
		return true, nil
	}
	return false, nil
}

// phoneRe is a phone number in E.164 format.
var phoneRe = regexp.MustCompile(`^\+[1-9][0-9]{6,14}$`)

func basicAuth(user, pass string) string {
	return base64.StdEncoding.EncodeToString([]byte(user + ":" + pass))
}

func validHTTPURL(s string) error {
	u, err := url.Parse(s)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return errors.New("a valid http(s) url is required")
	}
	return nil
}
