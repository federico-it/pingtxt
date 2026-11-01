package hub

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"html/template"
	"mime"
	"mime/quotedprintable"
	"strings"
	"time"
)

// Every email the hub sends shares one look, the one of the web UI: a
// monospace terminal page with dashed rules and the green / red / yellow
// status colors, in light and dark. Each message is multipart/alternative
// with a plain text part laid out for a monospace font and an HTML part
// built from tables and inline styles, which is what mail clients render
// reliably. The dark palette is applied with prefers-color-scheme by the
// clients that support it (Apple Mail, iOS, Outlook, Thunderbird...).

// Mail tones, which color the badge, the note and the button.
const (
	toneUp   = "up"
	toneDown = "down"
	toneWarn = "warn"
	toneInfo = "info"
)

// Light palette (the dark one is in the <style> of mailLayout).
var mailTones = map[string]string{
	toneUp:   "#1b9247",
	toneDown: "#d72828",
	toneWarn: "#a86f00",
	toneInfo: "#3275b4",
	"":       "#6a6966",
}

type mailRow struct {
	Key, Value string
	Tone       string // colors the value; "" for the normal text color
	Href       string // makes the value a link
}

type mailLink struct {
	Label, Href string
}

// mailView is the content of one email.
type mailView struct {
	Preheader string // inbox preview text, hidden in the message
	Command   string // shown as "$ <command>" above the title
	Tone      string
	Badge     string // e.g. "DOWN", shown before the title in the tone color
	Title     string
	Lead      string // a paragraph under the title
	Note      string // quoted text (a cause, an update message), kept as written
	Rows      []mailRow
	Action    *mailLink // the main button
	Footer    []string
	FootLinks []mailLink
	// Data is passed to the "extra" template of a message that has one
	// (the report tables).
	Data any
}

func (v mailView) heading() string {
	if v.Badge == "" {
		return v.Title
	}
	return "[" + v.Badge + "] " + v.Title
}

var mailFuncs = template.FuncMap{
	"tone": func(t string) template.CSS {
		if c, ok := mailTones[t]; ok {
			return template.CSS(c)
		}
		return template.CSS(mailTones[""])
	},
}

