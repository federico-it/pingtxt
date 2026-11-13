package store

import (
	"database/sql"
	"encoding/json"
	"errors"
)

// Schedule is an on-call rotation: its members take turns, one shift each,
// from StartAt on. Overrides put someone else on call for a while.
type Schedule struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	// Timezone of the shift changes (IANA name): a daily or weekly shift
	// changes at the same local time all year.
	Timezone string `json:"timezone"`
	// Members are user ids in rotation order.
	Members []int64 `json:"members"`
	// A shift lasts ShiftLen ShiftUnit: "hours", "days" or "weeks".
	ShiftLen  int    `json:"shift_len"`
	ShiftUnit string `json:"shift_unit"`
	// StartAt is when the first member's first shift starts (unix ms).
	StartAt   int64 `json:"start_at"`
	CreatedAt int64 `json:"created_at"`
}

// Shift units.
const (
	ShiftHours = "hours"
	ShiftDays  = "days"
	ShiftWeeks = "weeks"
)

// Override puts a user on call from StartAt to EndAt (unix ms), whatever
// the rotation says.
type Override struct {
	ID         int64 `json:"id"`
	ScheduleID int64 `json:"schedule_id"`
	UserID     int64 `json:"user_id"`
	StartAt    int64 `json:"start_at"`
	EndAt      int64 `json:"end_at"`
	CreatedAt  int64 `json:"created_at"`
}

const scheduleCols = `id, name, description, timezone, members, shift_len, shift_unit, start_at, created_at`

func scanSchedule(sc interface{ Scan(...any) error }) (*Schedule, error) {
	s := &Schedule{}
	var members string
	if err := sc.Scan(&s.ID, &s.Name, &s.Description, &s.Timezone, &members, &s.ShiftLen, &s.ShiftUnit, &s.StartAt, &s.CreatedAt); err != nil {
		return nil, err
	}
	_ = json.Unmarshal([]byte(members), &s.Members)
	if s.Members == nil {
		s.Members = []int64{}
	}
	return s, nil
}

// ListSchedules returns the on-call schedules by name.
func (s *Store) ListSchedules() ([]*Schedule, error) {
	rows, err := s.db.Query(`SELECT ` + scheduleCols + ` FROM oncall_schedules ORDER BY name, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Schedule{}
	for rows.Next() {
		sc, err := scanSchedule(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, sc)
	}
	return out, rows.Err()
}

// Schedule returns one on-call schedule.
func (s *Store) Schedule(id int64) (*Schedule, error) {
	sc, err := scanSchedule(s.db.QueryRow(`SELECT `+scheduleCols+` FROM oncall_schedules WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return sc, err
}

// CreateSchedule stores a new schedule and sets its id.
func (s *Store) CreateSchedule(sc *Schedule) error {
	sc.CreatedAt = now()
	id, err := s.db.insert(s.db, `INSERT INTO oncall_schedules (name, description, timezone, members, shift_len, shift_unit, start_at, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		sc.Name, sc.Description, sc.Timezone, jsonIDs(sc.Members), sc.ShiftLen, sc.ShiftUnit, sc.StartAt, sc.CreatedAt)
	if err != nil {
		return err
	}
	sc.ID = id
	return nil
}

// UpdateSchedule saves a schedule.
func (s *Store) UpdateSchedule(sc *Schedule) error {
	res, err := s.db.Exec(`UPDATE oncall_schedules SET name = ?, description = ?, timezone = ?, members = ?, shift_len = ?, shift_unit = ?, start_at = ? WHERE id = ?`,
		sc.Name, sc.Description, sc.Timezone, jsonIDs(sc.Members), sc.ShiftLen, sc.ShiftUnit, sc.StartAt, sc.ID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteSchedule deletes a schedule and its overrides.
func (s *Store) DeleteSchedule(id int64) error {
	t, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer t.Rollback()
	if _, err := t.Exec(`DELETE FROM oncall_overrides WHERE schedule_id = ?`, id); err != nil {
		return err
	}
	if _, err := t.Exec(`DELETE FROM oncall_schedules WHERE id = ?`, id); err != nil {
		return err
	}
	return t.Commit()
}

// Overrides returns the overrides of a schedule that end after since (unix
// ms), by start.
func (s *Store) Overrides(scheduleID, since int64) ([]*Override, error) {
	rows, err := s.db.Query(`SELECT id, schedule_id, user_id, start_at, end_at, created_at FROM oncall_overrides WHERE schedule_id = ? AND end_at > ? ORDER BY start_at, id`, scheduleID, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Override{}
	for rows.Next() {
		o := &Override{}
		if err := rows.Scan(&o.ID, &o.ScheduleID, &o.UserID, &o.StartAt, &o.EndAt, &o.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// CreateOverride stores an override and sets its id.
func (s *Store) CreateOverride(o *Override) error {
	o.CreatedAt = now()
	id, err := s.db.insert(s.db, `INSERT INTO oncall_overrides (schedule_id, user_id, start_at, end_at, created_at) VALUES (?, ?, ?, ?, ?)`,
		o.ScheduleID, o.UserID, o.StartAt, o.EndAt, o.CreatedAt)
	if err != nil {
		return err
	}
	o.ID = id
	return nil
}

// DeleteOverride deletes an override of a schedule.
func (s *Store) DeleteOverride(scheduleID, id int64) error {
	res, err := s.db.Exec(`DELETE FROM oncall_overrides WHERE id = ? AND schedule_id = ?`, id, scheduleID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}
