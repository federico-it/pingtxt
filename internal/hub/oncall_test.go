package hub

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/federico-it/pingtxt/internal/store"
)

func testUsers(n int) []*store.User {
	var out []*store.User
	for i := 1; i <= n; i++ {
		out = append(out, &store.User{ID: int64(i), Email: fmt.Sprintf("u%d@x", i), Role: store.RoleAdmin})
	}
	return out
}

// Weekly shifts change at the same local time across a DST change.
func TestRotationWeeklyDST(t *testing.T) {
	rome, _ := time.LoadLocation("Europe/Rome")
	start := time.Date(2026, 10, 12, 9, 0, 0, 0, rome) // Monday 9:00, summer time
	sc := &store.Schedule{ID: 1, Name: "ops", Timezone: "Europe/Rome", Members: []int64{1, 2, 3}, ShiftLen: 1, ShiftUnit: store.ShiftWeeks, StartAt: start.UnixMilli()}
	r := newRotation(sc, testUsers(3), nil)

	cases := []struct {
		at   time.Time
		want int64
	}{
		{start.Add(-time.Minute), 0},
		{start, 1},
		{time.Date(2026, 10, 19, 8, 59, 0, 0, rome), 1},
		{time.Date(2026, 10, 19, 9, 0, 0, 0, rome), 2},
		// Winter time from October 25: the change is still at 9:00 local.
		{time.Date(2026, 10, 26, 8, 59, 0, 0, rome), 2},
		{time.Date(2026, 10, 26, 9, 0, 0, 0, rome), 3},
		{time.Date(2026, 11, 2, 9, 0, 0, 0, rome), 1},
	}
	for _, c := range cases {
		u, _ := r.at(c.at)
		var got int64
		if u != nil {
			got = u.ID
		}
		if got != c.want {
			t.Errorf("%s: on call %d, want %d", c.at, got, c.want)
		}
	}

	// Overrides win, the newest where they overlap.
	from := time.Date(2026, 10, 20, 0, 0, 0, 0, rome)
	r.overrides = []*store.Override{
		{ID: 1, UserID: 3, StartAt: from.UnixMilli(), EndAt: from.Add(48 * time.Hour).UnixMilli()},
		{ID: 2, UserID: 1, StartAt: from.Add(24 * time.Hour).UnixMilli(), EndAt: from.Add(36 * time.Hour).UnixMilli()},
	}
	got := r.shifts(from.Add(-time.Hour), from.Add(72*time.Hour))
	var s []string
	for _, sh := range got {
		s = append(s, fmt.Sprintf("%d/%v/%s", sh.UserID, sh.Override, time.UnixMilli(sh.Start).In(rome).Format("02 15:04")))
	}
	if j := strings.Join(s, " "); j != "2/false/19 23:00 3/true/20 00:00 1/true/21 00:00 3/true/21 12:00 2/false/22 00:00" {
		t.Fatalf("shifts: %s", j)
	}
}

func TestRotationHours(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	sc := &store.Schedule{Timezone: "UTC", Members: []int64{1, 2, 9}, ShiftLen: 12, ShiftUnit: store.ShiftHours, StartAt: start.UnixMilli()}
	r := newRotation(sc, testUsers(2), nil) // user 9 was deleted: skipped
	for i, want := range []int64{1, 2, 1, 2} {
		u, _ := r.at(start.Add(time.Duration(i)*12*time.Hour + time.Hour))
		if u == nil || u.ID != want {
			t.Fatalf("shift %d: %+v, want %d", i, u, want)
		}
	}
	if got := r.shifts(start, start.Add(48*time.Hour)); len(got) != 4 || got[3].End != start.Add(48*time.Hour).UnixMilli() {
		t.Fatalf("shifts: %+v", got)
	}
}

