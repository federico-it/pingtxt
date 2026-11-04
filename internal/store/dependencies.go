package store

// monitorDeps returns the monitors each monitor depends on, for one monitor
// (id > 0) or all of them (id 0).
func (s *Store) monitorDeps(id int64) (map[int64][]int64, error) {
	q := `SELECT monitor_id, parent_id FROM monitor_dependencies`
	var args []any
	if id > 0 {
		q += ` WHERE monitor_id = ?`
		args = append(args, id)
	}
	rows, err := s.db.Query(q+` ORDER BY monitor_id, parent_id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64][]int64{}
	for rows.Next() {
		var m, p int64
		if err := rows.Scan(&m, &p); err != nil {
			return nil, err
		}
		out[m] = append(out[m], p)
	}
	return out, rows.Err()
}

// setMonitorDeps replaces the dependencies of a monitor. nil keeps the
// stored ones.
func setMonitorDeps(tx *tx, monitorID int64, parents []int64) error {
	if parents == nil {
		return nil
	}
	if _, err := tx.Exec(`DELETE FROM monitor_dependencies WHERE monitor_id = ?`, monitorID); err != nil {
		return err
	}
	for _, p := range parents {
		if p == monitorID {
			continue
		}
		if _, err := tx.Exec(`INSERT OR IGNORE INTO monitor_dependencies (monitor_id, parent_id) VALUES (?, ?)`, monitorID, p); err != nil {
			return err
		}
	}
	return nil
}

// MonitorDependencies returns the monitors each monitor depends on.
func (s *Store) MonitorDependencies() (map[int64][]int64, error) { return s.monitorDeps(0) }

// SetMonitorDependencies replaces the monitors one monitor depends on.
func (s *Store) SetMonitorDependencies(monitorID int64, parents []int64) error {
	if parents == nil {
		parents = []int64{}
	}
	t, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer t.Rollback()
	if err := setMonitorDeps(t, monitorID, parents); err != nil {
		return err
	}
	return t.Commit()
}
