package store

import (
	"database/sql"
	"encoding/json"
	"errors"
)

// OAuthClient is an application registered through OAuth dynamic client
// registration (an MCP client such as Claude). Clients are public: no secret,
// PKCE protects the authorization code.
type OAuthClient struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	RedirectURIs []string `json:"redirect_uris"`
	CreatedAt    int64    `json:"created_at"`
}

// OAuthGrant is the access a user gave a client: one access token and one
// refresh token, both stored hashed and rotated together.
type OAuthGrant struct {
	ID             int64  `json:"id"`
	UserID         int64  `json:"-"`
	ClientID       string `json:"client_id"`
	ClientName     string `json:"client_name"`
	Scope          string `json:"scope"`
	AccessHash     string `json:"-"`
	AccessExpires  int64  `json:"-"`
	RefreshHash    string `json:"-"`
	RefreshExpires int64  `json:"expires_at"`
	CreatedAt      int64  `json:"created_at"`
	LastUsedAt     int64  `json:"last_used_at"`
}

func (s *Store) CreateOAuthClient(c *OAuthClient) error {
	c.CreatedAt = now()
	uris, _ := json.Marshal(c.RedirectURIs)
	_, err := s.db.Exec(`INSERT INTO oauth_clients (id, name, redirect_uris, created_at) VALUES (?, ?, ?, ?)`, c.ID, c.Name, string(uris), c.CreatedAt)
	return err
}

func (s *Store) OAuthClient(id string) (*OAuthClient, error) {
	c := &OAuthClient{}
	var uris string
	err := s.db.QueryRow(`SELECT id, name, redirect_uris, created_at FROM oauth_clients WHERE id = ?`, id).Scan(&c.ID, &c.Name, &uris, &c.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	_ = json.Unmarshal([]byte(uris), &c.RedirectURIs)
	return c, nil
}

// DeleteUnusedOAuthClients removes clients registered before t that no user
// ever authorized: registration is open, so abandoned ones are cleaned up.
func (s *Store) DeleteUnusedOAuthClients(before int64) error {
	_, err := s.db.Exec(`DELETE FROM oauth_clients WHERE created_at < ? AND id NOT IN (SELECT client_id FROM oauth_grants)`, before)
	return err
}

const grantCols = `g.id, g.user_id, g.client_id, c.name, g.scope, g.access_hash, g.access_expires, g.refresh_hash, g.refresh_expires, g.created_at, g.last_used_at`

func scanGrant(sc interface{ Scan(...any) error }) (*OAuthGrant, error) {
	g := &OAuthGrant{}
	err := sc.Scan(&g.ID, &g.UserID, &g.ClientID, &g.ClientName, &g.Scope, &g.AccessHash, &g.AccessExpires, &g.RefreshHash, &g.RefreshExpires, &g.CreatedAt, &g.LastUsedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return g, err
}

func (s *Store) CreateOAuthGrant(g *OAuthGrant) error {
	g.CreatedAt = now()
	id, err := s.db.insert(s.db, `INSERT INTO oauth_grants (user_id, client_id, scope, access_hash, access_expires, refresh_hash, refresh_expires, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		g.UserID, g.ClientID, g.Scope, g.AccessHash, g.AccessExpires, g.RefreshHash, g.RefreshExpires, g.CreatedAt)
	if err != nil {
		return err
	}
	g.ID = id
	return nil
}

func (s *Store) OAuthGrantByAccess(hash string) (*OAuthGrant, error) {
	return scanGrant(s.db.QueryRow(`SELECT `+grantCols+` FROM oauth_grants g JOIN oauth_clients c ON c.id = g.client_id WHERE g.access_hash = ?`, hash))
}

func (s *Store) OAuthGrantByRefresh(hash string) (*OAuthGrant, error) {
	return scanGrant(s.db.QueryRow(`SELECT `+grantCols+` FROM oauth_grants g JOIN oauth_clients c ON c.id = g.client_id WHERE g.refresh_hash = ?`, hash))
}

// RotateOAuthGrant replaces both tokens of a grant, only if the refresh token
// is still the one presented (a reused refresh token loses the race).
func (s *Store) RotateOAuthGrant(id int64, oldRefresh, access string, accessExp int64, refresh string, refreshExp int64) error {
	res, err := s.db.Exec(`UPDATE oauth_grants SET access_hash = ?, access_expires = ?, refresh_hash = ?, refresh_expires = ? WHERE id = ? AND refresh_hash = ?`,
		access, accessExp, refresh, refreshExp, id, oldRefresh)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) TouchOAuthGrant(id, at int64) error {
	_, err := s.db.Exec(`UPDATE oauth_grants SET last_used_at = ? WHERE id = ?`, at, id)
	return err
}

// ListOAuthGrants returns the apps a user connected, newest first.
func (s *Store) ListOAuthGrants(userID int64) ([]*OAuthGrant, error) {
	rows, err := s.db.Query(`SELECT `+grantCols+` FROM oauth_grants g JOIN oauth_clients c ON c.id = g.client_id WHERE g.user_id = ? ORDER BY g.id DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*OAuthGrant{}
	for rows.Next() {
		g, err := scanGrant(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

func (s *Store) DeleteOAuthGrant(userID, id int64) error {
	res, err := s.db.Exec(`DELETE FROM oauth_grants WHERE id = ? AND user_id = ?`, id, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteOAuthGrantByID revokes a grant whatever its user (token revocation endpoint).
func (s *Store) DeleteOAuthGrantByID(id int64) error {
	_, err := s.db.Exec(`DELETE FROM oauth_grants WHERE id = ?`, id)
	return err
}

// DeleteExpiredOAuthGrants removes grants whose refresh token has expired.
func (s *Store) DeleteExpiredOAuthGrants(at int64) error {
	_, err := s.db.Exec(`DELETE FROM oauth_grants WHERE refresh_expires < ?`, at)
	return err
}
