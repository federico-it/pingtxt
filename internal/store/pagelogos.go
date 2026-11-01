package store

import (
	"database/sql"
	"errors"
)

// PageLogo is the logo of a status page. A vector logo is kept as SVG
// only; a raster one as PNG (for old browsers), WebP and AVIF.
type PageLogo struct {
	PageID    int64
	SVG       []byte
	PNG       []byte
	WebP      []byte
	AVIF      []byte
	Width     int // pixels of the raster images, or the SVG aspect ratio
	Height    int
	UpdatedAt int64
}

// PageLogo returns the logo of a status page, ErrNotFound if it has none.
func (s *Store) PageLogo(pageID int64) (*PageLogo, error) {
	l := &PageLogo{PageID: pageID}
	err := s.db.QueryRow(`SELECT svg, png, webp, avif, width, height, updated_at FROM status_page_logos WHERE page_id = ?`, pageID).
		Scan(&l.SVG, &l.PNG, &l.WebP, &l.AVIF, &l.Width, &l.Height, &l.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return l, err
}

// PageLogoInfo describes a logo without its images.
type PageLogoInfo struct {
	SVG       bool
	Width     int
	Height    int
	UpdatedAt int64
}

// PageLogoInfo returns what a page needs to link its logo, ErrNotFound if
// it has none.
func (s *Store) PageLogoInfo(pageID int64) (*PageLogoInfo, error) {
	l := &PageLogoInfo{}
	var svg int
	err := s.db.QueryRow(`SELECT CASE WHEN svg IS NULL THEN 0 ELSE 1 END, width, height, updated_at FROM status_page_logos WHERE page_id = ?`, pageID).
		Scan(&svg, &l.Width, &l.Height, &l.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	l.SVG = svg == 1
	return l, err
}

// SetPageLogo replaces the logo of a status page.
func (s *Store) SetPageLogo(l *PageLogo) error {
	l.UpdatedAt = now()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM status_page_logos WHERE page_id = ?`, l.PageID); err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO status_page_logos (page_id, svg, png, webp, avif, width, height, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		l.PageID, nilIfEmpty(l.SVG), nilIfEmpty(l.PNG), nilIfEmpty(l.WebP), nilIfEmpty(l.AVIF), l.Width, l.Height, l.UpdatedAt); err != nil {
		return err
	}
	return tx.Commit()
}

// DeletePageLogo removes the logo of a status page.
func (s *Store) DeletePageLogo(pageID int64) error {
	_, err := s.db.Exec(`DELETE FROM status_page_logos WHERE page_id = ?`, pageID)
	return err
}

func nilIfEmpty(b []byte) any {
	if len(b) == 0 {
		return nil
	}
	return b
}
