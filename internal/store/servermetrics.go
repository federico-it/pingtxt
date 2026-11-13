package store

import (
	"encoding/json"

	"github.com/federico-it/pingtxt/internal/check"
)

// ServerSample is the metrics one location of a server monitor reported at
// one check.
type ServerSample struct {
	MonitorID int64   `json:"-"`
	Location  string  `json:"location"`
	TS        int64   `json:"ts"` // unix ms
	CPUPct    float64 `json:"cpu_pct"`
	MemPct    float64 `json:"mem_pct"`
	DiskPct   float64 `json:"disk_pct"` // the fullest disk
	SwapPct   float64 `json:"swap_pct"`
	Load5     float64 `json:"load5"`
}

// InsertServerSample stores the metrics of a server check.
func (s *Store) InsertServerSample(monitorID int64, loc string, ts int64, m *check.ServerMetrics) error {
	data, err := json.Marshal(m)
	if err != nil {
		return err
	}
	var disk float64
	if d, ok := m.FullestDisk(); ok {
		disk = d.Pct()
	}
	_, err = s.db.Exec(`INSERT INTO server_metrics (monitor_id, location, ts, cpu_pct, mem_pct, disk_pct, swap_pct, load5, data) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		monitorID, loc, ts, m.CPUPct, m.MemPct(), disk, m.SwapPct(), m.Load5, string(data))
	return err
}

// ServerSamples lists the samples of a monitor taken at or after since,
// oldest first.
func (s *Store) ServerSamples(monitorID, since int64) ([]ServerSample, error) {
	rows, err := s.db.Query(`SELECT location, ts, cpu_pct, mem_pct, disk_pct, swap_pct, load5 FROM server_metrics WHERE monitor_id = ? AND ts >= ? ORDER BY ts`, monitorID, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ServerSample{}
	for rows.Next() {
		x := ServerSample{MonitorID: monitorID}
		if err := rows.Scan(&x.Location, &x.TS, &x.CPUPct, &x.MemPct, &x.DiskPct, &x.SwapPct, &x.Load5); err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

// LatestServerMetrics returns the last full snapshot of each location of a
// monitor, with its time (unix ms).
func (s *Store) LatestServerMetrics(monitorID int64) (map[string]*check.ServerMetrics, map[string]int64, error) {
	rows, err := s.db.Query(`SELECT m.location, m.ts, m.data FROM server_metrics m
		JOIN (SELECT location, MAX(ts) AS ts FROM server_metrics WHERE monitor_id = ? GROUP BY location) l
		ON l.location = m.location AND l.ts = m.ts
		WHERE m.monitor_id = ?`, monitorID, monitorID)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	out, at := map[string]*check.ServerMetrics{}, map[string]int64{}
	for rows.Next() {
		var loc, data string
		var ts int64
		if err := rows.Scan(&loc, &ts, &data); err != nil {
			return nil, nil, err
		}
		m := &check.ServerMetrics{}
		if json.Unmarshal([]byte(data), m) == nil {
			out[loc], at[loc] = m, ts
		}
	}
	return out, at, rows.Err()
}

// PruneServerSamples deletes samples taken before the given time.
func (s *Store) PruneServerSamples(before int64) (int64, error) {
	res, err := s.db.Exec(`DELETE FROM server_metrics WHERE ts < ?`, before)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
