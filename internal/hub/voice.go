package hub

import (
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"crypto/subtle"
	"encoding/base64"
	"encoding/xml"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/federico-it/pingtxt/internal/store"
)

// Voice calls through Twilio. A Twilio channel with mode "call" (or "both",
// SMS too) calls its numbers for bad news (down, expiry, agent offline):
// Twilio reads the alert out loud. When the hub has a public URL and the
// alert belongs to an incident, the call asks to press 1 to acknowledge it:
// Twilio posts the key to /api/voice/{channel}/{incident}, signed with the
// channel's auth token, and the incident is acknowledged by the admin whose
// account has the called number.

// twilioMode is how a Twilio channel reaches its numbers: sms, call or both.
func twilioMode(c map[string]string) string {
	switch c["mode"] {
	case "call", "both":
		return c["mode"]
	}
	return "sms"
}

// voiceText holds the sentences of a call in one language.
type voiceText struct {
	lang, test, alert, down, downFor, press, acked, notAdmin, resolved, gone, bye string
}

var voiceTexts = map[string]voiceText{
	"en": {
		lang: "en-US", test: "This is a test call from pingtxt.", alert: "Pingtxt alert.",
		down: "%s is down.", downFor: "It has been down for %s.",
		press: "Press 1 to acknowledge the incident.", acked: "Acknowledged. Reminders and escalation stop. Goodbye.",
		notAdmin: "This phone number does not belong to an admin of pingtxt, so it cannot acknowledge the incident. Goodbye.",
		resolved: "The incident is already resolved. Goodbye.", gone: "The incident no longer exists. Goodbye.", bye: "Goodbye.",
	},
	"it": {
		lang: "it-IT", test: "Questa è una chiamata di prova di pingtxt.", alert: "Avviso di pingtxt.",
		down: "%s non funziona.", downFor: "È giù da %s.",
		press: "Premi 1 per prendere in carico l'incidente.", acked: "Preso in carico. Promemoria ed escalation si fermano. Arrivederci.",
		notAdmin: "Questo numero non appartiene a un amministratore di pingtxt, quindi non può prendere in carico l'incidente. Arrivederci.",
		resolved: "L'incidente è già risolto. Arrivederci.", gone: "L'incidente non esiste più. Arrivederci.", bye: "Arrivederci.",
	},
}

func voiceLang(c map[string]string) voiceText {
	if t, ok := voiceTexts[c["language"]]; ok {
		return t
	}
	return voiceTexts["en"]
}

// spokenDuration reads a duration as words ("2 hours 5 minutes").
func spokenDuration(d time.Duration, lang string) string {
	m := int(d / time.Minute)
	h, m := m/60, m%60
	unit := func(n int, one, many string) string {
		if n == 1 {
			return "1 " + one
		}
		return strconv.Itoa(n) + " " + many
	}
	var parts []string
	if lang == "it-IT" {
		if h > 0 {
			parts = append(parts, unit(h, "ora", "ore"))
		}
		if m > 0 || h == 0 {
			parts = append(parts, unit(m, "minuto", "minuti"))
		}
		return strings.Join(parts, " e ")
	}
	if h > 0 {
		parts = append(parts, unit(h, "hour", "hours"))
	}
	if m > 0 || h == 0 {
		parts = append(parts, unit(m, "minute", "minutes"))
	}
	return strings.Join(parts, " ")
}

// speech is what a call says about an event.
func (e Event) speech(t voiceText) string {
	var parts []string
	if e.Test {
		parts = append(parts, t.test)
	}
	parts = append(parts, t.alert)
	switch {
	case e.Kind == KindStatus && e.Status == StatusDown:
		parts = append(parts, fmt.Sprintf(t.down, e.Monitor))
		if e.DownSince > 0 && e.At-e.DownSince >= 60_000 {
			parts = append(parts, fmt.Sprintf(t.downFor, spokenDuration(time.Duration(e.At-e.DownSince)*time.Millisecond, t.lang)))
		}
		if c := e.phoneCause(); c != "" {
			parts = append(parts, truncateRunes(c, 200)+".")
		}
	default:
		parts = append(parts, strings.TrimPrefix(e.Title(), "pingtxt: ")+".")
	}
	return strings.Join(parts, " ")
}

func truncateRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n])
}

func xmlText(s string) string {
	var b strings.Builder
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}

// callTwiML is the script of an alert call. With ackURL the callee can
// press 1 to acknowledge.
func callTwiML(e Event, t voiceText, ackURL string) string {
	say := `<Say language="` + t.lang + `">` + xmlText(e.speech(t)) + `</Say>`
	if ackURL == "" {
		return `<Response>` + say + `<Pause length="1"/>` + say + `</Response>`
	}
	press := `<Say language="` + t.lang + `">` + xmlText(t.press) + `</Say>`
	return `<Response><Gather numDigits="1" timeout="8" action="` + xmlText(ackURL) + `" method="POST">` +
		say + press + `<Pause length="1"/>` + say + press + `</Gather><Say language="` + t.lang + `">` + xmlText(t.bye) + `</Say></Response>`
}