// mailLayout is the HTML page; a message can add content after the rows by
// defining "extra" in a clone (see reportMailHTML).
var mailLayout = template.Must(template.New("mail").Funcs(mailFuncs).Parse(`<!doctype html>
<html lang="en"><head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<meta name="color-scheme" content="light dark">
<meta name="supported-color-schemes" content="light dark">
<title>{{.Title}}</title>
<style>
:root{color-scheme:light dark;supported-color-schemes:light dark}
body{margin:0;padding:0;-webkit-text-size-adjust:100%}
table{border-collapse:collapse}
a{color:inherit}
@media (prefers-color-scheme:dark){
.pt-bg{background:#0b0d0b!important}
.pt-fg{color:#dae0da!important}
.pt-mu{color:#828883!important}
.pt-bd{border-color:#2b2f2c!important}
.pt-box{background:#171917!important}
.pt-up{color:#56d57b!important}
.pt-down{color:#fc5855!important}
.pt-warn{color:#f0bb3b!important}
.pt-info{color:#55aee8!important}
.pt-bl-up{border-left-color:#56d57b!important}
.pt-bl-down{border-left-color:#fc5855!important}
.pt-bl-warn{border-left-color:#f0bb3b!important}
.pt-bl-info{border-left-color:#55aee8!important}
.pt-bl-{border-left-color:#828883!important}
.pt-btn{background:#6de18b!important;color:#0c0e0c!important;border-color:#6de18b!important}
}
@media (max-width:600px){
.pt-pad{padding-left:16px!important;padding-right:16px!important}
.pt-hide{display:none!important}
.pt-key{width:96px!important}
}
</style>
</head>
<body class="pt-bg" style="margin:0;padding:0;background:#fafaf9">
{{if .Preheader}}<div style="display:none;max-height:0;max-width:0;overflow:hidden;opacity:0;mso-hide:all">{{.Preheader}}</div>{{end}}
<table role="presentation" width="100%" cellpadding="0" cellspacing="0" border="0" class="pt-bg" bgcolor="#fafaf9" style="background:#fafaf9">
<tr><td align="center" style="padding:24px 0 32px">
<table role="presentation" width="100%" cellpadding="0" cellspacing="0" border="0" class="pt-fg" style="max-width:680px;color:#171613;font-family:'JetBrains Mono',ui-monospace,SFMono-Regular,Menlo,Consolas,'Liberation Mono',monospace;font-size:13px;line-height:1.6">
<tr><td class="pt-pad pt-bd" style="padding:0 24px 14px;border-bottom:1px dashed #d8d7d4">
<table role="presentation" width="100%" cellpadding="0" cellspacing="0" border="0"><tr>
<td class="pt-fg" style="font-weight:700;color:#171613;font-size:14px">&gt; pingtxt<span class="pt-up" style="color:#1b9247">_</span></td>
{{if .Badge}}<td align="right" class="pt-{{.Tone}}" style="color:{{tone .Tone}};font-size:12px">&#9679; {{.Badge}}</td>{{end}}
</tr></table>
</td></tr>
<tr><td class="pt-pad" style="padding:28px 24px 8px">
{{if .Command}}<div class="pt-mu" style="color:#6a6966;font-size:12px">$ {{.Command}}</div>{{end}}
<h1 class="pt-fg" style="margin:4px 0 0;font-size:18px;line-height:1.4;font-weight:700;color:#171613;word-break:break-word">{{if .Badge}}<span class="pt-{{.Tone}}" style="color:{{tone .Tone}}">[{{.Badge}}]</span> {{end}}{{.Title}}</h1>
{{if .Lead}}<p style="margin:12px 0 0">{{.Lead}}</p>{{end}}
{{if .Note}}<div class="pt-box pt-bl-{{.Tone}}" style="margin:18px 0 0;padding:10px 14px;background:#efeeec;border-left:2px solid {{tone .Tone}};white-space:pre-wrap;word-break:break-word">{{.Note}}</div>{{end}}
{{if .Rows}}<table role="presentation" width="100%" cellpadding="0" cellspacing="0" border="0" style="margin:20px 0 0">
{{range .Rows}}<tr><td class="pt-mu pt-bd pt-key" valign="top" style="width:128px;padding:7px 12px 7px 0;color:#6a6966;border-top:1px dashed #d8d7d4;white-space:nowrap">{{.Key}}</td><td class="pt-bd{{if .Tone}} pt-{{.Tone}}{{end}}" valign="top" style="padding:7px 0;border-top:1px dashed #d8d7d4;word-break:break-word{{if .Tone}};color:{{tone .Tone}};font-weight:700{{end}}">{{if .Href}}<a href="{{.Href}}" class="pt-fg" style="color:#171613;text-decoration:underline">{{.Value}}</a>{{else}}{{.Value}}{{end}}</td></tr>
{{end}}<tr><td class="pt-bd" colspan="2" style="border-top:1px dashed #d8d7d4;font-size:0;line-height:0">&nbsp;</td></tr>
</table>{{end}}
{{block "extra" .Data}}{{end}}
{{if .Action}}<table role="presentation" cellpadding="0" cellspacing="0" border="0" style="margin:24px 0 0"><tr><td class="pt-btn" bgcolor="#171613" style="background:#171613;border:1px solid #171613">
<a href="{{.Action.Href}}" class="pt-btn" style="display:inline-block;padding:8px 16px;color:#fafaf9;background:#171613;text-decoration:none;font-weight:700">{{.Action.Label}} &rarr;</a>
</td></tr></table>{{end}}
</td></tr>
<tr><td class="pt-pad" style="padding:28px 24px 0">
<div class="pt-mu pt-bd" style="border-top:1px dashed #d8d7d4;padding-top:12px;color:#6a6966;font-size:11px;line-height:1.7;word-break:break-word">
{{range .Footer}}<div>{{.}}</div>{{end}}
<div>{{range .FootLinks}}<a href="{{.Href}}" class="pt-mu" style="color:#6a6966;text-decoration:underline">{{.Label}}</a> &middot; {{end}}sent by pingtxt</div>
</div>
</td></tr>
</table>
</td></tr>
</table>
</body></html>`))

// renderHTML renders the view with the layout, or with t (a clone of
// mailLayout with an "extra" block) when set.
func (v mailView) renderHTML(t *template.Template) string {
	if t == nil {
		t = mailLayout
	}
	var b bytes.Buffer
	if err := t.Execute(&b, v); err != nil {
		// Only a bug in a template gets here: fall back to the text.
		return "<pre>" + template.HTMLEscapeString(v.renderText("")) + "</pre>"
	}
	return b.String()
}

