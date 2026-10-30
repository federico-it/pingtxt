package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"
)

// Maintenance kinds.
const (
	MaintOnce    = "once"
	MaintDaily   = "daily"
	MaintWeekly  = "weekly"
	MaintMonthly = "monthly"
)

// MaxMaintDurationMin caps the length of one recurring occurrence (7 days).
const MaxMaintDurationMin = 7 * 24 * 60

// Maintenance is a planned window during which alerts and incidents are
// suppressed for the monitors it covers.
//
// A "once" window runs from StartAt to EndAt (unix ms). Recurring windows
// (daily, weekly, monthly) start at StartTime ("HH:MM", in Timezone) on the
// matching days and last DurationMin minutes; for them StartAt/EndAt are an
// optional validity range (0 = no limit) for the occurrence start.
type Maintenance struct {
	ID          int64   `json:"id"`
	Name        string  `json:"name"`
	Description string  `json:"description"`
	AllMonitors bool    `json:"all_monitors"`
	MonitorIDs  []int64 `json:"monitor_ids"`
	Kind        string  `json:"kind"`
	StartAt     int64   `json:"start_at"`
	EndAt       int64   `json:"end_at"`
	StartTime   string  `json:"start_time"`
	DurationMin int     `json:"duration_min"`
	Weekdays    []int   `json:"weekdays"`   // 0 = Sunday … 6 = Saturday
	MonthDays   []int   `json:"month_days"` // 1 … 31
	Timezone    string  `json:"timezone"`
	Enabled     bool    `json:"enabled"`
	CreatedAt   int64   `json:"created_at"`
}

// Covers reports whether the window applies to a monitor.
func (m *Maintenance) Covers(monitorID int64) bool {
	return m.AllMonitors || slices.Contains(m.MonitorIDs, monitorID)
}

var tzCache sync.Map

// Location returns the window's time zone (UTC when unknown).
func (m *Maintenance) Location() *time.Location {
	name := m.Timezone
	if name == "" {
		return time.UTC
	}
	if l, ok := tzCache.Load(name); ok {
		return l.(*time.Location)
	}
	l, err := time.LoadLocation(name)
	if err != nil {
		return time.UTC
	}
	tzCache.Store(name, l)
	return l
}

// ParseClock parses "HH:MM".
func ParseClock(s string) (hour, min int, err error) {
	t, err := time.Parse("15:04", s)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid time %q, expected HH:MM", s)
	}
	return t.Hour(), t.Minute(), nil
}

func (m *Maintenance) matchesDay(d time.Time) bool {
	switch m.Kind {
	case MaintDaily:
		return true
	case MaintWeekly:
		return slices.Contains(m.Weekdays, int(d.Weekday()))
	case MaintMonthly:
		return slices.Contains(m.MonthDays, d.Day())
	}
	return false
}

// startOn returns the occurrence starting on the calendar day of d.
func (m *Maintenance) startOn(d time.Time, hh, mm int) time.Time {
	return time.Date(d.Year(), d.Month(), d.Day(), hh, mm, 0, 0, d.Location())
}

func (m *Maintenance) inRange(start time.Time) bool {
	ms := start.UnixMilli()
	if m.StartAt > 0 && ms < m.StartAt {
		return false
	}
	if m.EndAt > 0 && ms >= m.EndAt {
		return false
	}
	return true
}

// ActiveAt returns the occurrence that contains t, if any.
func (m *Maintenance) ActiveAt(t time.Time) (start, end time.Time, ok bool) {
	if !m.Enabled {
		return
	}
	if m.Kind == MaintOnce {
		ms := t.UnixMilli()
		if m.StartAt <= ms && ms < m.EndAt {
			return time.UnixMilli(m.StartAt), time.UnixMilli(m.EndAt), true
		}
		return
	}
	hh, mm, err := ParseClock(m.StartTime)
	if err != nil || m.DurationMin <= 0 {
		return
	}
	dur := time.Duration(m.DurationMin) * time.Minute
	lt := t.In(m.Location())
	// An occurrence may have started on one of the previous days.
	for k := 0; k <= m.DurationMin/(24*60)+1; k++ {
		day := lt.AddDate(0, 0, -k)
		if !m.matchesDay(day) {
			continue
		}
		s := m.startOn(day, hh, mm)
		if !s.After(t) && t.Before(s.Add(dur)) && m.inRange(s) {
			return s, s.Add(dur), true
		}
	}
	return
}

