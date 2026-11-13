package hub

import (
	"cmp"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/federico-it/pingtxt/internal/check"
	"github.com/federico-it/pingtxt/internal/store"
)

// Short causes for the phone: the Live Activity and the app's push
// notifications have one or two lines, so the cause stored with the
// incident ("loc1, loc2: message; loc3: other") is shortened there. The
// stored cause keeps its format: the web UI and the app parse it.

// parseCause splits a cause written by joinReasons into its reasons, as
// parseCause of the web UI does: a part without "location: " belongs to
// the message before it (a message that contains "; ").
func parseCause(cause string) []Reason {
	var out []Reason
	for _, part := range strings.Split(cause, "; ") {
		locs, msg, ok := strings.Cut(part, ": ")
		if !ok || locs == "" {
			if len(out) > 0 {
				out[len(out)-1].Message += "; " + part
			} else if strings.TrimSpace(part) != "" {
				out = append(out, Reason{Message: part})
			}
			continue
		}
		out = append(out, Reason{Message: msg, Locations: strings.Split(locs, ", ")})
	}
	return out
}

// shortCause gives the cause to show on the phone. On a status page
// monitor every location reads the same page, so the locations are left
// out and the page state is shortened to its worst component and first
// incident. On other monitors the locations are left out when all of them
// (nLocs) report the same problem.
func shortCause(cause, typ string, nLocs int) string {
	if cause == "" {
		return ""
	}
	reasons := parseCause(cause)
	if len(reasons) == 0 {
		return cause
	}
	if typ == check.TypeStatuspage {
		return shortStatuspage(reasons[0].Message)
	}
	if len(reasons) == 1 && nLocs > 0 && len(reasons[0].Locations) >= nLocs {
		return reasons[0].Message
	}
	return cause
}

// spComponentRe matches one "Component: state" of a status page message.
var spComponentRe = regexp.MustCompile(`(.+?): (major outage|partial outage|degraded performance|under maintenance)(?:, |$)`)

func spSeverity(state string) int {
	switch state {
	case "major outage":
		return 0
	case "partial outage":
		return 1
	case "degraded performance":
		return 2
	}
	return 3
}

// shortStatuspage shortens the message of a status page check
// ("A: partial outage, B: major outage · Incident: X (investigating); Y
// (identified)") to "B: major outage +1 · X +1".
func shortStatuspage(msg string) string {
	parts := strings.Split(msg, " · ")
	for i, p := range parts {
		switch {
		case strings.HasPrefix(p, "Incident: "):
			parts[i] = firstOf(strings.TrimPrefix(p, "Incident: "), true)
		case strings.HasPrefix(p, "Maintenance: "):
			parts[i] = "Maintenance: " + firstOf(strings.TrimPrefix(p, "Maintenance: "), false)
		case i == 0:
			parts[i] = worstComponent(p)
		}
	}
	return strings.Join(parts, " · ")
}

// worstComponent keeps the worst of the components listed in p, with the
// count of the others. p is returned as is when it is not such a list
// (the page description, an error).
func worstComponent(p string) string {
	m := spComponentRe.FindAllStringSubmatch(p, -1)
	if len(m) < 2 {
		return p
	}
	n := 0
	for _, x := range m {
		n += len(x[0])
	}
	if n != len(p) {
		return p
	}
	worst := slices.MinFunc(m, func(a, b []string) int { return cmp.Compare(spSeverity(a[2]), spSeverity(b[2])) })
	return fmt.Sprintf("%s: %s +%d", worst[1], worst[2], len(m)-1)
}

// firstOf keeps the first of a "; " separated list of names, without its
// " (status)" when stripStatus, followed by the count of the others.
func firstOf(list string, stripStatus bool) string {
	names := strings.Split(list, "; ")
	first := names[0]
	if stripStatus && strings.HasSuffix(first, ")") {
		if i := strings.LastIndex(first, " ("); i > 0 {
			first = first[:i]
		}
	}
	if len(names) > 1 {
		first += fmt.Sprintf(" +%d", len(names)-1)
	}
	return first
}

// monitorShortCause gives the short cause of an event or incident of m.
func monitorShortCause(cause string, m *store.Monitor) string {
	if m == nil {
		return cause
	}
	return shortCause(cause, m.Type, len(m.Locations))
}