func TestOnCallAPIAndChannel(t *testing.T) {
	h, st := newTestHub(t)
	c := newAPIClient(t, h, st) // creates the admin a@b.c
	admin, err := st.UserByEmail("a@b.c")
	must(t, err)
	bob, err := st.CreateUser("bob@b.c", "", store.RoleViewer)
	must(t, err)

	// Phone numbers are personal.
	var me struct {
		Phone string `json:"phone"`
	}
	if code := c.json("PUT", "/api/account/phone", `{"phone":"333 123"}`, nil); code != 400 {
		t.Fatalf("bad phone: %d", code)
	}
	if code := c.json("PUT", "/api/account/phone", `{"phone":"+39 333 123 4567"}`, &me); code != 200 || me.Phone != "+393331234567" {
		t.Fatalf("phone: %d %+v", code, me)
	}
	must(t, st.SetUserPhone(bob.ID, "+393330000000"))

	// A schedule where bob is on call now.
	now := time.Now()
	body := fmt.Sprintf(`{"name":"ops","timezone":"Europe/Rome","members":[%d,%d],"shift_len":1,"shift_unit":"weeks","start_at":%d}`, bob.ID, admin.ID, now.Add(-time.Hour).UnixMilli())
	var sv struct {
		ID       int64
		Current  *onCallShift `json:"current"`
		Next     *onCallShift `json:"next"`
		Channels []string     `json:"channels"`
	}
	if code := c.json("POST", "/api/oncall", body, &sv); code != 201 || sv.Current == nil || sv.Current.UserID != bob.ID || sv.Next == nil || sv.Next.UserID != admin.ID {
		t.Fatalf("create: %d %+v", code, sv)
	}
	for _, bad := range []string{`{"name":""}`, `{"name":"x","members":[]}`, `{"name":"x","members":[999]}`, `{"name":"x","members":[1],"shift_unit":"months"}`, `{"name":"x","members":[1],"timezone":"Mars/Base"}`, `{"name":"x","members":[1],"shift_len":13}`} {
		if code := c.json("POST", "/api/oncall", bad, nil); code != 400 {
			t.Errorf("%s: %d", bad, code)
		}
	}
	path := "/api/oncall/" + strconv.FormatInt(sv.ID, 10)
	// PUT keeps what the body leaves out.
	if code := c.json("PUT", path, `{"description":"backend"}`, &sv); code != 200 || sv.Current.UserID != bob.ID {
		t.Fatalf("update: %d %+v", code, sv)
	}
	sc, err := st.Schedule(sv.ID)
	must(t, err)
	if sc.Description != "backend" || sc.Timezone != "Europe/Rome" || len(sc.Members) != 2 {
		t.Fatalf("stored: %+v", sc)
	}

	// Twilio and the on-call channel that sends through it.
	var tc capture
	tsrv := tc.server(t, func(*http.Request) (int, string) { return 201, `{"sid":"X"}` })
	h.notify.endpoints = map[string]string{"twilio": tsrv.URL}
	h.smu.Lock()
	h.general.PublicURL = "https://hub.example.com"
	h.smu.Unlock()
	tw := &store.Notifier{Name: "twilio", Type: "twilio", Enabled: true, Config: map[string]string{"account_sid": "AC1", "auth_token": "tok", "from": "+15550001111", "to": "+390000000"}}
	must(t, validateNotifier(tw))
	must(t, st.CreateNotifier(tw))
	var oc store.Notifier
	ocBody := fmt.Sprintf(`{"name":"on call","type":"oncall","enabled":true,"config":{"schedule_id":"%d","app":"off","twilio_channel":"%d","twilio_mode":"both"},"events":["down","up"],"all_monitors":false}`, sv.ID, tw.ID)
	if code := c.json("POST", "/api/notifiers", ocBody, &oc); code != 201 {
		t.Fatalf("oncall channel: %d", code)
	}
	if code := c.json("POST", "/api/notifiers", `{"name":"x","type":"oncall","config":{"schedule_id":"999","twilio_channel":"1"}}`, nil); code != 400 {
		t.Fatalf("missing schedule: %d", code)
	}
	if code := c.json("DELETE", "/api/notifiers/"+strconv.FormatInt(tw.ID, 10), "", nil); code != 409 {
		t.Fatalf("delete used twilio channel: %d", code)
	}
	if code := c.json("DELETE", path, "", nil); code != 409 {
		t.Fatalf("delete used schedule: %d", code)
	}

	// A down alert texts and calls bob, with press 1 to acknowledge.
	ev := Event{MonitorID: 3, Monitor: "api", Target: "x", Status: StatusDown, Cause: "timeout", At: now.UnixMilli(), DownSince: now.Add(-5 * time.Minute).UnixMilli(), IncidentID: 42}
	ocStored, err := st.Notifier(oc.ID)
	must(t, err)
	if err := h.notify.Deliver(context.Background(), ocStored, ev); err != nil {
		t.Fatal(err)
	}
	reqs := tc.all()
	if len(reqs) != 2 || reqs[0].Path != "/2010-04-01/Accounts/AC1/Calls.json" || reqs[1].Path != "/2010-04-01/Accounts/AC1/Messages.json" {
		t.Fatalf("requests: %+v", reqs)
	}
	call := reqs[0].form(t)
	twiml := call.Get("Twiml")
	wantURL := fmt.Sprintf("https://hub.example.com/api/voice/%d/42", oc.ID)
	if call.Get("To") != "+393330000000" || !strings.Contains(twiml, "api is down.") || !strings.Contains(twiml, "It has been down for 5 minutes.") ||
		!strings.Contains(twiml, `action="`+wantURL+`"`) || !strings.Contains(twiml, "Press 1") {
		t.Fatalf("call: %v", call)
	}
	if reqs[1].form(t).Get("To") != "+393330000000" {
		t.Fatalf("sms: %v", reqs[1].form(t))
	}
	// A recovery is texted, not called.
	if err := h.notify.Deliver(context.Background(), ocStored, Event{MonitorID: 3, Monitor: "api", Target: "x", Status: StatusUp, At: now.UnixMilli()}); err != nil {
		t.Fatal(err)
	}
	if reqs := tc.all(); len(reqs) != 3 || !strings.HasSuffix(reqs[2].Path, "/Messages.json") {
		t.Fatalf("recovery: %+v", reqs)
	}

	// An override puts the admin on call; without a phone, the channel says so.
	var o store.Override
	ovBody := fmt.Sprintf(`{"user_id":%d,"start_at":%d,"end_at":%d}`, admin.ID, now.Add(-time.Minute).UnixMilli(), now.Add(time.Hour).UnixMilli())
	if code := c.json("POST", path+"/overrides", ovBody, &o); code != 201 {
		t.Fatalf("override: %d", code)
	}
	if code := c.json("GET", path, "", &sv); code != 200 || sv.Current.UserID != admin.ID || !sv.Current.Override || len(sv.Channels) != 1 {
		t.Fatalf("after override: %+v", sv)
	}
	must(t, st.SetUserPhone(admin.ID, ""))
	err = h.notify.Deliver(context.Background(), ocStored, ev)
	var se setupError
	if err == nil || !strings.Contains(err.Error(), "a@b.c has no phone number") || !errorsAs(err, &se) {
		t.Fatalf("no phone: %v", err)
	}
	var shifts []onCallShift
	if code := c.json("GET", path+"/shifts?from="+strconv.FormatInt(now.UnixMilli(), 10), "", &shifts); code != 200 || len(shifts) < 2 || shifts[0].UserID != admin.ID || shifts[1].UserID != bob.ID {
		t.Fatalf("shifts: %d %+v", code, shifts)
	}
	if code := c.json("DELETE", path+"/overrides/"+strconv.FormatInt(o.ID, 10), "", nil); code != 200 {
		t.Fatalf("delete override: %d", code)
	}
}

