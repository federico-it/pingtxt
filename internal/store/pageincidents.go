package store

import (
	"database/sql"
	"errors"
	"strings"
)

// Incident report states, in their usual order.
const (
	ReportInvestigating = "investigating"
	ReportIdentified    = "identified"
	ReportMonitoring    = "monitoring"
	ReportResolved      = "resolved"
)

// Incident report impacts.
const (
	ImpactNone     = "none"
	ImpactMinor    = "minor"
	ImpactMajor    = "major"
	ImpactCritical = "critical"
)

// PageIncident is an incident written by hand on a status page, with a
// timeline of updates ("investigating", ..., "resolved"). Unlike Incident,
// which the hub opens when a monitor goes down, it is a message to the
// page's visitors.
type PageIncident struct {
	ID         int64                `json:"id"`
	PageID     int64                `json:"page_id"`
	Title      string               `json:"title"`
	Impact     string               `json:"impact"`
	Status     string               `json:"status"` // status of the latest update
	CreatedAt  int64                `json:"created_at"`
	UpdatedAt  int64                `json:"updated_at"`
	ResolvedAt int64                `json:"resolved_at"` // 0 = still open
	Updates    []PageIncidentUpdate `json:"updates"`     // newest first
}

// PageIncidentUpdate is one message of a PageIncident timeline.
type PageIncidentUpdate struct {
	ID         int64  `json:"id"`
	IncidentID int64  `json:"incident_id"`
	Status     string `json:"status"`
	Message    string `json:"message"`
	CreatedAt  int64  `json:"created_at"`
}

const pageIncidentCols = `id, page_id, title, impact, status, created_at, updated_at, resolved_at`

