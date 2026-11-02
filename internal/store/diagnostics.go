package store

import (
	"encoding/json"
	"strings"
)

// Diagnostic is what one location found out about a failing check (see
// check.Diag). Data is kept as JSON, as the location sent it.
type Diagnostic struct {
	ID         int64           `json:"id"`
	MonitorID  int64           `json:"monitor_id"`
	IncidentID int64           `json:"incident_id"` // 0: no incident (yet)
	Location   string          `json:"location"`
	At         int64           `json:"at"`
	Data       json.RawMessage `json:"data"`
}

// InsertDiagnostic stores diagnostics. If the monitor has an open incident
// they are linked to it right away.
func (s *Store) InsertDiagnostic(d *Diagnostic) (int64, error) {
	var open int64
	_ = s.db.QueryRow(`SELECT id FROM incidents WHERE monitor_id = ? AND resolved_at = 0 ORDER BY id DESC LIMIT 1`, d.MonitorID).Scan(&open)
	d.IncidentID = open
	id, err := s.db.insert(s.db, `INSERT INTO diagnostics (monitor_id, incident_id, location, at, data) VALUES (?, ?, ?, ?, ?)`,
		d.MonitorID, d.IncidentID, d.Location, d.At, string(d.Data))
	d.ID = id
	return id, err
}

// LinkDiagnostics attaches diagnostics to the incident they explain.
func (s *Store) LinkDiagnostics(incidentID int64, ids []int64) error {
	if incidentID == 0 || len(ids) == 0 {
		return nil
	}
	args := []any{incidentID}
	for _, id := range ids {
		args = append(args, id)
	}
	_, err := s.db.Exec(`UPDATE diagnostics SET incident_id = ? WHERE incident_id = 0 AND id IN (?`+strings.Repeat(", ?", len(ids)-1)+`)`, args...)
	return err
}

// IncidentDiagnostics returns the diagnostics of an incident, oldest first.
func (s *Store) IncidentDiagnostics(incidentID int64) ([]*Diagnostic, error) {
	return s.diagnostics(`SELECT id, monitor_id, incident_id, location, at, data FROM diagnostics WHERE incident_id = ? ORDER BY at, id`, incidentID)
}

// MonitorDiagnostics returns the latest diagnostics of a monitor taken at
// or after since (unix ms), newest first.
func (s *Store) MonitorDiagnostics(monitorID, since int64, limit int) ([]*Diagnostic, error) {
	return s.diagnostics(`SELECT id, monitor_id, incident_id, location, at, data FROM diagnostics WHERE monitor_id = ? AND at >= ? ORDER BY at DESC, id DESC LIMIT ?`, monitorID, since, limit)
}

// DiagnosedIncidents reports which of the given incidents have diagnostics.
func (s *Store) DiagnosedIncidents(ids []int64) (map[int64]bool, error) {
	out := map[int64]bool{}
	if len(ids) == 0 {
		return out, nil
	}
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	rows, err := s.db.Query(`SELECT DISTINCT incident_id FROM diagnostics WHERE incident_id IN (?`+strings.Repeat(", ?", len(ids)-1)+`)`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = true
	}
	return out, rows.Err()
}

func (s *Store) diagnostics(q string, args ...any) ([]*Diagnostic, error) {
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Diagnostic{}
	for rows.Next() {
		d := &Diagnostic{}
		var data string
		if err := rows.Scan(&d.ID, &d.MonitorID, &d.IncidentID, &d.Location, &d.At, &data); err != nil {
			return nil, err
		}
		d.Data = json.RawMessage(data)
		out = append(out, d)
	}
	return out, rows.Err()
}

// PruneDiagnostics deletes diagnostics not linked to an incident taken
// before unlinkedBefore, and all of them taken before before (unix ms).
func (s *Store) PruneDiagnostics(unlinkedBefore, before int64) (int64, error) {
	res, err := s.db.Exec(`DELETE FROM diagnostics WHERE (incident_id = 0 AND at < ?) OR at < ?`, unlinkedBefore, before)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
