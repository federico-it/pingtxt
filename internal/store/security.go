package store

import (
	"database/sql"
	"errors"
)

// ---------- two-factor authentication (TOTP) ----------

// TOTP is a user's authenticator app secret. Enabled is false while the user
// is still setting it up (secret shown, first code not confirmed yet).
type TOTP struct {
	Secret   string
	Enabled  bool
	LastStep int64
}

// UserTOTP returns the user's TOTP secret, or ErrNotFound.
func (s *Store) UserTOTP(userID int64) (*TOTP, error) {
	t := &TOTP{}
	err := s.db.QueryRow(`SELECT secret, enabled, last_step FROM user_totp WHERE user_id = ?`, userID).Scan(&t.Secret, &t.Enabled, &t.LastStep)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return t, err
}

// TwoFactorEnabled reports whether the user has confirmed an authenticator app.
func (s *Store) TwoFactorEnabled(userID int64) bool {
	t, err := s.UserTOTP(userID)
	return err == nil && t.Enabled
}

// SetPendingTOTP stores a new, not yet confirmed secret (replacing a previous pending one).
func (s *Store) SetPendingTOTP(userID int64, secret string) error {
	_, err := s.db.Exec(`INSERT INTO user_totp (user_id, secret, enabled, last_step, created_at) VALUES (?, ?, 0, 0, ?)
		ON CONFLICT(user_id) DO UPDATE SET secret = excluded.secret, enabled = 0, last_step = 0, created_at = excluded.created_at
		WHERE user_totp.enabled = 0`, userID, secret, now())
	return err
}

// EnableTOTP confirms the pending secret and stores fresh recovery codes.
func (s *Store) EnableTOTP(userID, step int64, codeHashes []string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.Exec(`UPDATE user_totp SET enabled = 1, last_step = ? WHERE user_id = ? AND enabled = 0`, step, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return ErrNotFound
	}
	if err := replaceRecoveryCodes(tx, userID, codeHashes); err != nil {
		return err
	}
	return tx.Commit()
}

// UseTOTPStep records that the code of a time step was used. It returns false
// when that step (or a later one) was already used, which blocks replays.
func (s *Store) UseTOTPStep(userID, step int64) (bool, error) {
	res, err := s.db.Exec(`UPDATE user_totp SET last_step = ? WHERE user_id = ? AND last_step < ?`, step, userID, step)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n == 1, nil
}

// DeleteTOTP turns two-factor authentication off and drops the recovery codes.
func (s *Store) DeleteTOTP(userID int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM user_totp WHERE user_id = ?`, userID); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM recovery_codes WHERE user_id = ?`, userID); err != nil {
		return err
	}
	return tx.Commit()
}

// ReplaceRecoveryCodes invalidates the old recovery codes and stores new ones.
func (s *Store) ReplaceRecoveryCodes(userID int64, codeHashes []string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := replaceRecoveryCodes(tx, userID, codeHashes); err != nil {
		return err
	}
	return tx.Commit()
}

func replaceRecoveryCodes(tx *sql.Tx, userID int64, codeHashes []string) error {
	if _, err := tx.Exec(`DELETE FROM recovery_codes WHERE user_id = ?`, userID); err != nil {
		return err
	}
	for _, h := range codeHashes {
		if _, err := tx.Exec(`INSERT INTO recovery_codes (user_id, code_hash) VALUES (?, ?)`, userID, h); err != nil {
			return err
		}
	}
	return nil
}

// UseRecoveryCode marks an unused recovery code as used. It returns false if
// the code does not exist or was already used.
func (s *Store) UseRecoveryCode(userID int64, codeHash string) (bool, error) {
	res, err := s.db.Exec(`UPDATE recovery_codes SET used_at = ? WHERE user_id = ? AND code_hash = ? AND used_at = 0`, now(), userID, codeHash)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n == 1, nil
}

// RecoveryCodesLeft counts the unused recovery codes of a user.
func (s *Store) RecoveryCodesLeft(userID int64) int {
	var n int
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM recovery_codes WHERE user_id = ? AND used_at = 0`, userID).Scan(&n)
	return n
}

// ---------- personal API tokens ----------

// APIToken is a personal access token. Only its hash is stored.
type APIToken struct {
	ID         int64  `json:"id"`
	UserID     int64  `json:"-"`
	Name       string `json:"name"`
	TokenHash  string `json:"-"`
	Hint       string `json:"hint"`
	Scope      string `json:"scope"`
	CreatedAt  int64  `json:"created_at"`
	LastUsedAt int64  `json:"last_used_at"`
	ExpiresAt  int64  `json:"expires_at"`
}

const tokenCols = `id, user_id, name, token_hash, hint, scope, created_at, last_used_at, expires_at`

func scanToken(sc interface{ Scan(...any) error }) (*APIToken, error) {
	t := &APIToken{}
	err := sc.Scan(&t.ID, &t.UserID, &t.Name, &t.TokenHash, &t.Hint, &t.Scope, &t.CreatedAt, &t.LastUsedAt, &t.ExpiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return t, err
}

// CreateAPIToken stores a token and fills its id and creation time.
func (s *Store) CreateAPIToken(t *APIToken) error {
	t.CreatedAt = now()
	res, err := s.db.Exec(`INSERT INTO api_tokens (user_id, name, token_hash, hint, scope, created_at, expires_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		t.UserID, t.Name, t.TokenHash, t.Hint, t.Scope, t.CreatedAt, t.ExpiresAt)
	if err != nil {
		return err
	}
	t.ID, _ = res.LastInsertId()
	return nil
}

// ListAPITokens returns a user's tokens, newest first.
func (s *Store) ListAPITokens(userID int64) ([]*APIToken, error) {
	rows, err := s.db.Query(`SELECT `+tokenCols+` FROM api_tokens WHERE user_id = ? ORDER BY id DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*APIToken{}
	for rows.Next() {
		t, err := scanToken(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// APITokenByHash finds a token by the hash of its secret.
func (s *Store) APITokenByHash(hash string) (*APIToken, error) {
	return scanToken(s.db.QueryRow(`SELECT `+tokenCols+` FROM api_tokens WHERE token_hash = ?`, hash))
}

// TouchAPIToken records when a token was last used.
func (s *Store) TouchAPIToken(id, at int64) error {
	_, err := s.db.Exec(`UPDATE api_tokens SET last_used_at = ? WHERE id = ?`, at, id)
	return err
}

// DeleteAPIToken revokes one of the user's tokens.
func (s *Store) DeleteAPIToken(userID, id int64) error {
	res, err := s.db.Exec(`DELETE FROM api_tokens WHERE id = ? AND user_id = ?`, id, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}
