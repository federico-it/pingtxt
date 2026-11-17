package check

import (
	"bytes"
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"
)

// Apple's system status pages (www.apple.com/support/systemstatus for its
// consumer services, developer.apple.com/system-status for developer ones)
// are not Atlassian Statuspage pages: each is filled from a JSON feed with
// one entry per service and the events that touch it. The statuspage type
// reads them too, turning the feed into the summary of a Statuspage page,
// so components, the degraded/down mapping and the messages work the same.
//
// Every service is a component, its id the service name in lower case with
// dashes ("icloud-mail"). The state of a service comes from its ongoing
// events:
//
//	Outage affecting all users        major_outage (down)
//	Outage affecting some users       partial_outage (degraded)
//	Issue, Performance and the rest   degraded_performance (degraded)
//	Maintenance                       under_maintenance (up)
//
// Resolved, completed and upcoming events are left out.

// The addresses of Apple's pages, as StatuspageBase gives them.
const (
	AppleStatusURL          = "https://www.apple.com/support/systemstatus"
	AppleDeveloperStatusURL = "https://developer.apple.com/system-status"
)

// appleFeedBase serves the feeds; tests point it at their own server.
var appleFeedBase = "https://www.apple.com/support/systemstatus/data"

// applePage recognizes the URL of one of Apple's status pages or of its
// feed, in any language, and gives the page's address ("" when it is not
// one).
func applePage(u *url.URL) string {
	host := strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
	p := strings.ToLower(u.Path)
	switch host {
	case "apple.com":
		if strings.Contains(p, "/systemstatus/data/developer/") {
			return AppleDeveloperStatusURL
		}
		if strings.Contains(p, "/support/systemstatus") {
			return AppleStatusURL
		}
	case "developer.apple.com":
		if p == "/system-status" || strings.HasPrefix(p, "/system-status/") {
			return AppleDeveloperStatusURL
		}
	}
	return ""
}

// appleFeed gives the feed of an Apple page address, "" for other pages.
// The English feed is read whatever the page's language, so component ids
// stay the same.
func appleFeed(base string) (feed, name string) {
	switch base {
	case AppleStatusURL:
		return appleFeedBase + "/system_status_en_US.js", "Apple"
	case AppleDeveloperStatusURL:
		return appleFeedBase + "/developer/system_status_en_US.js", "Apple Developer"
	}
	return "", ""
}

type appleStatusFeed struct {
	Services *[]appleService `json:"services"`
}

type appleService struct {
	Name   string       `json:"serviceName"`
	Events []appleEvent `json:"events"`
}

type appleEvent struct {
	ID               string   `json:"messageId"`
	Type             string   `json:"statusType"`
	Status           string   `json:"eventStatus"`
	UsersAffected    string   `json:"usersAffected"`
	AffectedServices []string `json:"affectedServices"`
}

// appleState gives the component state of an ongoing event.
func (e appleEvent) state() string {
	switch strings.ToLower(e.Type) {
	case "maintenance":
		return spMaintenance
	case "outage":
		if strings.Contains(strings.ToLower(e.UsersAffected), "all users") {
			return spMajor
		}
		return spPartial
	}
	return spDegraded
}

// kind names the event in a message: "Outage", "Performance issue".
func (e appleEvent) kind() string {
	switch t := strings.TrimSpace(e.Type); strings.ToLower(t) {
	case "":
		return "Issue"
	case "performance":
		return "Performance issue"
	default:
		return t
	}
}

// spRank orders component states from best to worst.
var spRank = map[string]int{spOperational: 0, spMaintenance: 1, spDegraded: 2, spPartial: 3, spMajor: 4}

// appleID makes a component id of a service name: "iCloud Web Apps
// (iCloud.com)" is "icloud-web-apps-icloud-com".
func appleID(name string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(name) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			if dash && b.Len() > 0 {
				b.WriteByte('-')
			}
			b.WriteRune(r)
			dash = false
			continue
		}
		dash = true
	}
	return b.String()
}

// parseAppleStatus reads an Apple feed (plain JSON, or JSONP as the
// developer feed is) into the summary of a Statuspage page.
func parseAppleStatus(body []byte, base, name string) (*StatuspageSummary, error) {
	body = bytes.TrimSpace(body)
	if i := bytes.IndexByte(body, '('); i >= 0 && !bytes.HasPrefix(body, []byte("{")) {
		body = bytes.TrimSpace(bytes.TrimRight(body[i+1:], ";"))
		body = bytes.TrimSuffix(body, []byte(")"))
	}
	var f appleStatusFeed
	if err := json.Unmarshal(body, &f); err != nil || f.Services == nil {
		return nil, errors.New("not an Apple system status feed")
	}
	s := &StatuspageSummary{}
	s.Page.Name, s.Page.URL = name, base
	// Component ids first: an event names the other services it touches.
	ids := map[string]string{}
	used := map[string]bool{}
	for _, sv := range *f.Services {
		if _, ok := ids[sv.Name]; ok {
			continue
		}
		id := appleID(sv.Name)
		for n := 2; used[id]; n++ {
			id = fmt.Sprintf("%s-%d", appleID(sv.Name), n)
		}
		ids[sv.Name], used[id] = id, true
	}
	// An event shows up under every service it touches: keep it once.
	type event struct {
		appleEvent
		services []string
	}
	var events []*event
	byID := map[string]*event{}
	worst := spOperational
	for _, sv := range *f.Services {
		st := spOperational
		for _, e := range sv.Events {
			if !strings.EqualFold(e.Status, "ongoing") {
				continue
			}
			if es := e.state(); spRank[es] > spRank[st] {
				st = es
			}
			if ev := byID[e.ID]; ev != nil && e.ID != "" {
				ev.services = appendNew(ev.services, sv.Name)
				continue
			}
			ev := &event{appleEvent: e, services: appendNew(slices.Clone(e.AffectedServices), sv.Name)}
			events = append(events, ev)
			byID[e.ID] = ev
		}
		if spRank[st] > spRank[worst] {
			worst = st
		}
		s.Components = append(s.Components, StatuspageComponent{ID: ids[sv.Name], Name: sv.Name, Status: st})
	}
	switch worst {
	case spOperational:
		s.Status.Indicator, s.Status.Description = "none", "All Systems Operational"
	case spMaintenance:
		s.Status.Indicator, s.Status.Description = "maintenance", "Service Under Maintenance"
	case spMajor:
		s.Status.Indicator, s.Status.Description = "major", "Major Service Outage"
	default:
		s.Status.Indicator, s.Status.Description = "minor", "Minor Service Outage"
	}
	for _, ev := range events {
		in := statuspageIncident{Name: ev.kind() + " on " + strings.Join(ev.services, ", ")}
		for _, sv := range ev.services {
			in.Components = append(in.Components, struct {
				ID string `json:"id"`
			}{cmp.Or(ids[sv], appleID(sv))})
		}
		if ev.state() == spMaintenance {
			in.Status = "in_progress"
			s.Maintenances = append(s.Maintenances, in)
			continue
		}
		// "Some users are affected" → "some users affected"
		who := strings.ToLower(strings.TrimSpace(ev.UsersAffected))
		in.Status = cmp.Or(strings.TrimSuffix(strings.Replace(who, " are affected", " affected", 1), "."), "ongoing")
		s.Incidents = append(s.Incidents, in)
	}
	return s, nil
}

func appendNew(list []string, v string) []string {
	if slices.Contains(list, v) {
		return list
	}
	return append(list, v)
}