// voiceAckURL is where a call posts the key pressed, or "" when the call
// cannot acknowledge (no public URL, no incident, not a down alert).
func (n *Notifier) voiceAckURL(e Event, notifierID int64) string {
	if e.Test || e.Kind != KindStatus || e.Status != StatusDown || e.IncidentID == 0 || notifierID == 0 || n.publicURL == nil {
		return ""
	}
	base := publicBase(n.publicURL())
	if base == "" {
		return ""
	}
	return fmt.Sprintf("%s/api/voice/%d/%d", base, notifierID, e.IncidentID)
}

// callTwilio calls one number with the Twilio Calls API.
func (n *Notifier) callTwilio(ctx context.Context, c map[string]string, e Event, notifierID int64, to string) error {
	sid := c["account_sid"]
	u := n.endpoint("twilio") + "/2010-04-01/Accounts/" + url.PathEscape(sid) + "/Calls.json"
	f := url.Values{"To": {to}, "From": {c["from"]}, "Twiml": {callTwiML(e, voiceLang(c), n.voiceAckURL(e, notifierID))}}
	return n.post(ctx, u, "application/x-www-form-urlencoded", []byte(f.Encode()), map[string]string{"Authorization": "Basic " + basicAuth(sid, c["auth_token"])})
}

// ---------- callback ----------

// twilioSignature is Twilio's request signature: HMAC-SHA1 of the URL and
// the POST parameters sorted by name, with the auth token as key.
func twilioSignature(token, u string, form url.Values) string {
	keys := make([]string, 0, len(form))
	for k := range form {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString(u)
	for _, k := range keys {
		for _, v := range form[k] {
			b.WriteString(k)
			b.WriteString(v)
		}
	}
	mac := hmac.New(sha1.New, []byte(token))
	mac.Write([]byte(b.String()))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

// voiceChannel returns the Twilio settings a channel calls with: its own,
// or those of the Twilio channel of an oncall channel.
func (h *Hub) voiceChannel(id int64) (map[string]string, error) {
	nt, err := h.store.Notifier(id)
	if err != nil {
		return nil, err
	}
	switch nt.Type {
	case "twilio":
		return nt.Config, nil
	case "oncall":
		tid, _ := strconv.ParseInt(nt.Config["twilio_channel"], 10, 64)
		tw, err := h.store.Notifier(tid)
		if err != nil || tw.Type != "twilio" {
			return nil, store.ErrNotFound
		}
		return tw.Config, nil
	}
	return nil, store.ErrNotFound
}

func writeTwiML(w http.ResponseWriter, t voiceText, text string) {
	w.Header().Set("Content-Type", "text/xml; charset=utf-8")
	fmt.Fprintf(w, `<?xml version="1.0" encoding="UTF-8"?><Response><Say language="%s">%s</Say></Response>`, t.lang, xmlText(text))
}

// voiceCallback receives the key pressed during an alert call (Twilio
// <Gather>). 1 acknowledges the incident for the admin whose account has
// the called number.
func (h *Hub) voiceCallback(w http.ResponseWriter, r *http.Request) {
	nid, err1 := strconv.ParseInt(r.PathValue("channel"), 10, 64)
	iid, err2 := strconv.ParseInt(r.PathValue("incident"), 10, 64)
	if err1 != nil || err2 != nil {
		writeErr(w, 400, "invalid id")
		return
	}
	c, err := h.voiceChannel(nid)
	if err != nil {
		writeErr(w, 404, "channel not found")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	if err := r.ParseForm(); err != nil {
		writeErr(w, 400, "invalid form")
		return
	}
	base := publicBase(h.generalSettings().PublicURL)
	want := twilioSignature(c["auth_token"], base+r.URL.Path, r.PostForm)
	if base == "" || c["auth_token"] == "" || subtle.ConstantTimeCompare([]byte(want), []byte(r.Header.Get("X-Twilio-Signature"))) != 1 {
		writeErr(w, 403, "invalid signature")
		return
	}
	t := voiceLang(c)
	if r.PostForm.Get("Digits") != "1" {
		writeTwiML(w, t, t.bye)
		return
	}
	u, err := h.store.UserByPhone(r.PostForm.Get("To"))
	if err != nil || !u.IsAdmin() {
		writeTwiML(w, t, t.notAdmin)
		return
	}
	_, err = h.AckIncident(iid, u, true)
	switch {
	case errors.Is(err, errIncidentNotFound):
		writeTwiML(w, t, t.gone)
		return
	case errors.Is(err, errIncidentResolved):
		writeTwiML(w, t, t.resolved)
		return
	case err != nil:
		h.log.Error("ack by phone", "err", err)
		writeTwiML(w, t, t.bye)
		return
	}
	h.auditEvent(r, u, nil, "incident.ack", store.AuditOK, map[string]any{"_via": "phone call", "_incident": iid})
	writeTwiML(w, t, t.acked)
}