func errorsAs(err error, se *setupError) bool {
	for err != nil {
		if s, ok := err.(setupError); ok {
			*se = s
			return true
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}

// Pressing 1 in an alert call acknowledges the incident for the admin whose
// account has the called number.
func TestVoiceCallbackAck(t *testing.T) {
	h, st := newTestHub(t)
	srv := newAPIClient(t, h, st).srv // admin a@b.c
	admin, err := st.UserByEmail("a@b.c")
	must(t, err)
	must(t, st.SetUserPhone(admin.ID, "+393331234567"))
	h.smu.Lock()
	h.general.PublicURL = "https://hub.example.com"
	h.smu.Unlock()
	tw := &store.Notifier{Name: "twilio", Type: "twilio", Enabled: true, Config: map[string]string{"account_sid": "AC1", "auth_token": "tok", "from": "+15550001111", "to": "+393331234567", "mode": "call", "language": "it"}}
	must(t, validateNotifier(tw))
	must(t, st.CreateNotifier(tw))
	m := &store.Monitor{Name: "api", Type: "http", Target: "http://x", IntervalSec: 60, Retries: 1, Locations: []string{LocalLocation}}
	must(t, st.CreateMonitor(m))
	h.MonitorChanged(m.ID)
	h.HandleResult(LocalLocation, result(m.ID, false))
	open := h.openIncidentOf(m.ID)
	if open == nil {
		t.Fatal("no incident")
	}
	post := func(to, digits, sig string) (int, string) {
		path := fmt.Sprintf("/api/voice/%d/%d", tw.ID, open.ID)
		form := url.Values{"Digits": {digits}, "To": {to}, "CallSid": {"CA1"}}
		if sig == "" {
			sig = twilioSignature("tok", "https://hub.example.com"+path, form)
		}
		req, _ := http.NewRequest("POST", srv.URL+path, strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("X-Twilio-Signature", sig)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		b, _ := io.ReadAll(res.Body)
		return res.StatusCode, string(b)
	}
	if code, _ := post("+393331234567", "1", "forged"); code != 403 {
		t.Fatalf("forged: %d", code)
	}
	if code, body := post("+390000000", "1", ""); code != 200 || !strings.Contains(body, "non appartiene") {
		t.Fatalf("unknown number: %d %s", code, body)
	}
	if code, body := post("+393331234567", "1", ""); code != 200 || !strings.Contains(body, "Preso in carico") {
		t.Fatalf("ack: %d %s", code, body)
	}
	if o := h.openIncidentOf(m.ID); o == nil || o.AckedBy != "a@b.c" {
		t.Fatalf("not acknowledged: %+v", o)
	}
}

func TestTwilioVoiceValidation(t *testing.T) {
	base := func() map[string]string {
		return map[string]string{"account_sid": "AC1", "auth_token": "t", "from": "+15550001111", "to": "+391234567"}
	}
	c := base()
	c["mode"] = "call"
	if _, err := validateServiceConfig("twilio", c); err != nil {
		t.Fatal(err)
	}
	c = base()
	c["mode"], c["from"] = "both", "MG1"
	if _, err := validateServiceConfig("twilio", c); err == nil {
		t.Fatal("calls from a messaging service")
	}
	c = base()
	c["language"] = "fr"
	if _, err := validateServiceConfig("twilio", c); err == nil {
		t.Fatal("language fr")
	}
	if err := validateOnCallConfig(map[string]string{"schedule_id": "1", "app": "off"}); err == nil {
		t.Fatal("no way to reach anyone")
	}
}
