package hub

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/federico-it/pingtxt/internal/store"
)

// Update check: every few hours the hub asks GitHub for the latest published
// release and compares it with its own version, so admins see when a new
// version is out. Only release builds (v1.2.3) check: dev and branch builds
// (main-abc1234) never do. The check can be turned off in the settings, and
// then the hub makes no request at all.

// UpdateCheckSettings turn the release check on or off.
type UpdateCheckSettings struct {
	Disabled bool `json:"disabled"`
}

const (
	releaseRepo        = "federico-it/pingtxt"
	releaseCheckEvery  = 12 * time.Hour
	releaseRetryAfter  = time.Hour // after a failed check
	releaseCheckNowGap = time.Minute
)

// latestReleaseURL is a variable so tests can point it at a fake server.
var latestReleaseURL = "https://api.github.com/repos/" + releaseRepo + "/releases/latest"

type releaseInfo struct {
	Version     string `json:"version"`
	Name        string `json:"name"`
	URL         string `json:"url"`
	PublishedAt int64  `json:"published_at"` // unix ms
}

type releaseChecker struct {
	mu        sync.Mutex
	latest    *releaseInfo
	checkedAt time.Time
	err       string
	wake      chan struct{} // settings changed or a check was requested
	run       sync.Mutex    // one request at a time
}

// releaseCheckable reports whether this build has a version to compare.
func releaseCheckable(v string) bool {
	_, ok := semver(v)
	return ok
}

func (h *Hub) updateCheckEnabled() bool {
	return !h.generalSettings().UpdateCheck.Disabled && releaseCheckable(h.cfg.Version)
}

func (h *Hub) wakeReleaseCheck() {
	select {
	case h.releases.wake <- struct{}{}:
	default:
	}
}

func (h *Hub) releaseLoop(ctx context.Context) {
	select { // no request while the hub is starting
	case <-ctx.Done():
		return
	case <-time.After(30 * time.Second):
	}
	for {
		wait := releaseCheckEvery
		if h.updateCheckEnabled() {
			h.releases.mu.Lock()
			since, failed := time.Since(h.releases.checkedAt), h.releases.err != ""
			h.releases.mu.Unlock()
			due := releaseCheckEvery
			if failed {
				due = releaseRetryAfter
			}
			if since >= due {
				h.checkLatestRelease(ctx)
			} else {
				wait = due - since
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-h.releases.wake:
		case <-time.After(wait):
		}
	}
}

// checkLatestRelease asks GitHub for the latest release and keeps the answer.
func (h *Hub) checkLatestRelease(ctx context.Context) {
	h.releases.run.Lock()
	defer h.releases.run.Unlock()
	rel, err := h.fetchLatestRelease(ctx)
	h.releases.mu.Lock()
	defer h.releases.mu.Unlock()
	h.releases.checkedAt = time.Now()
	if err != nil {
		h.releases.err = err.Error()
		h.log.Warn("update check", "err", err)
		return
	}
	h.releases.err = ""
	if h.releases.latest == nil || h.releases.latest.Version != rel.Version {
		if newerThan(rel.Version, h.cfg.Version) {
			h.log.Info("a new pingtxt version is available", "current", h.cfg.Version, "latest", rel.Version, "url", rel.URL)
		}
	}
	h.releases.latest = rel
}

func (h *Hub) fetchLatestRelease(ctx context.Context) (*releaseInfo, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, latestReleaseURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "pingtxt/"+h.cfg.Version)
	res, err := h.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("github: %s", res.Status)
	}
	var body struct {
		TagName     string    `json:"tag_name"`
		Name        string    `json:"name"`
		HTMLURL     string    `json:"html_url"`
		Draft       bool      `json:"draft"`
		Prerelease  bool      `json:"prerelease"`
		PublishedAt time.Time `json:"published_at"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&body); err != nil {
		return nil, fmt.Errorf("github: %w", err)
	}
	if body.Draft || body.Prerelease || !releaseCheckable(body.TagName) {
		return nil, fmt.Errorf("github: unexpected release %q", body.TagName)
	}
	// The link is shown in the UI: only accept this repository's release pages.
	link := "https://github.com/" + releaseRepo + "/releases/tag/" + body.TagName
	if strings.HasPrefix(body.HTMLURL, "https://github.com/"+releaseRepo+"/releases/") {
		link = body.HTMLURL
	}
	rel := &releaseInfo{Version: body.TagName, Name: body.Name, URL: link}
	if !body.PublishedAt.IsZero() {
		rel.PublishedAt = body.PublishedAt.UnixMilli()
	}
	return rel, nil
}

type versionView struct {
	Current         string       `json:"current"`
	CheckEnabled    bool         `json:"check_enabled"`
	CheckDisabled   bool         `json:"check_disabled"` // turned off in the settings
	Latest          *releaseInfo `json:"latest"`
	UpdateAvailable bool         `json:"update_available"`
	CheckedAt       int64        `json:"checked_at"`
	Error           string       `json:"error,omitempty"`
}

func (h *Hub) versionStatus() versionView {
	v := versionView{
		Current:       h.cfg.Version,
		CheckEnabled:  h.updateCheckEnabled(),
		CheckDisabled: h.generalSettings().UpdateCheck.Disabled,
	}
	if !v.CheckEnabled {
		return v
	}
	h.releases.mu.Lock()
	defer h.releases.mu.Unlock()
	if h.releases.latest != nil {
		l := *h.releases.latest
		v.Latest = &l
		v.UpdateAvailable = newerThan(l.Version, h.cfg.Version)
	}
	if !h.releases.checkedAt.IsZero() {
		v.CheckedAt = h.releases.checkedAt.UnixMilli()
	}
	v.Error = h.releases.err
	return v
}

func (h *Hub) getVersion(w http.ResponseWriter, _ *http.Request, _ *store.User) {
	writeJSON(w, 200, h.versionStatus())
}

// checkVersionNow runs the release check right away (at most once a minute).
func (h *Hub) checkVersionNow(w http.ResponseWriter, r *http.Request, _ *store.User) {
	if !h.updateCheckEnabled() {
		writeErr(w, 409, errUpdateCheckOff.Error())
		return
	}
	h.releases.mu.Lock()
	recent := time.Since(h.releases.checkedAt) < releaseCheckNowGap
	h.releases.mu.Unlock()
	if !recent {
		h.checkLatestRelease(r.Context())
	}
	writeJSON(w, 200, h.versionStatus())
}

var errUpdateCheckOff = errors.New("the update check is off (turned off in the settings, or a development build)")