func scanPageIncident(sc interface{ Scan(...any) error }) (*PageIncident, error) {
	i := &PageIncident{Updates: []PageIncidentUpdate{}}
	err := sc.Scan(&i.ID, &i.PageID, &i.Title, &i.Impact, &i.Status, &i.CreatedAt, &i.UpdatedAt, &i.ResolvedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return i, err
}

// loadUpdates fills the timelines of the incidents with one query.
func (s *Store) loadUpdates(list []*PageIncident) error {
	if len(list) == 0 {
		return nil
	}
	byID := map[int64]*PageIncident{}
	args := make([]any, len(list))
	for n, i := range list {
		byID[i.ID] = i
		args[n] = i.ID
	}
	rows, err := s.db.Query(`SELECT id, incident_id, status, message, created_at FROM page_incident_updates
		WHERE incident_id IN (?`+strings.Repeat(`, ?`, len(list)-1)+`) ORDER BY created_at DESC, id DESC`, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var u PageIncidentUpdate
		if err := rows.Scan(&u.ID, &u.IncidentID, &u.Status, &u.Message, &u.CreatedAt); err != nil {
			return err
		}
		if i := byID[u.IncidentID]; i != nil {
			i.Updates = append(i.Updates, u)
		}
	}
	return rows.Err()
}

func (s *Store) queryPageIncidents(q string, args ...any) ([]*PageIncident, error) {
	rows, err := s.db.Query(`SELECT `+pageIncidentCols+` FROM page_incidents WHERE `+q, args...)
	if err != nil {
		return nil, err
	}
	out := []*PageIncident{}
	for rows.Next() {
		i, err := scanPageIncident(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, i)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// After closing the cursor: SQLite has a single connection.
	return out, s.loadUpdates(out)
}

// PageIncidents lists the incidents of a page, open ones first, then by
// start, newest first. limit <= 0 means all.
func (s *Store) PageIncidents(pageID int64, limit int) ([]*PageIncident, error) {
	q := `page_id = ? ORDER BY CASE WHEN resolved_at = 0 THEN 0 ELSE 1 END, created_at DESC, id DESC`
	args := []any{pageID}
	if limit > 0 {
		q += ` LIMIT ?`
		args = append(args, limit)
	}
	return s.queryPageIncidents(q, args...)
}

// PublicPageIncidents returns the open incidents of a page and those resolved
// after `since`, newest first.
func (s *Store) PublicPageIncidents(pageID, since int64, limit int) ([]*PageIncident, error) {
	return s.queryPageIncidents(`page_id = ? AND (resolved_at = 0 OR resolved_at >= ?)
		ORDER BY CASE WHEN resolved_at = 0 THEN 0 ELSE 1 END, created_at DESC, id DESC LIMIT ?`, pageID, since, limit)
}

// PageIncident returns one incident of a page.
func (s *Store) PageIncident(pageID, id int64) (*PageIncident, error) {
	list, err := s.queryPageIncidents(`page_id = ? AND id = ?`, pageID, id)
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, ErrNotFound
	}
	return list[0], nil
}

func resolvedAt(status string, at int64) int64 {
	if status == ReportResolved {
		return at
	}
	return 0
}

// CreatePageIncident stores an incident and its first update.
func (s *Store) CreatePageIncident(i *PageIncident, first *PageIncidentUpdate) error {
	t := now()
	i.CreatedAt, i.UpdatedAt = t, t
	i.Status = first.Status
	i.ResolvedAt = resolvedAt(first.Status, t)
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	id, err := s.db.insert(tx, `INSERT INTO page_incidents (page_id, title, impact, status, created_at, updated_at, resolved_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		i.PageID, i.Title, i.Impact, i.Status, i.CreatedAt, i.UpdatedAt, i.ResolvedAt)
	if err != nil {
		return err
	}
	i.ID = id
	first.IncidentID, first.CreatedAt = id, t
	if first.ID, err = s.db.insert(tx, `INSERT INTO page_incident_updates (incident_id, status, message, created_at) VALUES (?, ?, ?, ?)`,
		id, first.Status, first.Message, t); err != nil {
		return err
	}
	i.Updates = []PageIncidentUpdate{*first}
	return tx.Commit()
}

// UpdatePageIncident changes the title and impact of an incident.
func (s *Store) UpdatePageIncident(i *PageIncident) error {
	res, err := s.db.Exec(`UPDATE page_incidents SET title = ?, impact = ? WHERE id = ? AND page_id = ?`, i.Title, i.Impact, i.ID, i.PageID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// DeletePageIncident removes an incident and its updates.
func (s *Store) DeletePageIncident(pageID, id int64) error {
	res, err := s.db.Exec(`DELETE FROM page_incidents WHERE id = ? AND page_id = ?`, id, pageID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// AddPageIncidentUpdate appends an update; the incident takes its status.
func (s *Store) AddPageIncidentUpdate(pageID int64, u *PageIncidentUpdate) error {
	t := now()
	u.CreatedAt = t
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var resolved int64
	err = tx.QueryRow(`SELECT resolved_at FROM page_incidents WHERE id = ? AND page_id = ?`, u.IncidentID, pageID).Scan(&resolved)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	} else if err != nil {
		return err
	}
	// A resolved incident keeps its first resolution time if resolved again.
	switch {
	case u.Status != ReportResolved:
		resolved = 0
	case resolved == 0:
		resolved = t
	}
	if _, err := tx.Exec(`UPDATE page_incidents SET status = ?, updated_at = ?, resolved_at = ? WHERE id = ?`,
		u.Status, t, resolved, u.IncidentID); err != nil {
		return err
	}
	if u.ID, err = s.db.insert(tx, `INSERT INTO page_incident_updates (incident_id, status, message, created_at) VALUES (?, ?, ?, ?)`,
		u.IncidentID, u.Status, u.Message, t); err != nil {
		return err
	}
	return tx.Commit()
}

// DeletePageIncidentUpdate removes an update. The last one cannot go: the
// incident would have no status. The incident takes the status of the
// latest update left.
func (s *Store) DeletePageIncidentUpdate(pageID, incidentID, id int64) error {
	i, err := s.PageIncident(pageID, incidentID)
	if err != nil {
		return err
	}
	var left []PageIncidentUpdate
	found := false
	for _, u := range i.Updates {
		if u.ID == id {
			found = true
			continue
		}
		left = append(left, u)
	}
	if !found {
		return ErrNotFound
	}
	if len(left) == 0 {
		return ErrConflict
	}
	latest := left[0] // updates are newest first
	resolved := int64(0)
	if latest.Status == ReportResolved {
		resolved = latest.CreatedAt
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM page_incident_updates WHERE id = ? AND incident_id = ?`, id, incidentID); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE page_incidents SET status = ?, resolved_at = ? WHERE id = ?`, latest.Status, resolved, incidentID); err != nil {
		return err
	}
	return tx.Commit()
}

// ---------- subscribers ----------

// PageSubscriber is an email address that gets the incident updates of a
// status page. It receives nothing until it confirms with Token, which also
// unsubscribes it.
type PageSubscriber struct {
	ID          int64  `json:"id"`
	PageID      int64  `json:"page_id"`
	Email       string `json:"email"`
	Token       string `json:"-"`
	ConfirmedAt int64  `json:"confirmed_at"` // 0 = waiting for confirmation
	CreatedAt   int64  `json:"created_at"`
}

const subscriberCols = `id, page_id, email, token, confirmed_at, created_at`

func scanSubscriber(sc interface{ Scan(...any) error }) (*PageSubscriber, error) {
	p := &PageSubscriber{}
	err := sc.Scan(&p.ID, &p.PageID, &p.Email, &p.Token, &p.ConfirmedAt, &p.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return p, err
}

func (s *Store) querySubscribers(q string, args ...any) ([]*PageSubscriber, error) {
	rows, err := s.db.Query(`SELECT `+subscriberCols+` FROM page_subscribers WHERE `+q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*PageSubscriber{}
	for rows.Next() {
		p, err := scanSubscriber(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// PageSubscribers lists the subscribers of a page by email. confirmedOnly
// leaves out those that did not confirm yet.
func (s *Store) PageSubscribers(pageID int64, confirmedOnly bool) ([]*PageSubscriber, error) {
	q := `page_id = ?`
	if confirmedOnly {
		q += ` AND confirmed_at > 0`
	}
	return s.querySubscribers(q+` ORDER BY email`, pageID)
}

// CountPageSubscribers returns how many addresses a page has, confirmed or not.
func (s *Store) CountPageSubscribers(pageID int64) (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM page_subscribers WHERE page_id = ?`, pageID).Scan(&n)
	return n, err
}

