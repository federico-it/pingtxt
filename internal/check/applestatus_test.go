package check

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestApplePage(t *testing.T) {
	cases := map[string]string{
		"https://www.apple.com/support/systemstatus/":                                      AppleStatusURL,
		"apple.com/it/support/systemstatus":                                                AppleStatusURL,
		"http://www.apple.com/support/systemstatus/data/system_status_it_IT.js":            AppleStatusURL,
		"https://developer.apple.com/system-status/":                                       AppleDeveloperStatusURL,
		"https://www.apple.com/support/systemstatus/data/developer/system_status_en_US.js": AppleDeveloperStatusURL,
		"https://www.apple.com/iphone/":                                                    "https://www.apple.com/iphone",
		"https://developer.apple.com/system-statuses":                                      "https://developer.apple.com/system-statuses",
	}
	for in, want := range cases {
		if got, err := StatuspageBase(in); err != nil || got != want {
			t.Errorf("StatuspageBase(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
}

func TestAppleID(t *testing.T) {
	for in, want := range map[string]string{
		"iCloud Web Apps (iCloud.com)":  "icloud-web-apps-icloud-com",
		"App Store - In-App Purchases":  "app-store-in-app-purchases",
		"Apple Account Card":            "apple-account-card",
		"  Maps Routing & Navigation  ": "maps-routing-navigation",
	} {
		if got := appleID(in); got != want {
			t.Errorf("appleID(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestParseAppleStatus(t *testing.T) {
	body := []byte(`jsonCallback({"services":[
		{"serviceName":"APNS","events":[]},
		{"serviceName":"Xcode Cloud","events":[
			{"messageId":"1","statusType":"Outage","eventStatus":"ongoing","usersAffected":"All users are affected"},
			{"messageId":"0","statusType":"Outage","eventStatus":"resolved","usersAffected":"All users were affected"}]},
		{"serviceName":"TestFlight","events":[
			{"messageId":"2","statusType":"Issue","eventStatus":"ongoing","usersAffected":"Some users are affected","affectedServices":["TestFlight","Sandbox"]}]},
		{"serviceName":"Sandbox","events":[
			{"messageId":"2","statusType":"Issue","eventStatus":"ongoing","usersAffected":"Some users are affected","affectedServices":["TestFlight","Sandbox"]},
			{"messageId":"3","statusType":"Maintenance","eventStatus":"ongoing"},
			{"messageId":"4","statusType":"Maintenance","eventStatus":"upcoming"}]},
		{"serviceName":"Maps","events":[
			{"messageId":"5","statusType":"Outage","eventStatus":"ongoing","usersAffected":"Some users are affected"}]}
	]});`)
	s, err := parseAppleStatus(body, AppleDeveloperStatusURL, "Apple Developer")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"apns": spOperational, "xcode-cloud": spMajor, "testflight": spDegraded, "sandbox": spDegraded, "maps": spPartial}
	for _, c := range s.Components {
		if want[c.ID] != c.Status {
			t.Errorf("%s (%s): %s, want %s", c.ID, c.Name, c.Status, want[c.ID])
		}
	}
	if len(s.Components) != len(want) || s.Status.Indicator != "major" || s.Page.Name != "Apple Developer" {
		t.Errorf("summary: %+v", s)
	}
	if len(s.Incidents) != 3 || s.Incidents[1].Name != "Issue on TestFlight, Sandbox" || s.Incidents[1].Status != "some users affected" {
		t.Errorf("incidents: %+v", s.Incidents)
	}
	if len(s.Maintenances) != 1 || s.Maintenances[0].Status != "in_progress" {
		t.Errorf("maintenances: %+v", s.Maintenances)
	}

	// Whole page: down on an outage of all users. Components: degraded on
	// an issue, up when operational.
	if r := s.Evaluate(nil); r.OK || !strings.Contains(r.Message, "Outage on Xcode Cloud (all users affected)") {
		t.Errorf("page: %+v", r)
	}
	if r := s.Evaluate([]string{"apns", "testflight"}); !r.OK || !r.Degraded ||
		r.Message != "TestFlight: degraded performance · Incident: Issue on TestFlight, Sandbox (some users affected)" {
		t.Errorf("testflight: %+v", r)
	}
	if r := s.Evaluate([]string{"apns"}); !r.OK || r.Degraded || r.Message != "" {
		t.Errorf("apns: %+v", r)
	}
	if r := s.Evaluate([]string{"maps"}); !r.OK || !r.Degraded {
		t.Errorf("maps, some users: %+v", r)
	}

	ok, _ := parseAppleStatus([]byte(`{"services":[{"serviceName":"iCloud Mail","events":[]}]}`), AppleStatusURL, "Apple")
	if ok == nil || ok.Status.Indicator != "none" {
		t.Errorf("operational: %+v", ok)
	}
	for _, bad := range []string{`{"hello":"world"}`, `<html>`, ``} {
		if _, err := parseAppleStatus([]byte(bad), AppleStatusURL, "Apple"); err == nil {
			t.Errorf("%q: want error", bad)
		}
	}
}

func TestRunAppleStatus(t *testing.T) {
	plain, err := os.ReadFile("testdata/apple_status.json")
	if err != nil {
		t.Fatal(err)
	}
	jsonp, err := os.ReadFile("testdata/apple_developer_status.js")
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/system_status_en_US.js":
			_, _ = w.Write(plain)
		case "/developer/system_status_en_US.js":
			_, _ = w.Write(jsonp)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	old := appleFeedBase
	appleFeedBase = srv.URL
	defer func() { appleFeedBase = old }()

	// The real feeds of 2026-11-17, with outages of iCloud Mail (some
	// users), AppleCare on Device and Xcode Cloud (all users).
	r := Run(context.Background(), Job{Type: TypeStatuspage, Target: "https://www.apple.com/support/systemstatus/"})
	if r.OK || !strings.Contains(r.Message, "Outage on AppleCare on Device (all users affected)") {
		t.Errorf("apple page: %+v", r)
	}
	r = Run(context.Background(), Job{Type: TypeStatuspage, Target: AppleStatusURL, Keyword: "icloud-mail,app-store"})
	if !r.OK || !r.Degraded || !strings.HasPrefix(r.Message, "iCloud Mail: partial outage") {
		t.Errorf("iCloud Mail: %+v", r)
	}
	r = Run(context.Background(), Job{Type: TypeStatuspage, Target: "https://developer.apple.com/system-status/", Keyword: "xcode-cloud"})
	if r.OK || !strings.HasPrefix(r.Message, "Xcode Cloud: major outage") {
		t.Errorf("Xcode Cloud: %+v", r)
	}
	r = Run(context.Background(), Job{Type: TypeStatuspage, Target: AppleDeveloperStatusURL, Keyword: "apns"})
	if !r.OK || r.Degraded || r.Message != "" {
		t.Errorf("APNS: %+v", r)
	}
}
