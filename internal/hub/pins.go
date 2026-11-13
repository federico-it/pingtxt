package hub

import (
	"net/http"
	"slices"
	"strconv"
	"sync"

	"github.com/federico-it/pingtxt/internal/store"
)

// Each user pins monitors to the top of their monitor list. Pins belong to
// the account, so they follow the user to every browser and to the app;
// read-only users and read-only tokens can change them, since they change
// nothing for anybody else.

// maxPins bounds the list a user can save.
const maxPins = 1000

// MonitorPins is the list of pinned monitors of the user who asks.
type MonitorPins struct {
	// MonitorIDs in pin order, oldest first.
	MonitorIDs []int64 `json:"monitor_ids"`
}

// pinsMu makes the read-modify-write of a user's pins atomic.
var pinsMu sync.Mutex

// userPins returns the user's pins without the monitors deleted since.
func (h *Hub) userPins(userID int64) ([]int64, map[int64]bool, error) {
	ids, err := h.store.UserPins(userID)
	if err != nil {
		return nil, nil, err
	}
	exists, err := h.store.MonitorIDs()
	if err != nil {
		return nil, nil, err
	}
	return cleanPins(ids, exists), exists, nil
}

// cleanPins drops unknown and repeated ids, keeping the order.
func cleanPins(ids []int64, exists map[int64]bool) []int64 {
	out := []int64{}
	seen := map[int64]bool{}
	for _, id := range ids {
		if exists[id] && !seen[id] && len(out) < maxPins {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}

func (h *Hub) getPins(w http.ResponseWriter, r *http.Request, u *store.User, _ *store.APIToken) {
	ids, _, err := h.userPins(u.ID)
	if err != nil {
		h.internalErr(w, err)
		return
	}
	writeJSON(w, 200, MonitorPins{MonitorIDs: ids})
}

// putPins replaces the list. Ids of monitors that do not exist (anymore)
// are dropped instead of failing, so a stale list from a browser or the app
// still saves the rest.
func (h *Hub) putPins(w http.ResponseWriter, r *http.Request, u *store.User, _ *store.APIToken) {
	pinsMu.Lock()
	defer pinsMu.Unlock()
	cur, exists, err := h.userPins(u.ID)
	if err != nil {
		h.internalErr(w, err)
		return
	}
	var in MonitorPins
	if readJSONOver(r, MonitorPins{MonitorIDs: cur}, &in) != nil {
		writeErr(w, 400, "invalid body")
		return
	}
	h.savePins(w, u, cleanPins(in.MonitorIDs, exists))
}

// pinMonitor adds one monitor at the end of the list (no change if pinned).
func (h *Hub) pinMonitor(w http.ResponseWriter, r *http.Request, u *store.User, _ *store.APIToken) {
	h.changePin(w, r, u, true)
}

// unpinMonitor takes one monitor off the list (no change if not pinned).
func (h *Hub) unpinMonitor(w http.ResponseWriter, r *http.Request, u *store.User, _ *store.APIToken) {
	h.changePin(w, r, u, false)
}

func (h *Hub) changePin(w http.ResponseWriter, r *http.Request, u *store.User, pin bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeErr(w, 400, "invalid monitor id")
		return
	}
	pinsMu.Lock()
	defer pinsMu.Unlock()
	cur, exists, err := h.userPins(u.ID)
	if err != nil {
		h.internalErr(w, err)
		return
	}
	if pin && !exists[id] {
		writeErr(w, 404, "monitor not found")
		return
	}
	has := slices.Contains(cur, id)
	switch {
	case pin && !has:
		if len(cur) >= maxPins {
			writeErr(w, 400, "too many pinned monitors (max "+strconv.Itoa(maxPins)+")")
			return
		}
		cur = append(cur, id)
	case !pin && has:
		cur = slices.DeleteFunc(cur, func(x int64) bool { return x == id })
	}
	h.savePins(w, u, cur)
}

func (h *Hub) savePins(w http.ResponseWriter, u *store.User, ids []int64) {
	if err := h.store.SetUserPins(u.ID, ids); err != nil {
		h.internalErr(w, err)
		return
	}
	writeJSON(w, 200, MonitorPins{MonitorIDs: ids})
}
