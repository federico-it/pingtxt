package store

import (
	"database/sql"
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
	if _, err := tx.Exec(`INSERT OR REPLACE INTO result_rollups
		(monitor_id, location, start, total, ok, maint, lat_sum, lat_n, up_lat_sum, up_lat_n, lat_min, lat_max)
		SELECT monitor_id, location, ?, SUM(maint = 0), SUM(ok AND maint = 0), SUM(maint),
			COALESCE(SUM(CASE WHEN ok = 1 THEN latency_ms END), 0), SUM(ok = 1),
			COALESCE(SUM(CASE WHEN ok = 1 AND maint = 0 THEN latency_ms END), 0), SUM(ok = 1 AND maint = 0),
			COALESCE(MIN(CASE WHEN ok = 1 THEN latency_ms END), 0), COALESCE(MAX(CASE WHEN ok = 1 THEN latency_ms END), 0)
		FROM results INDEXED BY results_ts WHERE ts >= ? AND ts < ? GROUP BY monitor_id, location`,
		h, h, h+RollupHour); err != nil {
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
		res, err := s.db.Exec(`DELETE FROM result_rollups WHERE (monitor_id, start, location) IN
			(SELECT monitor_id, start, location FROM result_rollups WHERE start < ? LIMIT 5000)`, before)
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