// renderText is the plain text alternative, laid out like the HTML. extra
// is put after the rows (the report tables).
func (v mailView) renderText(extra string) string {
	var b strings.Builder
	if v.Command != "" {
		b.WriteString("$ " + v.Command + "\n")
	}
	b.WriteString(v.heading() + "\n")
	if v.Lead != "" {
		b.WriteString("\n" + v.Lead + "\n")
	}
	if v.Note != "" {
		b.WriteString("\n")
		for _, l := range strings.Split(strings.TrimRight(v.Note, "\n"), "\n") {
			b.WriteString("> " + l + "\n")
		}
	}
	if len(v.Rows) > 0 {
		w := 0
		for _, r := range v.Rows {
			w = max(w, len([]rune(r.Key)))
		}
		b.WriteString("\n")
		for _, r := range v.Rows {
			b.WriteString(padRight(r.Key, w) + "  " + r.Value + "\n")
		}
	}
	if extra != "" {
		b.WriteString("\n" + strings.TrimRight(extra, "\n") + "\n")
	}
	if v.Action != nil {
		b.WriteString("\n" + v.Action.Label + ": " + v.Action.Href + "\n")
	}
	b.WriteString("\n-- \n")
	for _, f := range v.Footer {
		b.WriteString(f + "\n")
	}
	for _, l := range v.FootLinks {
		if strings.Contains(l.Href, l.Label) {
			b.WriteString(l.Href + "\n") // the label is the address
		} else {
			b.WriteString(l.Label + ": " + l.Href + "\n")
		}
	}
	b.WriteString("sent by pingtxt\n")
	return b.String()
}

// mailHeaders are the per-message headers of writeMail.
type mailHeaders struct {
	to          string // the To header
	subject     string
	unsubscribe string // one-click unsubscribe URL (RFC 8058), optional
}

// mailMsg is a rendered email: SMTP sends it as MIME (writeMail), Resend
// as JSON.
type mailMsg struct {
	hd   mailHeaders
	text string
	html string
	date time.Time
}

func newMail(hd mailHeaders, text, html string, now time.Time) mailMsg {
	return mailMsg{hd: hd, text: text, html: html, date: now}
}

// writeMail renders a multipart/alternative message (plain text and HTML)
// from the channel's sender.
func writeMail(ec *emailConfig, m mailMsg) []byte {
	hd, text, html, now := m.hd, m.text, m.html, m.date
	domain := ec.From.Address[strings.LastIndexByte(ec.From.Address, '@')+1:]
	rb := make([]byte, 12)
	_, _ = rand.Read(rb)
	boundary := "pingtxt-" + hex.EncodeToString(rb)
	var b bytes.Buffer
	hdr := func(k, v string) { b.WriteString(k + ": " + v + "\r\n") }
	hdr("From", ec.From.String())
	hdr("To", hd.to)
	hdr("Subject", mime.QEncoding.Encode("utf-8", hd.subject))
	hdr("Date", now.Format(time.RFC1123Z))
	hdr("Message-ID", newMessageID(domain))
	hdr("MIME-Version", "1.0")
	hdr("Content-Type", `multipart/alternative; boundary="`+boundary+`"`)
	hdr("Auto-Submitted", "auto-generated")
	if hd.unsubscribe != "" {
		// RFC 8058 one-click unsubscribe, offered by mail clients.
		hdr("List-Unsubscribe", "<"+hd.unsubscribe+">")
		hdr("List-Unsubscribe-Post", "List-Unsubscribe=One-Click")
	}
	b.WriteString("\r\n")
	part := func(ctype, body string) {
		b.WriteString("--" + boundary + "\r\n")
		b.WriteString("Content-Type: " + ctype + "; charset=utf-8\r\n")
		b.WriteString("Content-Transfer-Encoding: quoted-printable\r\n\r\n")
		qp := quotedprintable.NewWriter(&b)
		_, _ = qp.Write([]byte(strings.ReplaceAll(strings.ReplaceAll(body, "\r\n", "\n"), "\n", "\r\n")))
		_ = qp.Close()
		b.WriteString("\r\n")
	}
	part("text/plain", text)
	part("text/html", html)
	b.WriteString("--" + boundary + "--\r\n")
	return b.Bytes()
}

// mailTime formats a time for the emails, in UTC.
func mailTime(ms int64) string {
	return time.UnixMilli(ms).UTC().Format("2006-01-02 15:04:05 UTC")
}

// publicBase is the hub address of the public URL setting, with a scheme
// and no trailing slash, or "" when it is not set.
func publicBase(u string) string {
	u = strings.TrimRight(strings.TrimSpace(u), "/")
	if u == "" {
		return ""
	}
	if !strings.Contains(u, "://") {
		u = "https://" + u
	}
	return u
}

// monitorURL links a monitor in the hub, or "" without a public URL.
func monitorURL(base string, id int64) string {
	if base == "" || id <= 0 {
		return ""
	}
	return fmt.Sprintf("%s/monitors/view/?id=%d", base, id)
}