// NextAfter returns the first occurrence starting after t (within ~13 months).
func (m *Maintenance) NextAfter(t time.Time) (start, end time.Time, ok bool) {
	if !m.Enabled {
		return
	}
	if m.Kind == MaintOnce {
		if m.StartAt > t.UnixMilli() {
			return time.UnixMilli(m.StartAt), time.UnixMilli(m.EndAt), true
		}
		return
	}
	hh, mm, err := ParseClock(m.StartTime)
	if err != nil || m.DurationMin <= 0 {
		return
	}
	dur := time.Duration(m.DurationMin) * time.Minute
	lt := t.In(m.Location())
	for k := 0; k <= 400; k++ {
		day := lt.AddDate(0, 0, k)
		s := m.startOn(day, hh, mm)
		if m.EndAt > 0 && s.UnixMilli() >= m.EndAt {
			return
		}
		if s.After(t) && m.matchesDay(day) && m.inRange(s) {
			return s, s.Add(dur), true
		}
	}
	return
}

const maintCols = `id, name, description, all_monitors, monitor_ids, kind, start_at, end_at, start_time, duration_min, weekdays, month_days, timezone, enabled, created_at`

func scanMaintenance(sc interface{ Scan(...any) error }) (*Maintenance, error) {
	m := &Maintenance{}
	var ids, wd, md string
	err := sc.Scan(&m.ID, &m.Name, &m.Description, &m.AllMonitors, &ids, &m.Kind, &m.StartAt, &m.EndAt, &m.StartTime, &m.DurationMin, &wd, &md, &m.Timezone, &m.Enabled, &m.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	_ = json.Unmarshal([]byte(ids), &m.MonitorIDs)
	_ = json.Unmarshal([]byte(wd), &m.Weekdays)
	_ = json.Unmarshal([]byte(md), &m.MonthDays)
	if m.MonitorIDs == nil {
		m.MonitorIDs = []int64{}
	}
	if m.Weekdays == nil {
		m.Weekdays = []int{}
	}
	if m.MonthDays == nil {
		m.MonthDays = []int{}
	}
	return m, nil
}

func (s *Store) ListMaintenances() ([]*Maintenance, error) {
	rows, err := s.db.Query(`SELECT ` + maintCols + ` FROM maintenances ORDER BY name COLLATE NOCASE`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Maintenance{}
	for rows.Next() {
		m, err := scanMaintenance(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *Store) Maintenance(id int64) (*Maintenance, error) {
	return scanMaintenance(s.db.QueryRow(`SELECT `+maintCols+` FROM maintenances WHERE id = ?`, id))
}

func maintJSON(m *Maintenance) (ids, wd, md string) {
	a, _ := json.Marshal(m.MonitorIDs)
	b, _ := json.Marshal(m.Weekdays)
	c, _ := json.Marshal(m.MonthDays)
	return string(a), string(b), string(c)
}

func (s *Store) CreateMaintenance(m *Maintenance) error {
	ids, wd, md := maintJSON(m)
	m.CreatedAt = now()
	res, err := s.db.Exec(`INSERT INTO maintenances (name, description, all_monitors, monitor_ids, kind, start_at, end_at, start_time, duration_min, weekdays, month_days, timezone, enabled, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		m.Name, m.Description, b2i(m.AllMonitors), ids, m.Kind, m.StartAt, m.EndAt, m.StartTime, m.DurationMin, wd, md, m.Timezone, b2i(m.Enabled), m.CreatedAt)
	if err != nil {
		return err
	}
	m.ID, _ = res.LastInsertId()
	return nil
}

func (s *Store) UpdateMaintenance(m *Maintenance) error {
	ids, wd, md := maintJSON(m)
	_, err := s.db.Exec(`UPDATE maintenances SET name = ?, description = ?, all_monitors = ?, monitor_ids = ?, kind = ?, start_at = ?, end_at = ?, start_time = ?, duration_min = ?, weekdays = ?, month_days = ?, timezone = ?, enabled = ? WHERE id = ?`,
		m.Name, m.Description, b2i(m.AllMonitors), ids, m.Kind, m.StartAt, m.EndAt, m.StartTime, m.DurationMin, wd, md, m.Timezone, b2i(m.Enabled), m.ID)
	return err
}

func (s *Store) DeleteMaintenance(id int64) error {
	_, err := s.db.Exec(`DELETE FROM maintenances WHERE id = ?`, id)
	return err
}

// HasOpenIncident reports whether a monitor has an unresolved incident.
func (s *Store) HasOpenIncident(monitorID int64) (bool, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM incidents WHERE monitor_id = ? AND resolved_at = 0`, monitorID).Scan(&n)
	return n > 0, err
}
