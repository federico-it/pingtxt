package store

// Expiry check kinds.
const (
	ExpiryCert   = "cert"
	ExpiryDomain = "domain"
)

// ExpiryCheck is the last known expiry of a monitor's TLS certificate or
// domain registration, with the alert state.
type ExpiryCheck struct {
	MonitorID int64  `json:"monitor_id"`
	Kind      string `json:"kind"`       // "cert" or "domain"
	Subject   string `json:"subject"`    // host name (cert) or registrable domain
	ExpiresAt int64  `json:"expires_at"` // unix ms, 0 = unknown
	Issuer    string `json:"issuer"`     // certificate issuer or registrar
	Source    string `json:"source"`     // "tls", "rdap" or "whois"
	Error     string `json:"error"`      // last lookup error, if any
	CheckedAt int64  `json:"checked_at"` // unix ms
	// AlertedDays is the threshold (in days) of the last alert sent,
	// 0 when the "expired" alert was sent, -1 when no alert is pending.
	AlertedDays int `json:"-"`
}

const expiryCols = `monitor_id, kind, subject, expires_at, issuer, source, error, checked_at, alerted_days`

// ExpiryChecks returns the expiry checks of a monitor (all monitors when id is 0).
func (s *Store) ExpiryChecks(monitorID int64) ([]*ExpiryCheck, error) {
	q := `SELECT ` + expiryCols + ` FROM expiry_checks`
	args := []any{}
	if monitorID > 0 {
		q += ` WHERE monitor_id = ?`
		args = append(args, monitorID)
	}
	rows, err := s.db.Query(q+` ORDER BY monitor_id, kind`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*ExpiryCheck{}
	for rows.Next() {
		e := &ExpiryCheck{}
		if err := rows.Scan(&e.MonitorID, &e.Kind, &e.Subject, &e.ExpiresAt, &e.Issuer, &e.Source, &e.Error, &e.CheckedAt, &e.AlertedDays); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// SaveExpiryCheck inserts or replaces a check.
func (s *Store) SaveExpiryCheck(e *ExpiryCheck) error {
	_, err := s.db.Exec(`INSERT INTO expiry_checks (`+expiryCols+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(monitor_id, kind) DO UPDATE SET subject = excluded.subject, expires_at = excluded.expires_at,
		issuer = excluded.issuer, source = excluded.source, error = excluded.error, checked_at = excluded.checked_at,
		alerted_days = excluded.alerted_days`,
		e.MonitorID, e.Kind, e.Subject, e.ExpiresAt, e.Issuer, e.Source, e.Error, e.CheckedAt, e.AlertedDays)
	return err
}

// DeleteExpiryCheck removes a check (the alert was turned off or no longer applies).
func (s *Store) DeleteExpiryCheck(monitorID int64, kind string) error {
	_, err := s.db.Exec(`DELETE FROM expiry_checks WHERE monitor_id = ? AND kind = ?`, monitorID, kind)
	return err
}
