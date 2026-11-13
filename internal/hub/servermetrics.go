package hub

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/federico-it/pingtxt/internal/check"
	"github.com/federico-it/pingtxt/internal/store"
)

// serverPoints caps the points per location in a server history: longer
// windows are averaged into buckets.
const serverPoints = 300

// ServerLocation is the latest snapshot of one location of a server
// monitor.
type ServerLocation struct {
	Location string               `json:"location"`
	Name     string               `json:"name"`
	At       int64                `json:"at"` // unix ms, 0 = no data yet
	Metrics  *check.ServerMetrics `json:"metrics"`
}

// ServerHistory answers GET /api/monitors/{id}/server.
type ServerHistory struct {
	From      int64                `json:"from"` // unix ms
	To        int64                `json:"to"`
	Locations []ServerLocation     `json:"locations"`
	Points    []store.ServerSample `json:"points"`
}

func (h *Hub) monitorServer(w http.ResponseWriter, r *http.Request, _ *store.User) {
	id, ok := pathID(r)
	if !ok {
		writeErr(w, 400, "invalid id")
		return
	}
	m, err := h.store.Monitor(id)
	if errors.Is(err, store.ErrNotFound) {
		writeErr(w, 404, "monitor not found")
		return
	} else if err != nil {
		h.internalErr(w, err)
		return
	}
	hours := 24
	if s := r.URL.Query().Get("hours"); s != "" {
		if hours, err = strconv.Atoi(s); err != nil || hours < 1 || hours > 168 {
			writeErr(w, 400, "hours must be 1 to 168")
			return
		}
	}
	now := time.Now().UnixMilli()
	from := now - int64(hours)*3600_000
	samples, err := h.store.ServerSamples(id, from)
	if err != nil {
		h.internalErr(w, err)
		return
	}
	latest, at, err := h.store.LatestServerMetrics(id)
	if err != nil {
		h.internalErr(w, err)
		return
	}
	names := h.locationNames()
	out := ServerHistory{From: from, To: now, Locations: []ServerLocation{}, Points: downsample(samples, from, now)}
	if m.Type == check.TypeServer {
		for _, l := range h.locationsOf(m) {
			out.Locations = append(out.Locations, ServerLocation{Location: l, Name: nameOr(names, l), At: at[l], Metrics: latest[l]})
		}
	}
	writeJSON(w, 200, out)
}

// downsample averages the samples of each location into at most
// serverPoints buckets over [from, to].
func downsample(in []store.ServerSample, from, to int64) []store.ServerSample {
	size := max((to-from)/serverPoints, 1)
	type key struct {
		loc string
		b   int64
	}
	type acc struct {
		s store.ServerSample
		n float64
	}
	sums := map[key]*acc{}
	var order []key
	for _, s := range in {
		k := key{s.Location, (s.TS - from) / size}
		a := sums[k]
		if a == nil {
			a = &acc{s: store.ServerSample{Location: s.Location}}
			sums[k] = a
			order = append(order, k)
		}
		a.n++
		a.s.TS += s.TS
		a.s.CPUPct += s.CPUPct
		a.s.MemPct += s.MemPct
		a.s.DiskPct += s.DiskPct
		a.s.SwapPct += s.SwapPct
		a.s.Load5 += s.Load5
	}
	out := make([]store.ServerSample, 0, len(order))
	for _, k := range order {
		a := sums[k]
		r := func(v float64) float64 { return float64(int64(v/a.n*10+0.5)) / 10 }
		out = append(out, store.ServerSample{Location: k.loc, TS: int64(float64(a.s.TS) / a.n),
			CPUPct: r(a.s.CPUPct), MemPct: r(a.s.MemPct), DiskPct: r(a.s.DiskPct), SwapPct: r(a.s.SwapPct),
			Load5: float64(int64(a.s.Load5/a.n*100+0.5)) / 100})
	}
	return out
}