// PageSubscriberByEmail finds an address on a page.
func (s *Store) PageSubscriberByEmail(pageID int64, email string) (*PageSubscriber, error) {
	return scanSubscriber(s.db.QueryRow(`SELECT `+subscriberCols+` FROM page_subscribers WHERE page_id = ? AND email = ?`, pageID, email))
}

// PageSubscriberByToken finds a subscriber from the token of its links.
func (s *Store) PageSubscriberByToken(token string) (*PageSubscriber, error) {
	if token == "" {
		return nil, ErrNotFound
	}
	return scanSubscriber(s.db.QueryRow(`SELECT `+subscriberCols+` FROM page_subscribers WHERE token = ?`, token))
}

// CreatePageSubscriber adds an unconfirmed address.
func (s *Store) CreatePageSubscriber(p *PageSubscriber) error {
	p.CreatedAt = now()
	p.ConfirmedAt = 0
	id, err := s.db.insert(s.db, `INSERT INTO page_subscribers (page_id, email, token, confirmed_at, created_at) VALUES (?, ?, ?, 0, ?)`,
		p.PageID, p.Email, p.Token, p.CreatedAt)
	if err != nil {
		return uniqueErr(err)
	}
	p.ID = id
	return nil
}

// RenewPageSubscriber gives an unconfirmed address a new token and creation
// time, when it asks for the confirmation email again.
func (s *Store) RenewPageSubscriber(p *PageSubscriber) error {
	p.CreatedAt = now()
	_, err := s.db.Exec(`UPDATE page_subscribers SET token = ?, created_at = ? WHERE id = ? AND confirmed_at = 0`, p.Token, p.CreatedAt, p.ID)
	return err
}

// ConfirmPageSubscriber marks the address as confirmed.
func (s *Store) ConfirmPageSubscriber(id int64) error {
	_, err := s.db.Exec(`UPDATE page_subscribers SET confirmed_at = ? WHERE id = ? AND confirmed_at = 0`, now(), id)
	return err
}

// DeletePageSubscriber removes an address from a page.
func (s *Store) DeletePageSubscriber(pageID, id int64) error {
	res, err := s.db.Exec(`DELETE FROM page_subscribers WHERE id = ? AND page_id = ?`, id, pageID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// PrunePageSubscribers drops the addresses that never confirmed and were
// created before `before`.
func (s *Store) PrunePageSubscribers(before int64) (int64, error) {
	res, err := s.db.Exec(`DELETE FROM page_subscribers WHERE confirmed_at = 0 AND created_at < ?`, before)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
