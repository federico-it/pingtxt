package store

import (
	"database/sql"
	"errors"
	"strings"
)

// StatusPage is a public page showing the state of selected monitors.
type StatusPage struct {
	ID            int64   `json:"id"`
	Slug          string  `json:"slug"`
	Title         string  `json:"title"`
	Description   string  `json:"description"`
	Domain        string  `json:"domain"`
	Published     bool    `json:"published"`
	ShowIncidents bool    `json:"show_incidents"`
	MonitorIDs    []int64 `json:"monitor_ids"`
	// GroupIDs adds every monitor of these groups, current and future.
	GroupIDs  []int64 `json:"group_ids"`
	CreatedAt int64   `json:"created_at"`
}

const statusPageCols = `id, slug, title, description, domain, published, show_incidents, created_at`

func (s *Store) scanStatusPage(sc interface{ Scan(...any) error }) (*StatusPage, error) {
	p := &StatusPage{}
	err := sc.Scan(&p.ID, &p.Slug, &p.Title, &p.Description, &p.Domain, &p.Published, &p.ShowIncidents, &p.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return p, err
}

func (s *Store) loadPageMonitors(p *StatusPage) error {
	rows, err := s.db.Query(`SELECT monitor_id FROM status_page_monitors WHERE page_id = ? ORDER BY position, monitor_id`, p.ID)
	if err != nil {
		return err
	}
	defer rows.Close()
	p.MonitorIDs = []int64{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return err
		}
		p.MonitorIDs = append(p.MonitorIDs, id)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	rows.Close()
	grows, err := s.db.Query(`SELECT group_id FROM status_page_groups WHERE page_id = ? ORDER BY position, group_id`, p.ID)
	if err != nil {
		return err
	}
	defer grows.Close()
	p.GroupIDs = []int64{}
	for grows.Next() {
		var id int64
		if err := grows.Scan(&id); err != nil {
			return err
		}
		p.GroupIDs = append(p.GroupIDs, id)
	}
	return grows.Err()
}

func (s *Store) ListStatusPages() ([]*StatusPage, error) {
	rows, err := s.db.Query(`SELECT ` + statusPageCols + ` FROM status_pages ORDER BY LOWER(title)`)
	if err != nil {
		return nil, err
	}
	out := []*StatusPage{}
	for rows.Next() {
		p, err := s.scanStatusPage(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, p)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// Loaded after closing the cursor: the pool has a single connection.
	for _, p := range out {
		if err := s.loadPageMonitors(p); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (s *Store) statusPageWhere(where string, arg any) (*StatusPage, error) {
	p, err := s.scanStatusPage(s.db.QueryRow(`SELECT `+statusPageCols+` FROM status_pages WHERE `+where, arg))
	if err != nil {
		return nil, err
	}
	return p, s.loadPageMonitors(p)
}

func (s *Store) StatusPage(id int64) (*StatusPage, error) {
	return s.statusPageWhere(`id = ?`, id)
}

func (s *Store) StatusPageBySlug(slug string) (*StatusPage, error) {
	return s.statusPageWhere(`slug = ?`, slug)
}

func (s *Store) StatusPageByDomain(domain string) (*StatusPage, error) {
	if domain == "" {
		return nil, ErrNotFound
	}
	return s.statusPageWhere(`domain = ?`, domain)
}

func uniqueErr(err error) error {
	if isUnique(err) {
		return ErrConflict
	}
	return err
}

func setPageMonitors(tx *tx, p *StatusPage) error {
	if _, err := tx.Exec(`DELETE FROM status_page_monitors WHERE page_id = ?`, p.ID); err != nil {
		return err
	}
	for i, id := range p.MonitorIDs {
		if _, err := tx.Exec(`INSERT INTO status_page_monitors (page_id, monitor_id, position) VALUES (?, ?, ?)`, p.ID, id, i); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(`DELETE FROM status_page_groups WHERE page_id = ?`, p.ID); err != nil {
		return err
	}
	for i, id := range p.GroupIDs {
		if _, err := tx.Exec(`INSERT INTO status_page_groups (page_id, group_id, position) VALUES (?, ?, ?)`, p.ID, id, i); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) CreateStatusPage(p *StatusPage) error {
	p.CreatedAt = now()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	id, err := s.db.insert(tx, `INSERT INTO status_pages (slug, title, description, domain, published, show_incidents, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		p.Slug, p.Title, p.Description, p.Domain, b2i(p.Published), b2i(p.ShowIncidents), p.CreatedAt)
	if err != nil {
		return uniqueErr(err)
	}
	p.ID = id
	if err := setPageMonitors(tx, p); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) UpdateStatusPage(p *StatusPage) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.Exec(`UPDATE status_pages SET slug = ?, title = ?, description = ?, domain = ?, published = ?, show_incidents = ? WHERE id = ?`,
		p.Slug, p.Title, p.Description, p.Domain, b2i(p.Published), b2i(p.ShowIncidents), p.ID)
	if err != nil {
		return uniqueErr(err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	if err := setPageMonitors(tx, p); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) DeleteStatusPage(id int64) error {
	_, err := s.db.Exec(`DELETE FROM status_pages WHERE id = ?`, id)
	return err
}

// StatusPageDomains maps every custom domain to its page slug.
func (s *Store) StatusPageDomains() (map[string]string, error) {
	rows, err := s.db.Query(`SELECT domain, slug FROM status_pages WHERE domain != ''`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var d, slug string
		if err := rows.Scan(&d, &slug); err != nil {
			return nil, err
		}
		out[d] = slug
	}
	return out, rows.Err()
}

// IncidentsSince returns incidents of the given monitors that started after
// `since` or are still open, newest first.
func (s *Store) IncidentsSince(monitorIDs []int64, since int64, limit int) ([]*Incident, error) {
	out := []*Incident{}
	if len(monitorIDs) == 0 {
		return out, nil
	}
	args := []any{}
	for _, id := range monitorIDs {
		args = append(args, id)
	}
	args = append(args, since, limit)
	q := `SELECT i.id, i.monitor_id, m.name, i.started_at, i.resolved_at, i.cause FROM incidents i JOIN monitors m ON m.id = i.monitor_id
		WHERE i.monitor_id IN (?` + strings.Repeat(`, ?`, len(monitorIDs)-1) + `) AND (i.started_at >= ? OR i.resolved_at = 0)
		ORDER BY i.started_at DESC LIMIT ?`
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		i := &Incident{}
		if err := rows.Scan(&i.ID, &i.MonitorID, &i.MonitorName, &i.StartedAt, &i.ResolvedAt, &i.Cause); err != nil {
			return nil, err
		}
		out = append(out, i)
	}
	return out, rows.Err()
}
