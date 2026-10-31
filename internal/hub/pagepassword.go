package hub

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/federico-it/pingtxt/internal/store"
	"golang.org/x/crypto/bcrypt"
)

// Password protected status pages.
//
// A page with a password shows visitors a form instead of its content. The
// right password sets a cookie that opens the page (and its badges and
// subscription form) for pagePassTTL. The cookie is signed with the page's
// password hash, so changing or removing the password logs every visitor out.
// Logged-in users of the hub open protected pages without the password.

const (
	pagePassTTL      = 30 * 24 * time.Hour
	pagePassMin      = 6
	pagePassMax      = 72  // bcrypt reads at most 72 bytes
	pagePassFailAddr = 10  // wrong passwords per page and client address
	pagePassFailPage = 100 // wrong passwords per page, from any address
)

func pageCookieName(id int64) string { return "pingtxt_page_" + strconv.FormatInt(id, 10) }

func pageCookieMAC(id int64, hash string, exp int64) string {
	m := hmac.New(sha256.New, []byte("pingtxt status page\x00"+hash))
	m.Write([]byte(strconv.FormatInt(id, 10) + ":" + strconv.FormatInt(exp, 10)))
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}

// pageCookieOK reports whether the request carries a valid cookie for the page.
func pageCookieOK(r *http.Request, id int64, hash string) bool {
	c, err := r.Cookie(pageCookieName(id))
	if err != nil {
		return false
	}
	expS, mac, ok := strings.Cut(c.Value, ".")
	if !ok {
		return false
	}
	exp, err := strconv.ParseInt(expS, 10, 64)
	if err != nil || exp < time.Now().Unix() {
		return false
	}
	return hmac.Equal([]byte(mac), []byte(pageCookieMAC(id, hash, exp)))
}

// pageOpen reports whether the request may see a page: it has no password,
// the visitor unlocked it, or a hub user is logged in (not on custom domains,
// which never see the hub's session cookie anyway).
func (h *Hub) pageOpen(r *http.Request, page *publicPage, byDomain bool) bool {
	if page.passHash == "" || pageCookieOK(r, page.id, page.passHash) {
		return true
	}
	return !byDomain && h.currentUser(r) != nil
}

// passwordRequired answers a request for a protected page without access.
// 403, not 401: the UI sends 401 answers to the hub's login page.
func passwordRequired(w http.ResponseWriter, page *publicPage) {
	writeJSON(w, http.StatusForbidden, map[string]any{
		"error": "this status page needs a password", "password_required": true, "title": page.Title,
	})
}

// hashPagePassword checks and hashes a new page password.
func hashPagePassword(pw string) (string, error) {
	if utf8.RuneCountInString(pw) < pagePassMin {
		return "", errors.New("the page password needs at least " + strconv.Itoa(pagePassMin) + " characters")
	}
	if len(pw) > pagePassMax {
		return "", errors.New("the page password can be at most " + strconv.Itoa(pagePassMax) + " bytes")
	}
	b, err := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.DefaultCost)
	return string(b), err
}

// applyPagePassword sets p.PasswordHash from the request: a new password,
// the current hash (old) kept, or none.
func applyPagePassword(p *store.StatusPage, old string) error {
	pw := p.Password
	p.Password = ""
	switch {
	case pw != "":
		hash, err := hashPagePassword(pw)
		if err != nil {
			return err
		}
		p.PasswordHash = hash
	case p.Protected && old != "":
		p.PasswordHash = old
	case p.Protected:
		return errors.New("enter a password for the page")
	default:
		p.PasswordHash = ""
	}
	p.Protected = p.PasswordHash != ""
	return nil
}

func (h *Hub) publicUnlock(w http.ResponseWriter, r *http.Request) {
	h.serveUnlock(w, r, strings.ToLower(r.PathValue("slug")))
}

// serveUnlock checks the password of a page and sets its cookie.
func (h *Hub) serveUnlock(w http.ResponseWriter, r *http.Request, slug string) {
	page, err := h.cachedPublicPage(slug)
	if errors.Is(err, store.ErrNotFound) || (err == nil && !page.Published) {
		writeErr(w, 404, "status page not found")
		return
	} else if err != nil {
		h.internalErr(w, err)
		return
	}
	if page.passHash == "" {
		writeJSON(w, 200, map[string]bool{"ok": true})
		return
	}
	var in struct {
		Password string `json:"password"`
	}
	if readJSON(r, &in) != nil {
		writeErr(w, 400, "invalid body")
		return
	}
	h.limits.init()
	now := time.Now()
	pageKey := strconv.FormatInt(page.id, 10)
	addrKey := pageKey + "\x00" + limitAddr(r)
	wait, blocked := h.limits.pageAddr.blocked(addrKey, now)
	if w2, b := h.limits.page.blocked(pageKey, now); b {
		blocked, wait = true, max(wait, w2)
	}
	if blocked {
		h.log.Warn("status page password rate limited", "page", page.Slug, "addr", limitAddr(r))
		tooManyAttempts(w, wait)
		return
	}
	if len(in.Password) > pagePassMax || bcrypt.CompareHashAndPassword([]byte(page.passHash), []byte(in.Password)) != nil {
		h.limits.pageAddr.fail(addrKey, now)
		h.limits.page.fail(pageKey, now)
		writeErr(w, http.StatusForbidden, "wrong password")
		return
	}
	exp := now.Add(pagePassTTL)
	http.SetCookie(w, &http.Cookie{
		Name: pageCookieName(page.id), Value: strconv.FormatInt(exp.Unix(), 10) + "." + pageCookieMAC(page.id, page.passHash, exp.Unix()),
		Path: "/", Expires: exp, HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: isHTTPS(r),
	})
	writeJSON(w, 200, map[string]bool{"ok": true})
}
