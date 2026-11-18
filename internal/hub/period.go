package hub

import (
	"sync"
	"time"
)

// ---------- dashboard period (?range= on GET /api/monitors) ----------

// monitorPeriod is the uptime of a monitor over the period the dashboard
// shows (?range=1h|24h|7d|30d): the same bars as the monitor page, the
// uptime (0..1, -1 = no checks) and the average latency over them.
type monitorPeriod struct {
	Range  string  `json:"range"`
	From   int64   `json:"from"`
	To     int64   `json:"to"`
	Uptime float64 `json:"uptime"`
	AvgMs  float64 `json:"avg_ms"`
	Bars   []bar   `json:"bars"`
}

// periodTTL is how long the bars of every monitor over a period are reused:
// a period of days reads a month of rollups, so it is not read again on
// every dashboard refresh.
var periodTTL = map[string]time.Duration{
	"1h":  15 * time.Second,
	"6h":  time.Minute,
	"7d":  2 * time.Minute,
	"30d": 5 * time.Minute,
}

type periodEntry struct {
	mu   sync.Mutex
	at   time.Time
	from int64
	to   int64
	size int64
	n    int
	// cells of each monitor per slot start
	cells map[int64]map[int64]*periodCell
}

type periodCell struct {
	total, up, degraded, maint, latN, upLatN int
	latSum, upLatSum                         float64
}

type periodCache struct {
	mu      sync.Mutex
	byRange map[string]*periodEntry
}

func (c *periodCache) entry(rng string) *periodEntry {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.byRange == nil {
		c.byRange = map[string]*periodEntry{}
	}
	e := c.byRange[rng]
	if e == nil {
		e = &periodEntry{}
		c.byRange[rng] = e
	}
	return e
}

// periodBars returns a function that gives the period of a monitor over the
// range rng (a key of ranges other than 24h, which the in-memory stats
// serve), reading the results of all monitors at once.
func (h *Hub) periodBars(rng string) (func(id int64) *monitorPeriod, error) {
	e := h.periods.entry(rng)
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.cells == nil || time.Since(e.at) > periodTTL[rng] {
		d := ranges[rng]
		size, ok := barSizes[d]
		if !ok {
			size = max(d.Milliseconds()/barCount, int64(time.Minute/time.Millisecond))
		}
		now := time.Now().UnixMilli()
		n := int(d.Milliseconds() / size)
		// n slots, the last one holding now (partial), as on the monitor page.
		last := now - now%size
		from := last - int64(n-1)*size
		cells, err := h.store.PeriodStats(from, now+1, size)
		if err != nil {
			return nil, err
		}
		byMon := map[int64]map[int64]*periodCell{}
		for _, c := range cells {
			m := byMon[c.MonitorID]
			if m == nil {
				m = map[int64]*periodCell{}
				byMon[c.MonitorID] = m
			}
			p := m[c.Start]
			if p == nil {
				p = &periodCell{}
				m[c.Start] = p
			}
			p.total += c.Total
			p.up += c.Up
			p.degraded += c.Degraded
			p.maint += c.Maint
			p.latSum += c.LatSum
			p.latN += c.LatN
			p.upLatSum += c.UpLatSum
			p.upLatN += c.UpLatN
		}
		e.at, e.from, e.to, e.size, e.n, e.cells = time.Now(), from, now, size, n, byMon
	}
	from, to, size, n, all := e.from, e.to, e.size, e.n, e.cells
	return func(id int64) *monitorPeriod {
		p := &monitorPeriod{Range: rng, From: from, To: to, Uptime: -1, Bars: make([]bar, n)}
		cells := all[id]
		total, up, latN := 0, 0, 0
		lat := 0.0
		for i := range n {
			t := from + int64(i)*size
			b := bar{T: t}
			if c := cells[t]; c != nil {
				b.Total, b.OK, b.Degraded, b.Maint = c.total, c.up, c.degraded, c.maint
				if c.latN > 0 {
					b.AvgMs = c.latSum / float64(c.latN)
				}
				total += c.total
				up += c.up
				lat += c.upLatSum
				latN += c.upLatN
			}
			p.Bars[i] = b
		}
		if total > 0 {
			p.Uptime = float64(up) / float64(total)
		}
		if latN > 0 {
			p.AvgMs = lat / float64(latN)
		}
		return p
	}, nil
}
