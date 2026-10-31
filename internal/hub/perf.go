package hub

import (
	"hash/fnv"
	"log/slog"
	"strconv"
	"sync"
	"time"

	"github.com/federico-it/pingtxt/internal/store"
)

const (
	// localWorkers is the number of checks the hub's own location runs at
	// once. Checks wait on the network, so this can be well above the CPU
	// count: 256 workers handle 5000 monitors a minute even when a fifth of
	// them hang until a 10s timeout.
	localWorkers = 256
	// localQueueSize bounds the checks waiting for a worker. When it is full
	// the check is skipped for this round rather than queued forever.
	localQueueSize = 8192

	// maxFirstRunSpread caps how long a monitor may wait for its first check
	// after a restart.
	maxFirstRunSpread = 5 * time.Minute

	writerBatch = 1000                   // rows per transaction, at most
	writerFlush = 250 * time.Millisecond // max time a result waits in memory
	writerQueue = 20000                  // results buffered before add blocks
)

// firstRunOffset spreads monitors over their interval (capped) so that
// thousands of monitors with the same interval do not fire in the same
// second. The offset depends only on the id, so it is stable across restarts.
func firstRunOffset(m *store.Monitor) time.Duration {
	spread := min(time.Duration(max(m.IntervalSec, 10))*time.Second, maxFirstRunSpread)
	f := fnv.New64a()
	_, _ = f.Write([]byte(strconv.FormatInt(m.ID, 10)))
	return time.Duration(f.Sum64() % uint64(spread))
}

// ---------- in-memory dashboard stats ----------

// statSize is the width of one dashboard bar (24h / barCount).
const statSize = int64(24*time.Hour/time.Millisecond) / barCount

// statCell aggregates the results of one bar.
type statCell struct {
	start    int64
	total    int // checks outside maintenance
	up       int // checks outside maintenance taken while the monitor was not down
	degraded int // checks outside maintenance taken while the monitor was degraded
	maint    int // checks during maintenance
	latSum   float64
	latN     int // OK checks (with maintenance), for the bar latency
	upLatSum float64
	upLatN   int // OK checks outside maintenance, for the 24h average
}

// monStats keeps the last 24h of a monitor's results aggregated per bar, so
// the dashboard does not have to scan the results table. Guarded by Hub.mu.
type monStats struct {
	cells [barCount + 2]statCell
	last  *store.ResultRow
	// since: results older than this come from the database (warmStats),
	// not from HandleResult, so they are not counted twice.
	since int64
}

func (s *monStats) cell(start int64) *statCell {
	c := &s.cells[(start/statSize)%int64(len(s.cells))]
	if c.start != start {
		if start < c.start {
			return nil // older than the window
		}
		*c = statCell{start: start}
	}
	return c
}

func (s *monStats) add(r *store.ResultRow) {
	if s.last == nil || r.TS >= s.last.TS {
		s.last = r
	}
	if r.TS < s.since {
		return
	}
	c := s.cell(r.TS - r.TS%statSize)
	if c == nil {
		return
	}
	if r.Maint {
		c.maint++
	} else {
		c.total++
		if r.MonState != store.MonDown {
			c.up++
		}
		if r.MonState == store.MonDegraded {
			c.degraded++
		}
		if r.OK {
			c.upLatSum += r.LatencyMs
			c.upLatN++
		}
	}
	if r.OK {
		c.latSum += r.LatencyMs
		c.latN++
	}
}

// bars returns the last barCount bars up to now, plus the uptime and the
// average latency over them (-1 uptime without data). Uptime and the OK
// count of a bar follow the monitor status: a check counts as up when the
// monitor was not down right after it.
func (s *monStats) bars(now int64) ([]bar, float64, float64) {
	from := now - int64(24*time.Hour/time.Millisecond)
	from -= from % statSize
	var out []bar
	for t := from; t <= now; t += statSize {
		b := bar{T: t}
		if c := &s.cells[(t/statSize)%int64(len(s.cells))]; c.start == t {
			b.Total, b.OK, b.Degraded, b.Maint = c.total, c.up, c.degraded, c.maint
			if c.latN > 0 {
				b.AvgMs = c.latSum / float64(c.latN)
			}
		}
		out = append(out, b)
	}
	if len(out) > barCount {
		out = out[len(out)-barCount:]
	}
	total, ok, latN := 0, 0, 0
	lat := 0.0
	for _, b := range out {
		c := &s.cells[(b.T/statSize)%int64(len(s.cells))]
		if c.start != b.T {
			continue
		}
		total += c.total
		ok += c.up
		lat += c.upLatSum
		latN += c.upLatN
	}
	up, avg := -1.0, 0.0
	if total > 0 {
		up = float64(ok) / float64(total)
	}
	if latN > 0 {
		avg = lat / float64(latN)
	}
	return out, up, avg
}

