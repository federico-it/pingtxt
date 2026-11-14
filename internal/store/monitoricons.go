package store

import (
	"database/sql"
	"errors"
)

// MonitorIcon is the icon image of a monitor: a cleaned SVG or a PNG, with
// an optional variant for dark themes.
type MonitorIcon struct {
	MonitorID int64
	// Source is what produced the image: "favicon", "custom" or
	// "dashboard". Ref is the page the favicon was looked up on, or the
	// Dashboard Icons name.
	Source    string
	Ref       string
	Image     []byte
	ImageType string
	Dark      []byte
	DarkType  string
	// CheckedAt is when the image was fetched or looked for (unix ms).
	CheckedAt int64
}

// MonitorIcon returns the icon of a monitor, ErrNotFound if it has none.
// A favicon that was looked for and not found has no Image.
func (s *Store) MonitorIcon(monitorID int64) (*MonitorIcon, error) {
	i := &MonitorIcon{MonitorID: monitorID}
	err := s.db.QueryRow(`SELECT source, ref, image, image_type, dark, dark_type, checked_at FROM monitor_icons WHERE monitor_id = ?`, monitorID).
		Scan(&i.Source, &i.Ref, &i.Image, &i.ImageType, &i.Dark, &i.DarkType, &i.CheckedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return i, err
}

// MonitorIconRefs returns the source and ref of every stored icon, by
// monitor, without the images.
func (s *Store) MonitorIconRefs() (map[int64]*MonitorIcon, error) {
	rows, err := s.db.Query(`SELECT monitor_id, source, ref, checked_at, CASE WHEN image IS NULL THEN 0 ELSE 1 END FROM monitor_icons`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]*MonitorIcon{}
	for rows.Next() {
		i := &MonitorIcon{}
		var has int
		if err := rows.Scan(&i.MonitorID, &i.Source, &i.Ref, &i.CheckedAt, &has); err != nil {
			return nil, err
		}
		if has == 1 {
			i.Image = []byte{} // marks that there is one
		}
		out[i.MonitorID] = i
	}
	return out, rows.Err()
}

// SetMonitorIcon replaces the icon of a monitor and records on the monitor
// whether there is an image. i.Image may be empty: a favicon that was not
// found, kept so the hub does not look again at every start.
func (s *Store) SetMonitorIcon(i *MonitorIcon) error {
	if i.CheckedAt == 0 {
		i.CheckedAt = now()
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM monitor_icons WHERE monitor_id = ?`, i.MonitorID); err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO monitor_icons (monitor_id, source, ref, image, image_type, dark, dark_type, checked_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		i.MonitorID, i.Source, i.Ref, nilIfEmpty(i.Image), i.ImageType, nilIfEmpty(i.Dark), i.DarkType, i.CheckedAt); err != nil {
		return err
	}
	var v int64
	if len(i.Image) > 0 {
		v = i.CheckedAt
	}
	if _, err := tx.Exec(`UPDATE monitors SET icon_v = ?, icon_dark = ? WHERE id = ?`, v, b2i(len(i.Image) > 0 && len(i.Dark) > 0), i.MonitorID); err != nil {
		return err
	}
	return tx.Commit()
}

// DeleteMonitorIcon removes the icon image of a monitor.
func (s *Store) DeleteMonitorIcon(monitorID int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM monitor_icons WHERE monitor_id = ?`, monitorID); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE monitors SET icon_v = 0, icon_dark = 0 WHERE id = ?`, monitorID); err != nil {
		return err
	}
	return tx.Commit()
}

// SetMonitorIconSetting changes only Monitor.Icon.
func (s *Store) SetMonitorIconSetting(monitorID int64, icon string) error {
	_, err := s.db.Exec(`UPDATE monitors SET icon = ? WHERE id = ?`, icon, monitorID)
	return err
}
