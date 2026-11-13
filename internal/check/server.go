package check

import (
	"context"
	"errors"
	"fmt"
	"math"
	"path"
	"strings"
	"sync"
	"time"
)

// TypeServer reports the metrics of the machine running the check: each
// location of the monitor is a server (the hub, or an agent's host).
const TypeServer = "server"

// ServerLimits are the thresholds of a server monitor; 0 turns one off. A
// check above any of them fails.
type ServerLimits struct {
	CPUPct  int `json:"cpu_pct"`  // CPU use since the previous check, percent
	MemPct  int `json:"mem_pct"`  // memory in use (not available), percent
	DiskPct int `json:"disk_pct"` // the fullest checked disk, percent
	SwapPct int `json:"swap_pct"` // swap in use, percent
	// Load is the 5 minute load average per CPU core, e.g. 1.5.
	Load float64 `json:"load"`
}

// ServerMetrics is a snapshot of a server.
type ServerMetrics struct {
	// CPUPct is the CPU use (all cores) since the previous check of the
	// same monitor, or over one second for the first one.
	CPUPct    float64     `json:"cpu_pct"`
	Cores     int         `json:"cores"`
	Load1     float64     `json:"load1"`
	Load5     float64     `json:"load5"`
	Load15    float64     `json:"load15"`
	MemTotal  uint64      `json:"mem_total"` // bytes
	MemUsed   uint64      `json:"mem_used"`  // bytes, total minus available
	SwapTotal uint64      `json:"swap_total"`
	SwapUsed  uint64      `json:"swap_used"`
	Disks     []DiskUsage `json:"disks"`
	UptimeSec int64       `json:"uptime_sec"`
}

// DiskUsage is one mounted filesystem.
type DiskUsage struct {
	Mount string `json:"mount"`
	FS    string `json:"fs"`
	Total uint64 `json:"total"` // bytes
	Used  uint64 `json:"used"`  // bytes
	// Free is what unprivileged users can still write: Used/(Used+Free)
	// is the share df shows.
	Free uint64 `json:"free"`
}

// Pct is the share of the disk in use, as df shows it.
func (d DiskUsage) Pct() float64 { return pctOf(d.Used, d.Used+d.Free) }

// MemPct is the share of memory in use.
func (m *ServerMetrics) MemPct() float64 { return pctOf(m.MemUsed, m.MemTotal) }

// SwapPct is the share of swap in use (0 without swap).
func (m *ServerMetrics) SwapPct() float64 { return pctOf(m.SwapUsed, m.SwapTotal) }

// FullestDisk is the disk with the highest share in use, if any.
func (m *ServerMetrics) FullestDisk() (DiskUsage, bool) {
	var best DiskUsage
	found := false
	for _, d := range m.Disks {
		if !found || d.Pct() > best.Pct() {
			best, found = d, true
		}
	}
	return best, found
}

func pctOf(a, b uint64) float64 {
	if b == 0 {
		return 0
	}
	return math.Round(float64(a)/float64(b)*1000) / 10
}

// ServerMounts parses the target of a server monitor: the mount points to
// check, comma-separated, or empty for every disk.
func ServerMounts(target string) ([]string, error) {
	var out []string
	for _, f := range strings.FieldsFunc(target, func(r rune) bool { return r == ',' || r == ' ' || r == '\n' }) {
		if !strings.HasPrefix(f, "/") {
			return nil, fmt.Errorf("%q is not a mount point: use absolute paths like /data", f)
		}
		p := path.Clean(f)
		dup := false
		for _, o := range out {
			dup = dup || o == p
		}
		if !dup {
			out = append(out, p)
		}
	}
	if len(out) > 20 {
		return nil, errors.New("at most 20 mount points")
	}
	return out, nil
}

// ValidateServerLimits checks the ranges of the thresholds.
func ValidateServerLimits(l ServerLimits) error {
	for _, v := range []int{l.CPUPct, l.MemPct, l.DiskPct, l.SwapPct} {
		if v < 0 || v > 100 {
			return errors.New("server limits are percentages from 0 (off) to 100")
		}
	}
	if l.Load < 0 || l.Load > 1000 || math.IsNaN(l.Load) {
		return errors.New("the load limit is per core, from 0 (off) to 1000")
	}
	return nil
}

