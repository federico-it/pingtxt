package store

import (
	"database/sql"
	"encoding/json"
	"errors"
)

// GetSetting decodes the JSON value stored under key into v.
// It returns ErrNotFound when the key was never written.
func (s *Store) GetSetting(key string, v any) error {
	var raw string
	err := s.db.QueryRow(`SELECT value FROM settings WHERE key = ?`, key).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	return json.Unmarshal([]byte(raw), v)
}

// SetSetting stores v as JSON under key.
func (s *Store) SetSetting(key string, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`INSERT INTO settings (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, string(b))
	return err
}

// ---------- external identities (OAuth / OIDC) ----------

// Identity links an account at an external provider to a local user.
type Identity struct {
	Provider  string `json:"provider"`
	Subject   string `json:"-"`
	UserID    int64  `json:"-"`
	Email     string `json:"email"`
	CreatedAt int64  `json:"created_at"`
}

// IdentityUser returns the user linked to provider/subject.
func (s *Store) IdentityUser(provider, subject string) (*User, error) {
	return s.scanUser(s.db.QueryRow(`SELECT u.id, u.email, u.password_hash, u.created_at FROM identities i JOIN users u ON u.id = i.user_id WHERE i.provider = ? AND i.subject = ?`, provider, subject))
}

// LinkIdentity links provider/subject to a user, replacing any previous link of that external account.
func (s *Store) LinkIdentity(provider, subject string, userID int64, email string) error {
	_, err := s.db.Exec(`INSERT INTO identities (provider, subject, user_id, email, created_at) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(provider, subject) DO UPDATE SET user_id = excluded.user_id, email = excluded.email`, provider, subject, userID, email, now())
	return err
}

// UserIdentities lists the external accounts linked to a user.
func (s *Store) UserIdentities(userID int64) ([]*Identity, error) {
	rows, err := s.db.Query(`SELECT provider, subject, user_id, email, created_at FROM identities WHERE user_id = ? ORDER BY provider`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Identity{}
	for rows.Next() {
		i := &Identity{}
		if err := rows.Scan(&i.Provider, &i.Subject, &i.UserID, &i.Email, &i.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, i)
	}
	return out, rows.Err()
}

// UnlinkIdentity removes a user's link to a provider.
func (s *Store) UnlinkIdentity(userID int64, provider string) error {
	_, err := s.db.Exec(`DELETE FROM identities WHERE user_id = ? AND provider = ?`, userID, provider)
	return err
}

// ---------- users list ----------

// ListUsers returns every user, oldest first.
func (s *Store) ListUsers() ([]*User, error) {
	rows, err := s.db.Query(`SELECT id, email, password_hash, created_at FROM users ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*User{}
	for rows.Next() {
		u := &User{}
		if err := rows.Scan(&u.ID, &u.Email, &u.PasswordHash, &u.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// DeleteUser removes a user with its sessions and linked identities.
func (s *Store) DeleteUser(id int64) error {
	_, err := s.db.Exec(`DELETE FROM users WHERE id = ?`, id)
	return err
}
