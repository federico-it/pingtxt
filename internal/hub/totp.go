package hub

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/federico-it/pingtxt/internal/store"
)

// Two-factor authentication with an authenticator app (TOTP, RFC 6238:
// SHA-1, 6 digits, 30 second steps) plus single-use recovery codes.
// It applies to password logins; single sign-on relies on the provider's own MFA.

const (
	totpPeriod        = 30
	totpSkew          = 1 // accept the previous and next step for clock drift
	recoveryCodeCount = 10
	challengeTTL      = 5 * time.Minute
	challengeAttempts = 5
)

var b32 = base32.StdEncoding.WithPadding(base32.NoPadding)

func newTOTPSecret() string {
	b := make([]byte, 20)
	_, _ = rand.Read(b)
	return b32.EncodeToString(b)
}

func totpCode(key []byte, step int64) string {
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], uint64(step))
	m := hmac.New(sha1.New, key)
	m.Write(msg[:])
	sum := m.Sum(nil)
	off := sum[len(sum)-1] & 0x0f
	v := binary.BigEndian.Uint32(sum[off:off+4]) & 0x7fffffff
	return fmt.Sprintf("%06d", v%1_000_000)
}

// totpMatch returns the time step whose code equals code, if any, near now.
func totpMatch(secret, code string, now time.Time) (int64, bool) {
	key, err := b32.DecodeString(strings.ToUpper(secret))
	if err != nil || len(code) != 6 {
		return 0, false
	}
	cur := now.Unix() / totpPeriod
	for s := cur - totpSkew; s <= cur+totpSkew; s++ {
		if subtle.ConstantTimeCompare([]byte(totpCode(key, s)), []byte(code)) == 1 {
			return s, true
		}
	}
	return 0, false
}

func totpURI(secret, account, issuer string) string {
	q := url.Values{}
	q.Set("secret", secret)
	q.Set("issuer", issuer)
	q.Set("algorithm", "SHA1")
	q.Set("digits", "6")
	q.Set("period", fmt.Sprint(totpPeriod))
	return "otpauth://totp/" + url.PathEscape(issuer+":"+account) + "?" + q.Encode()
}

// normalizeCode drops spaces and dashes so "123 456" and "abcde-fghij" work.
func normalizeCode(c string) string {
	return strings.ToLower(strings.NewReplacer(" ", "", "-", "", "\t", "").Replace(strings.TrimSpace(c)))
}

func isDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return s != ""
}

// newRecoveryCodes returns codes to show once ("xxxxx-xxxxx") and their hashes.
func newRecoveryCodes() (codes, hashes []string) {
	for range recoveryCodeCount {
		b := make([]byte, 7)
		_, _ = rand.Read(b)
		c := strings.ToLower(b32.EncodeToString(b))[:10]
		codes = append(codes, c[:5]+"-"+c[5:])
		hashes = append(hashes, HashToken(c))
	}
	return codes, hashes
}

var errBadCode = errors.New("invalid code")

// verifySecondFactor checks an authenticator code or, if allowRecovery, a recovery code.
func (h *Hub) verifySecondFactor(userID int64, code string, allowRecovery bool) error {
	t, err := h.store.UserTOTP(userID)
	if err != nil || !t.Enabled {
		return errors.New("two-factor authentication is not enabled")
	}
	code = normalizeCode(code)
	if isDigits(code) {
		step, ok := totpMatch(t.Secret, code, time.Now())
		if !ok {
			return errBadCode
		}
		if used, err := h.store.UseTOTPStep(userID, step); err != nil {
			return err
		} else if !used {
			return errors.New("code already used, wait for the next one")
		}
		return nil
	}
	if !allowRecovery {
		return errBadCode
	}
	if ok, err := h.store.UseRecoveryCode(userID, HashToken(code)); err != nil {
		return err
	} else if !ok {
		return errBadCode
	}
	h.log.Info("recovery code used", "user", userID)
	return nil
}

// ---------- login challenge ----------

// challenge is a password login waiting for the second factor.
type challenge struct {
	userID   int64
	attempts int
	expires  time.Time
}

type challenges struct {
	mu sync.Mutex
	m  map[string]*challenge
}

func (c *challenges) put(userID int64) string {
	token, _ := NewToken("")
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.m == nil {
		c.m = map[string]*challenge{}
	}
	now := time.Now()
	for k, v := range c.m {
		if now.After(v.expires) {
			delete(c.m, k)
		}
	}
	if len(c.m) >= 1000 {
		for k := range c.m {
			delete(c.m, k)
			break
		}
	}
	c.m[token] = &challenge{userID: userID, expires: now.Add(challengeTTL)}
	return token
}

// get returns the user of a live challenge and counts one attempt.
func (c *challenges) get(token string) (int64, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	ch, ok := c.m[token]
	if !ok || time.Now().After(ch.expires) || ch.attempts >= challengeAttempts {
		delete(c.m, token)
		return 0, false
	}
	ch.attempts++
	return ch.userID, true
}

func (c *challenges) done(token string) {
	c.mu.Lock()
	delete(c.m, token)
	c.mu.Unlock()
}

