package hub

import (
	"errors"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/federico-it/pingtxt/internal/store"
)

// tagColors are the colors a tag can have ("" = default).
var tagColors = []string{"", "red", "orange", "yellow", "green", "cyan", "blue", "purple", "pink", "gray"}

func cleanIDs(in []int64) []int64 {
	out := []int64{}
	for _, id := range in {
		if id > 0 {
			out = append(out, id)
		}
	}
	slices.Sort(out)
	return slices.Compact(out)
}

// checkGroupsTags fails when a group or tag id does not exist.
func (h *Hub) checkGroupsTags(groupIDs, tagIDs []int64) error {
	for _, id := range groupIDs {
		if _, err := h.store.Group(id); err != nil {
			return errors.New("unknown group")
		}
	}
	for _, id := range tagIDs {
		if _, err := h.store.Tag(id); err != nil {
			return errors.New("unknown tag")
		}
	}
	return nil
}

// labelsChanged refreshes the group and tags of every scheduled monitor after
// a group or tag change, then re-applies maintenance windows (which may
// cover monitors through them) and drops cached status pages.
func (h *Hub) labelsChanged() {
	ms, err := h.store.ListMonitors()
	if err != nil {
		h.log.Error("reload monitors", "err", err)
		return
	}
	h.mu.Lock()
	for _, m := range ms {
		if st, ok := h.monitors[m.ID]; ok {
			st.m.GroupID, st.m.TagIDs = m.GroupID, m.TagIDs
		}
	}
	h.mu.Unlock()
	h.afterMaintChange()
	h.pages.mu.Lock()
	h.pages.cache = nil
	h.pages.badges = nil
	h.pages.mu.Unlock()
}

// ---------- groups API ----------

func validateGroup(g *store.Group) error {
	g.Name = strings.TrimSpace(g.Name)
	g.Description = strings.TrimSpace(g.Description)
	if g.Name == "" {
		return errors.New("name is required")
	}
	if len(g.Name) > 64 || len(g.Description) > 500 {
		return errors.New("name or description is too long")
	}
	return nil
}

func (h *Hub) listGroups(w http.ResponseWriter, r *http.Request, _ *store.User) {
	list, err := h.store.ListGroups()
	if err != nil {
		h.internalErr(w, err)
		return
	}
	writeJSON(w, 200, list)
}

func (h *Hub) saveGroup(w http.ResponseWriter, r *http.Request, _ *store.User) {
	var g store.Group
	if readJSON(r, &g) != nil {
		writeErr(w, 400, "invalid body")
		return
	}
	create := r.Method == http.MethodPost
	if !create {
		id, ok := pathID(r)
		if !ok {
			writeErr(w, 400, "invalid id")
			return
		}
		g.ID = id
	}
	if err := validateGroup(&g); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	var err error
	if create {
		err = h.store.CreateGroup(&g)
	} else {
		err = h.store.UpdateGroup(&g)
	}
	switch {
	case errors.Is(err, store.ErrConflict):
		writeErr(w, 409, "a group with this name already exists")
		return
	case errors.Is(err, store.ErrNotFound):
		writeErr(w, 404, "group not found")
		return
	case err != nil:
		h.internalErr(w, err)
		return
	}
	h.pages.mu.Lock()
	h.pages.cache = nil // group names show on status pages
	h.pages.badges = nil
	h.pages.mu.Unlock()
	got, err := h.store.Group(g.ID)
	if err != nil {
		h.internalErr(w, err)
		return
	}
	code := 200
	if create {
		code = 201
	}
	writeJSON(w, code, got)
}

func (h *Hub) deleteGroup(w http.ResponseWriter, r *http.Request, _ *store.User) {
	id, ok := pathID(r)
	if !ok {
		writeErr(w, 400, "invalid id")
		return
	}
	if err := h.store.DeleteGroup(id); err != nil {
		h.internalErr(w, err)
		return
	}
	h.labelsChanged()
	writeJSON(w, 200, map[string]bool{"ok": true})
}

