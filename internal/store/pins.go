package store

import (
	"database/sql"
	"encoding/json"
	"errors"
)

// UserPins returns the ids of the monitors a user pinned, in pin order. Ids
// of deleted monitors may still be in the list: callers drop them.
func (s *Store) UserPins(userID int64) ([]int64, error) {
	var raw string
	err := s.db.QueryRow(`SELECT pinned_monitors FROM users WHERE id = ?`, userID).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	ids := []int64{}
	if raw != "" {
		_ = json.Unmarshal([]byte(raw), &ids)
	}
	return ids, nil
}

// SetUserPins saves the monitors a user pinned.
func (s *Store) SetUserPins(userID int64, ids []int64) error {
	if ids == nil {
		ids = []int64{}
	}
	b, err := json.Marshal(ids)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`UPDATE users SET pinned_monitors = ? WHERE id = ?`, string(b), userID)
	return err
}

// MonitorIDs returns the ids of every monitor.
func (s *Store) MonitorIDs() (map[int64]bool, error) {
	rows, err := s.db.Query(`SELECT id FROM monitors`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]bool{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = true
	}
	return out, rows.Err()
}