// loginSecondFactor finishes a password login with an authenticator or recovery code.
func (h *Hub) loginSecondFactor(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Challenge string `json:"challenge"`
		Code      string `json:"code"`
	}
	if readJSON(r, &in) != nil {
		writeErr(w, 400, "invalid body")
		return
	}
	uid, ok := h.challenges.get(in.Challenge)
	if !ok {
		writeErr(w, http.StatusGone, "login expired, enter your password again")
		return
	}
	if h.codeBlocked(w, uid) {
		return
	}
	if err := h.verifySecondFactor(uid, in.Code, true); err != nil {
		if errors.Is(err, errBadCode) {
			h.codeFailed(uid)
		}
		time.Sleep(400 * time.Millisecond)
		writeErr(w, 401, err.Error())
		return
	}
	h.challenges.done(in.Challenge)
	u, err := h.store.UserByID(uid)
	if err != nil {
		writeErr(w, http.StatusGone, "login expired, enter your password again")
		return
	}
	if err := h.startSession(w, r, u); err != nil {
		h.internalErr(w, err)
		return
	}
	writeJSON(w, 200, u)
}

// ---------- account endpoints ----------

func (h *Hub) setupTwoFactor(w http.ResponseWriter, r *http.Request, u *store.User) {
	if h.store.TwoFactorEnabled(u.ID) {
		writeErr(w, 409, "two-factor authentication is already on")
		return
	}
	secret := newTOTPSecret()
	if err := h.store.SetPendingTOTP(u.ID, secret); err != nil {
		h.internalErr(w, err)
		return
	}
	issuer := "pingtxt"
	if pu, err := url.Parse(h.hubURL(r)); err == nil && pu.Hostname() != "" {
		issuer = "pingtxt (" + pu.Hostname() + ")"
	}
	writeJSON(w, 200, map[string]string{"secret": secret, "uri": totpURI(secret, u.Email, issuer)})
}

type codeInput struct {
	Code string `json:"code"`
}

func (h *Hub) enableTwoFactor(w http.ResponseWriter, r *http.Request, u *store.User) {
	var in codeInput
	if readJSON(r, &in) != nil {
		writeErr(w, 400, "invalid body")
		return
	}
	t, err := h.store.UserTOTP(u.ID)
	if err != nil || t.Enabled {
		writeErr(w, 409, "start the setup again")
		return
	}
	step, ok := totpMatch(t.Secret, normalizeCode(in.Code), time.Now())
	if !ok {
		writeErr(w, 400, "invalid code, check the time on your phone")
		return
	}
	codes, hashes := newRecoveryCodes()
	if err := h.store.EnableTOTP(u.ID, step, hashes); err != nil {
		h.internalErr(w, err)
		return
	}
	h.logoutOtherSessions(r, u)
	h.log.Info("two-factor enabled", "user", u.Email)
	writeJSON(w, 200, map[string]any{"recovery_codes": codes})
}

func (h *Hub) disableTwoFactor(w http.ResponseWriter, r *http.Request, u *store.User) {
	var in codeInput
	if readJSON(r, &in) != nil {
		writeErr(w, 400, "invalid body")
		return
	}
	if h.codeBlocked(w, u.ID) {
		return
	}
	if err := h.verifySecondFactor(u.ID, in.Code, true); err != nil {
		if errors.Is(err, errBadCode) {
			h.codeFailed(u.ID)
		}
		writeErr(w, 400, err.Error())
		return
	}
	if err := h.store.DeleteTOTP(u.ID); err != nil {
		h.internalErr(w, err)
		return
	}
	h.log.Info("two-factor disabled", "user", u.Email)
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (h *Hub) regenerateRecoveryCodes(w http.ResponseWriter, r *http.Request, u *store.User) {
	var in codeInput
	if readJSON(r, &in) != nil {
		writeErr(w, 400, "invalid body")
		return
	}
	if h.codeBlocked(w, u.ID) {
		return
	}
	if err := h.verifySecondFactor(u.ID, in.Code, false); err != nil {
		if errors.Is(err, errBadCode) {
			h.codeFailed(u.ID)
		}
		writeErr(w, 400, err.Error())
		return
	}
	codes, hashes := newRecoveryCodes()
	if err := h.store.ReplaceRecoveryCodes(u.ID, hashes); err != nil {
		h.internalErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"recovery_codes": codes})
}

// resetUserTwoFactor lets another user turn 2FA off for someone who lost their phone.
func (h *Hub) resetUserTwoFactor(w http.ResponseWriter, r *http.Request, me *store.User) {
	id, ok := pathID(r)
	if !ok {
		writeErr(w, 400, "invalid id")
		return
	}
	if id == me.ID {
		writeErr(w, 400, "turn off your own two-factor authentication from your account")
		return
	}
	target, err := h.store.UserByID(id)
	if err != nil {
		writeErr(w, 404, "user not found")
		return
	}
	if err := h.ResetTwoFactor(target.Email); err != nil {
		h.internalErr(w, err)
		return
	}
	h.log.Info("two-factor reset", "user", target.Email, "by", me.Email)
	writeJSON(w, 200, map[string]bool{"ok": true})
}

// ResetTwoFactor turns 2FA off for a user and logs them out (admin recovery).
func (h *Hub) ResetTwoFactor(email string) error {
	u, err := h.store.UserByEmail(strings.ToLower(strings.TrimSpace(email)))
	if err != nil {
		return fmt.Errorf("user %s: %w", email, err)
	}
	if err := h.store.DeleteTOTP(u.ID); err != nil {
		return err
	}
	return h.store.DeleteUserSessions(u.ID, "")
}

func (h *Hub) logoutOtherSessions(r *http.Request, u *store.User) {
	cur := ""
	if c, err := r.Cookie(sessionCookie); err == nil {
		cur = HashToken(c.Value)
	}
	_ = h.store.DeleteUserSessions(u.ID, cur)
}
