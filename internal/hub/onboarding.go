package hub

import (
	"errors"
	"net/http"

	"github.com/federico-it/pingtxt/internal/store"
)

// The first-run checklist on the dashboard: the steps that make a new hub
// useful, each marked done from the current configuration, so it needs no
// bookkeeping. Admins can hide it; it hides itself when every step is done.

const keyOnboarding = "onboarding"

// onboardingSettings is what the hub stores about the checklist.
type onboardingSettings struct {
	Dismissed bool `json:"dismissed"`
}

// onboardingStep is one step of the checklist. Optional steps do not count
// for "all done" (a hub can work fine without them).
type onboardingStep struct {
	ID       string `json:"id"`
	Done     bool   `json:"done"`
	Optional bool   `json:"optional,omitempty"`
}

type onboardingView struct {
	Dismissed bool             `json:"dismissed"`
	Done      int              `json:"done"`
	Total     int              `json:"total"`
	Complete  bool             `json:"complete"` // every required step is done
	Steps     []onboardingStep `json:"steps"`
}

// Step ids, also the keys of the UI texts (onboarding.steps.<id>).
const (
	stepMonitor    = "monitor"
	stepChannel    = "channel"
	stepPublicURL  = "public_url"
	stepAgent      = "agent"
	stepStatusPage = "status_page"
	stepTwoFactor  = "two_factor"
	stepBackups    = "backups"
)

func (h *Hub) onboardingSettings() onboardingSettings {
	var s onboardingSettings
	if err := h.store.GetSetting(keyOnboarding, &s); err != nil && !errors.Is(err, store.ErrNotFound) {
		h.log.Error("load onboarding settings", "err", err)
	}
	return s
}

// onboarding works out the checklist for user u.
func (h *Hub) onboarding(u *store.User) (*onboardingView, error) {
	st := h.store
	monitors, err := st.ListMonitors()
	if err != nil {
		return nil, err
	}
	notifiers, err := st.ListNotifiers()
	if err != nil {
		return nil, err
	}
	agents, err := st.ListAgents()
	if err != nil {
		return nil, err
	}
	pages, err := st.ListStatusPages()
	if err != nil {
		return nil, err
	}
	steps := []onboardingStep{
		{ID: stepMonitor, Done: len(monitors) > 0},
		{ID: stepChannel, Done: len(notifiers) > 0},
		{ID: stepPublicURL, Done: h.generalSettings().PublicURL != ""},
		{ID: stepAgent, Done: len(agents) > 0, Optional: true},
		{ID: stepStatusPage, Done: len(pages) > 0, Optional: true},
		{ID: stepTwoFactor, Done: st.TwoFactorEnabled(u.ID)},
	}
	// Scheduled backups exist only with SQLite.
	if h.FileBackups() {
		steps = append(steps, onboardingStep{ID: stepBackups, Done: h.backupSettings().Enabled})
	}
	v := &onboardingView{Dismissed: h.onboardingSettings().Dismissed, Total: len(steps), Steps: steps, Complete: true}
	for _, s := range steps {
		if s.Done {
			v.Done++
		} else if !s.Optional {
			v.Complete = false
		}
	}
	return v, nil
}

func (h *Hub) getOnboarding(w http.ResponseWriter, r *http.Request, u *store.User) {
	v, err := h.onboarding(u)
	if err != nil {
		h.internalErr(w, err)
		return
	}
	writeJSON(w, 200, v)
}

// putOnboarding hides the checklist ({"dismissed": true}) or shows it again.
func (h *Hub) putOnboarding(w http.ResponseWriter, r *http.Request, u *store.User) {
	cur := h.onboardingSettings()
	var s onboardingSettings
	if readJSONOver(r, &cur, &s) != nil {
		writeErr(w, 400, "invalid body")
		return
	}
	if err := h.store.SetSetting(keyOnboarding, s); err != nil {
		h.internalErr(w, err)
		return
	}
	h.getOnboarding(w, r, u)
}
