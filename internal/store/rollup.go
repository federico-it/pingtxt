package store

import (
	"database/sql"
	"strconv"
	"strings"
	"time"
)

// Raw results are summarized per monitor, location and hour in
// result_rollups. Raw rows are kept for a few days (detail views, the 24h
// dashboard); older history is read from the hourly rollups, which are about
// 60 times smaller with a 1-minute interval.
//
// rollupUntil is the end of the last summarized hour: readers take
// [.., rollupUntil) from the rollups and [rollupUntil, ..) from the raw
// results, so nothing is counted twice. Raw results are never pruned past
// rollupUntil.

// RollupHour is the width of one rollup row.
const RollupHour = int64(time.Hour / time.Millisecond)

// RollupLag is how long after an hour ends it is summarized, so that late
// results (batched writes, agents on a slow link) are included.
const RollupLag = 10 * time.Minute

func (s *Store) loadRollupUntil() error {
	var v sql.NullInt64
	if err := s.db.QueryRow(`SELECT MAX(start) FROM result_rollups`).Scan(&v); err != nil {
		return err
	}
	if v.Valid {
		s.setRollupUntil(v.Int64 + RollupHour)
	}
	return nil
}

func (s *Store) setRollupUntil(t int64) {
	s.rmu.Lock()
	s.rollupUntil = max(s.rollupUntil, t)
	s.rmu.Unlock()
}

// RollupUntil returns the end of the summarized history (0: none yet).
func (s *Store) RollupUntil() int64 {
	s.rmu.RLock()
	defer s.rmu.RUnlock()
	return s.rollupUntil
}

// RollupResults summarizes every complete hour up to now-RollupLag that is
// not summarized yet, one transaction per hour so the database connection is
// never held for long. It returns the number of hours processed.
func (s *Store) RollupResults(now time.Time, stop <-chan struct{}) (int, error) {
	end := now.Add(-RollupLag).UnixMilli()
	end -= end % RollupHour
	from := s.RollupUntil()
	if from == 0 {
		var first sql.NullInt64
		if err := s.db.QueryRow(`SELECT MIN(ts) FROM results`).Scan(&first); err != nil || !first.Valid {
			return 0, err
		}
		from = first.Int64 - first.Int64%RollupHour
	}
	n := 0
	for h := from; h < end; h += RollupHour {
		select {
		case <-stop:
			return n, nil
		default:
		}
		if err := s.rollupHour(h); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

func (s *Store) rollupHour(h int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// The hour is a trusted integer: written inline so that PostgreSQL knows its type.
	q := `INSERT INTO result_rollups
		(monitor_id, location, start, total, ok, up, degraded, maint, lat_sum, lat_n, up_lat_sum, up_lat_n, lat_min, lat_max,
		ph_n, ph_lat_sum, dns_sum, conn_sum, tls_sum, ttfb_sum)
		SELECT monitor_id, location, ` + strconv.FormatInt(h, 10) + `, SUM(CASE WHEN maint = 0 THEN 1 ELSE 0 END), SUM(CASE WHEN ok = 1 AND maint = 0 THEN 1 ELSE 0 END),
			SUM(CASE WHEN mon_state < 2 AND maint = 0 THEN 1 ELSE 0 END), SUM(CASE WHEN mon_state = 1 AND maint = 0 THEN 1 ELSE 0 END), SUM(maint),
			COALESCE(SUM(CASE WHEN ok = 1 THEN latency_ms END), 0), SUM(CASE WHEN ok = 1 THEN 1 ELSE 0 END),
			COALESCE(SUM(CASE WHEN ok = 1 AND maint = 0 THEN latency_ms END), 0), SUM(CASE WHEN ok = 1 AND maint = 0 THEN 1 ELSE 0 END),
			COALESCE(MIN(CASE WHEN ok = 1 THEN latency_ms END), 0), COALESCE(MAX(CASE WHEN ok = 1 THEN latency_ms END), 0),
			` + rawPhaseSums + `
		FROM results INDEXED BY results_ts WHERE ts >= ? AND ts < ? GROUP BY monitor_id, location`
	switch s.db.driver {
	case DriverSQLite:
		q = strings.Replace(q, "INSERT INTO", "INSERT OR REPLACE INTO", 1)
	case DriverMySQL:
		q = strings.Replace(q, "INSERT INTO", "REPLACE INTO", 1)
	case DriverPostgres:
		q += ` ON CONFLICT (monitor_id, start, location) DO UPDATE SET total = excluded.total, ok = excluded.ok, up = excluded.up, degraded = excluded.degraded,
			maint = excluded.maint, lat_sum = excluded.lat_sum, lat_n = excluded.lat_n, up_lat_sum = excluded.up_lat_sum,
			up_lat_n = excluded.up_lat_n, lat_min = excluded.lat_min, lat_max = excluded.lat_max,
			ph_n = excluded.ph_n, ph_lat_sum = excluded.ph_lat_sum, dns_sum = excluded.dns_sum, conn_sum = excluded.conn_sum,
			tls_sum = excluded.tls_sum, ttfb_sum = excluded.ttfb_sum`
	}
	if _, err := tx.Exec(q, h, h+RollupHour); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	s.setRollupUntil(h + RollupHour)
	return nil
}

// PruneRollups deletes hourly rollups that start before `before`.
func (s *Store) PruneRollups(before int64) (int64, error) {
	var total int64
	for {
		q := `DELETE FROM result_rollups WHERE (monitor_id, start, location) IN
			(SELECT monitor_id, start, location FROM result_rollups WHERE start < ? LIMIT 5000)`
		if s.db.driver == DriverMySQL {
			q = `DELETE FROM result_rollups WHERE start < ? LIMIT 5000`
		}
		res, err := s.db.Exec(q, before)
		if err != nil {
			return total, err
		}
		n, _ := res.RowsAffected()
		total += n
		if n < 5000 {
			return total, nil
		}
	}
}