// cpuTimes is a reading of the CPU counters.
type cpuTimes struct {
	busy, total uint64
	at          time.Time
}

// prevCPU keeps the last reading per monitor, so the CPU use of a check
// covers the whole interval since the previous one.
var prevCPU = struct {
	sync.Mutex
	m map[int64]cpuTimes
}{m: map[int64]cpuTimes{}}

func cpuSince(ctx context.Context, monitorID int64) (float64, error) {
	now, err := readCPU()
	if err != nil {
		return 0, err
	}
	prevCPU.Lock()
	prev, ok := prevCPU.m[monitorID]
	prevCPU.m[monitorID] = now
	prevCPU.Unlock()
	if !ok || now.at.Sub(prev.at) < time.Second || now.at.Sub(prev.at) > 6*time.Hour || now.total <= prev.total {
		// No usable reading: measure over one second.
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		case <-time.After(time.Second):
		}
		prev = now
		if now, err = readCPU(); err != nil {
			return 0, err
		}
		prevCPU.Lock()
		prevCPU.m[monitorID] = now
		prevCPU.Unlock()
	}
	if now.total <= prev.total {
		return 0, nil
	}
	busy := float64(now.busy-prev.busy) / float64(now.total-prev.total) * 100
	return math.Round(min(max(busy, 0), 100)*10) / 10, nil
}

// runServer reads the metrics of the machine it runs on and compares them
// with the limits.
func runServer(ctx context.Context, j Job) Result {
	start := time.Now()
	mounts, err := ServerMounts(j.Target)
	if err != nil {
		return Result{Message: err.Error()}
	}
	m, err := collectServer(ctx, j.MonitorID, mounts)
	lat := ms(time.Since(start))
	if err != nil {
		if ctx.Err() != nil {
			return Result{LatencyMs: lat, Message: "timeout"}
		}
		return Result{LatencyMs: lat, Message: err.Error()}
	}
	var lim ServerLimits
	if j.ServerLimits != nil {
		lim = *j.ServerLimits
	}
	return serverResult(m, lim, mounts, lat)
}

func serverResult(m *ServerMetrics, lim ServerLimits, mounts []string, lat float64) Result {
	r := Result{LatencyMs: lat, Server: m, OK: true}
	var over []string
	check := func(name string, v float64, limit int) {
		if limit > 0 && v > float64(limit) {
			over = append(over, fmt.Sprintf("%s %.0f%% over %d%%", name, v, limit))
		}
	}
	check("cpu", m.CPUPct, lim.CPUPct)
	check("ram", m.MemPct(), lim.MemPct)
	if m.SwapTotal > 0 {
		check("swap", m.SwapPct(), lim.SwapPct)
	}
	for _, d := range m.Disks {
		check("disk "+d.Mount, d.Pct(), lim.DiskPct)
	}
	cores := max(m.Cores, 1)
	if lim.Load > 0 && m.Load5/float64(cores) > lim.Load {
		over = append(over, fmt.Sprintf("load %.2f over %.2f per core (%d cores)", m.Load5, lim.Load, cores))
	}
	// Mount points asked for but not found are a failure: the disk to
	// watch is missing.
	for _, want := range mounts {
		found := false
		for _, d := range m.Disks {
			found = found || d.Mount == want
		}
		if !found {
			over = append(over, "no filesystem at "+want)
		}
	}
	if len(over) > 0 {
		r.OK = false
		r.Message = strings.Join(over, ", ")
		return r
	}
	parts := []string{fmt.Sprintf("cpu %.0f%%", m.CPUPct), fmt.Sprintf("ram %.0f%%", m.MemPct())}
	if d, ok := m.FullestDisk(); ok {
		parts = append(parts, fmt.Sprintf("disk %s %.0f%%", d.Mount, d.Pct()))
	}
	parts = append(parts, fmt.Sprintf("load %.2f", m.Load5))
	r.Message = strings.Join(parts, ", ")
	return r
}