// warmStats adds the last 24h of results stored before h.warmUntil to the
// dashboard stats. It scans a day of results, which takes seconds with
// thousands of monitors, so Run calls it in the background: until it is done
// the dashboard only shows results received since the start.
func (h *Hub) warmStats() {
	t0 := time.Now()
	h.mu.Lock()
	until := h.warmUntil
	h.mu.Unlock()
	if until == 0 {
		return
	}
	from := until - int64(24*time.Hour/time.Millisecond) - statSize
	from -= from % statSize
	cells, err := h.store.ResultStats(from, until, statSize)
	if err != nil {
		h.log.Error("load dashboard stats", "err", err)
		return
	}
	h.mu.Lock()
	ids := make([]int64, 0, len(h.monitors))
	for id := range h.monitors {
		ids = append(ids, id)
	}
	for _, rc := range cells {
		st := h.monitors[rc.MonitorID]
		if st == nil || st.stats == nil {
			continue
		}
		if c := st.stats.cell(rc.Start); c != nil {
			c.total += rc.Total
			c.up += rc.Up
			c.degraded += rc.Degraded
			c.maint += rc.Maint
			c.latSum += rc.LatSum
			c.latN += rc.LatN
			c.upLatSum += rc.UpLatSum
			c.upLatN += rc.UpLatN
		}
	}
	h.mu.Unlock()
	for _, id := range ids {
		last, err := h.store.RecentResults(id, 1)
		if err != nil || len(last) == 0 {
			continue
		}
		h.mu.Lock()
		if st := h.monitors[id]; st != nil && st.stats != nil && st.stats.last == nil {
			st.stats.last = last[0]
		}
		h.mu.Unlock()
	}
	h.log.Info("loaded dashboard stats", "monitors", len(ids), "took", time.Since(t0).Round(time.Millisecond))
}

// ---------- batched result writer ----------

// resultWriter collects results and inserts them in batches, one transaction
// per batch. Probes never wait on the database, and a busy database (a long
// prune, a heavy page) delays inserts instead of the checks.
type resultWriter struct {
	st   *store.Store
	log  *slog.Logger
	ch   chan *store.ResultRow
	quit chan struct{}
	done chan struct{}
	once sync.Once
}

func newResultWriter(st *store.Store, log *slog.Logger) *resultWriter {
	return &resultWriter{st: st, log: log, ch: make(chan *store.ResultRow, writerQueue), quit: make(chan struct{}), done: make(chan struct{})}
}

// add queues a row. When the queue is full it blocks (backpressure); after
// stop it writes the row directly.
func (w *resultWriter) add(r *store.ResultRow) {
	select {
	case <-w.done:
		if err := w.st.InsertResult(r); err != nil {
			w.log.Error("insert result", "err", err)
		}
		return
	default:
	}
	select {
	case w.ch <- r:
	case <-w.done:
		if err := w.st.InsertResult(r); err != nil {
			w.log.Error("insert result", "err", err)
		}
	}
}

func (w *resultWriter) run() {
	defer close(w.done)
	t := time.NewTicker(writerFlush)
	defer t.Stop()
	batch := make([]*store.ResultRow, 0, writerBatch)
	flush := func() {
		if len(batch) == 0 {
			return
		}
		if err := w.st.InsertResults(batch); err != nil {
			w.log.Error("insert results", "rows", len(batch), "err", err)
		}
		batch = batch[:0]
	}
	for {
		select {
		case r := <-w.ch:
			batch = append(batch, r)
			if len(batch) >= writerBatch {
				flush()
			}
		case <-t.C:
			flush()
		case <-w.quit:
			for {
				select {
				case r := <-w.ch:
					batch = append(batch, r)
					if len(batch) >= writerBatch {
						flush()
					}
				default:
					flush()
					return
				}
			}
		}
	}
}

// stop flushes the queued rows and waits for the writer to exit.
func (w *resultWriter) stop() {
	w.once.Do(func() { close(w.quit) })
	<-w.done
}
