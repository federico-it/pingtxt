package hub

import (
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// Brute force protection for password logins and second factor codes.
//
// Failed attempts are counted per account and per client address over a
// sliding window. Once a key reaches its limit, further attempts are refused
// with 429 until the oldest failure leaves the window, even with the right
// password. Successful logins do not count.
//
// The per account limit holds even when an attacker rotates addresses (or
// spoofs X-Forwarded-For); the per address limit stops one client spraying
// passwords across many accounts.
const (
	loginWindow       = 15 * time.Minute
	loginFailsAccount = 10 // failed passwords per email
	loginFailsAddr    = 30 // failed passwords per client address
	codeFailsUser     = 10 // wrong second factor codes per user, across challenges
	maxLimiterKeys    = 10000
)

type failLimiter struct {
	mu    sync.Mutex
	limit int
	fails map[string][]time.Time
}

func newFailLimiter(limit int) *failLimiter {
	return &failLimiter{limit: limit, fails: map[string][]time.Time{}}
}

// recent drops failures older than the window. Call with l.mu held.
func (l *failLimiter) recent(key string, now time.Time) []time.Time {
	list := l.fails[key]
	i := 0
	for i < len(list) && now.Sub(list[i]) >= loginWindow {
		i++
	}
	list = list[i:]
	if len(list) == 0 {
		delete(l.fails, key)
		return nil
	}
	l.fails[key] = list
	return list
}

// blocked reports whether key reached its limit, and for how long.
func (l *failLimiter) blocked(key string, now time.Time) (time.Duration, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	list := l.recent(key, now)
	if len(list) < l.limit {
		return 0, false
	}
	return loginWindow - now.Sub(list[len(list)-l.limit]), true
}

// fail records a failed attempt.
func (l *failLimiter) fail(key string, now time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.fails) >= maxLimiterKeys {
		// Bound memory: forget the keys whose failures expired, then any key.
		for k := range l.fails {
			l.recent(k, now)
		}
		for k := range l.fails {
			if len(l.fails) < maxLimiterKeys {
				break
			}
			delete(l.fails, k)
		}
	}
	list := l.recent(key, now)
	if len(list) >= l.limit {
		list = list[1:] // keep at most limit entries
	}
	l.fails[key] = append(list, now)
}

// loginLimits groups the limiters of the hub.
type loginLimits struct {
	once    sync.Once
	account *failLimiter
	addr    *failLimiter
	code    *failLimiter
	// Status page passwords, see pagepassword.go.
	pageAddr *failLimiter
	page     *failLimiter
}

func (l *loginLimits) init() {
	l.once.Do(func() {
		l.account = newFailLimiter(loginFailsAccount)
		l.addr = newFailLimiter(loginFailsAddr)
		l.code = newFailLimiter(codeFailsUser)
		l.pageAddr = newFailLimiter(pagePassFailAddr)
		l.page = newFailLimiter(pagePassFailPage)
	})
}

// limitAddr is the client address used for rate limits. It is the direct
// peer, or the first X-Forwarded-For entry when the hub runs behind a proxy.
// IPv6 clients count per /64: one host usually holds a whole /64 and could
// otherwise rotate addresses to get around the limit.
func limitAddr(r *http.Request) string {
	ip := net.ParseIP(clientIP(r))
	if ip == nil {
		return r.RemoteAddr
	}
	if ip.To4() != nil {
		return ip.To4().String()
	}
	return (&net.IPNet{IP: ip.Mask(net.CIDRMask(64, 128)), Mask: net.CIDRMask(64, 128)}).String()
}

func tooManyAttempts(w http.ResponseWriter, wait time.Duration) {
	secs := max(int(wait.Seconds()+0.5), 1)
	w.Header().Set("Retry-After", strconv.Itoa(secs))
	writeErr(w, http.StatusTooManyRequests, "too many failed attempts, try again in "+waitText(wait))
}

func waitText(d time.Duration) string {
	if m := int(d.Round(time.Minute).Minutes()); m > 1 {
		return strconv.Itoa(m) + " minutes"
	}
	return "a minute"
}

// passwordBlocked checks the limits before a password is verified.
func (h *Hub) passwordBlocked(w http.ResponseWriter, r *http.Request, email string) bool {
	h.limits.init()
	now := time.Now()
	wait, blocked := h.limits.account.blocked(email, now)
	if w2, b := h.limits.addr.blocked(limitAddr(r), now); b {
		blocked, wait = true, max(wait, w2)
	}
	if blocked {
		h.log.Warn("login rate limited", "email", email, "addr", limitAddr(r))
		tooManyAttempts(w, wait)
	}
	return blocked
}

func (h *Hub) passwordFailed(r *http.Request, email string) {
	h.limits.init()
	now := time.Now()
	h.limits.account.fail(email, now)
	h.limits.addr.fail(limitAddr(r), now)
}

// codeBlocked checks the second factor limit of a user.
func (h *Hub) codeBlocked(w http.ResponseWriter, userID int64) bool {
	h.limits.init()
	wait, blocked := h.limits.code.blocked(strconv.FormatInt(userID, 10), time.Now())
	if blocked {
		h.log.Warn("second factor rate limited", "user", userID)
		tooManyAttempts(w, wait)
	}
	return blocked
}

func (h *Hub) codeFailed(userID int64) {
	h.limits.init()
	h.limits.code.fail(strconv.FormatInt(userID, 10), time.Now())
}