// setGroupMonitors sets the exact list of monitors in a group. Monitors
// moved in leave their previous group.
func (h *Hub) setGroupMonitors(w http.ResponseWriter, r *http.Request, _ *store.User) {
	id, ok := pathID(r)
	if !ok {
		writeErr(w, 400, "invalid id")
		return
	}
	var body struct {
		MonitorIDs []int64 `json:"monitor_ids"`
	}
	if readJSON(r, &body) != nil {
		writeErr(w, 400, "invalid body")
		return
	}
	if _, err := h.store.Group(id); err != nil {
		writeErr(w, 404, "group not found")
		return
	}
	ids := cleanIDs(body.MonitorIDs)
	for _, mid := range ids {
		if _, err := h.store.Monitor(mid); err != nil {
			writeErr(w, 400, "unknown monitor")
			return
		}
	}
	if err := h.store.SetGroupMembers(id, ids); err != nil {
		h.internalErr(w, err)
		return
	}
	h.labelsChanged()
	got, err := h.store.Group(id)
	if err != nil {
		h.internalErr(w, err)
		return
	}
	writeJSON(w, 200, got)
}

// ---------- tags API ----------

func validateTag(t *store.Tag) error {
	t.Name = strings.TrimSpace(t.Name)
	t.Color = strings.ToLower(strings.TrimSpace(t.Color))
	if t.Name == "" {
		return errors.New("name is required")
	}
	if len(t.Name) > 32 || strings.ContainsAny(t.Name, ",\n\t") {
		return errors.New("tag name must be up to 32 characters, without commas")
	}
	if !slices.Contains(tagColors, t.Color) {
		return errors.New("invalid color")
	}
	return nil
}

func (h *Hub) listTags(w http.ResponseWriter, r *http.Request, _ *store.User) {
	list, err := h.store.ListTags()
	if err != nil {
		h.internalErr(w, err)
		return
	}
	writeJSON(w, 200, list)
}

func (h *Hub) saveTag(w http.ResponseWriter, r *http.Request, _ *store.User) {
	var t store.Tag
	if readJSON(r, &t) != nil {
		writeErr(w, 400, "invalid body")
		return
	}
	create := r.Method == http.MethodPost
	if !create {
		id, ok := pathID(r)
		if !ok {
			writeErr(w, 400, "invalid id")
			return
		}
		t.ID = id
	}
	if err := validateTag(&t); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	var err error
	if create {
		err = h.store.CreateTag(&t)
	} else {
		err = h.store.UpdateTag(&t)
	}
	switch {
	case errors.Is(err, store.ErrConflict):
		writeErr(w, 409, "a tag with this name already exists")
		return
	case errors.Is(err, store.ErrNotFound):
		writeErr(w, 404, "tag not found")
		return
	case err != nil:
		h.internalErr(w, err)
		return
	}
	got, err := h.store.Tag(t.ID)
	if err != nil {
		h.internalErr(w, err)
		return
	}
	code := 200
	if create {
		code = 201
	}
	writeJSON(w, code, got)
}

func (h *Hub) deleteTag(w http.ResponseWriter, r *http.Request, _ *store.User) {
	id, ok := pathID(r)
	if !ok {
		writeErr(w, 400, "invalid id")
		return
	}
	if err := h.store.DeleteTag(id); err != nil {
		h.internalErr(w, err)
		return
	}
	h.labelsChanged()
	writeJSON(w, 200, map[string]bool{"ok": true})
}

// monitorFilter reads the optional ?group= and ?tag= filters of the monitor
// list. group=none selects monitors without a group.
type monitorFilter struct {
	group  int64 // -1 = no group, 0 = any
	tag    int64 // 0 = any
	active bool
}

func parseMonitorFilter(r *http.Request) (monitorFilter, error) {
	var f monitorFilter
	q := r.URL.Query()
	if g := q.Get("group"); g != "" {
		f.active = true
		if g == "none" {
			f.group = -1
		} else if id, err := strconv.ParseInt(g, 10, 64); err == nil && id > 0 {
			f.group = id
		} else {
			return f, errors.New("invalid group filter")
		}
	}
	if t := q.Get("tag"); t != "" {
		f.active = true
		id, err := strconv.ParseInt(t, 10, 64)
		if err != nil || id <= 0 {
			return f, errors.New("invalid tag filter")
		}
		f.tag = id
	}
	return f, nil
}

func (f monitorFilter) match(m *store.Monitor) bool {
	switch {
	case f.group == -1 && m.GroupID != 0:
		return false
	case f.group > 0 && m.GroupID != f.group:
		return false
	case f.tag > 0 && !slices.Contains(m.TagIDs, f.tag):
		return false
	}
	return true
}
