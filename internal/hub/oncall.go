package hub

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/federico-it/pingtxt/internal/store"
)

// On-call schedules. A schedule rotates its members: each one is on call
// for a shift (some hours, days or weeks), then the next one takes over,
// from the schedule's start on. Daily and weekly shifts change at the same
// local time of the schedule's timezone all year. Overrides put someone
// else on call for a while (a swap, a sick day); the latest override wins
// where they overlap.
//
// The "oncall" notification channel alerts whoever is on call when the
// event happens: on their phones (app), by email (through an email
// channel) and by SMS or call (through a Twilio channel, to the phone
// number of their account). Escalation works as for any channel: a second
// on-call channel with "send after" reaches a backup schedule.

const (
	maxScheduleMembers = 50
	maxShiftsRange     = 92 * 24 * time.Hour // longest range of GET .../shifts
	maxOverrideLen     = 366 * 24 * time.Hour
)

// shiftMax is the longest shift of each unit.
var shiftMax = map[string]int{store.ShiftHours: 24 * 31, store.ShiftDays: 92, store.ShiftWeeks: 12}

// rotation is a schedule ready to answer who is on call.
type rotation struct {
	sc        *store.Schedule
	loc       *time.Location
	members   []*store.User // in order, deleted users left out
	overrides []*store.Override
	users     map[int64]*store.User
}

// loadRotation reads a schedule, its users and the overrides that end after since.
func loadRotation(st *store.Store, id int64, since time.Time) (*rotation, error) {
	sc, err := st.Schedule(id)
	if err != nil {
		return nil, err
	}
	users, err := st.ListUsers()
	if err != nil {
		return nil, err
	}
	ov, err := st.Overrides(id, since.UnixMilli())
	if err != nil {
		return nil, err
	}
	return newRotation(sc, users, ov), nil
}

func newRotation(sc *store.Schedule, users []*store.User, ov []*store.Override) *rotation {
	r := &rotation{sc: sc, loc: scheduleLocation(sc.Timezone), users: map[int64]*store.User{}}
	for _, u := range users {
		r.users[u.ID] = u
	}
	for _, id := range sc.Members {
		if u := r.users[id]; u != nil {
			r.members = append(r.members, u)
		}
	}
	for _, o := range ov {
		if r.users[o.UserID] != nil {
			r.overrides = append(r.overrides, o)
		}
	}
	return r
}

func scheduleLocation(tz string) *time.Location {
	if loc, err := time.LoadLocation(tz); err == nil && tz != "" {
		return loc
	}
	return time.UTC
}

// shift returns the number of the rotation shift running at t (0 = the
// first one) and when it starts and ends. k is -1 before the schedule
// starts.
func (r *rotation) shift(t time.Time) (k int, start, end time.Time) {
	anchor := time.UnixMilli(r.sc.StartAt).In(r.loc)
	if t.Before(anchor) {
		return -1, time.Time{}, anchor
	}
	n := max(r.sc.ShiftLen, 1)
	if r.sc.ShiftUnit == store.ShiftHours {
		d := time.Duration(n) * time.Hour
		k = int(t.Sub(anchor) / d)
		start = anchor.Add(time.Duration(k) * d)
		return k, start, start.Add(d)
	}
	days := n
	if r.sc.ShiftUnit == store.ShiftWeeks {
		days *= 7
	}
	at := func(k int) time.Time { return anchor.AddDate(0, 0, k*days) }
	k = int(t.Sub(anchor).Hours()/24) / days
	for k > 0 && at(k).After(t) {
		k--
	}
	for !at(k + 1).After(t) {
		k++
	}
	return k, at(k), at(k + 1)
}

// at returns who is on call at t (nil = nobody) and whether an override
// put them there.
func (r *rotation) at(t time.Time) (*store.User, bool) {
	ms := t.UnixMilli()
	var best *store.Override
	for _, o := range r.overrides {
		if o.StartAt <= ms && ms < o.EndAt && (best == nil || o.ID > best.ID) {
			best = o
		}
	}
	if best != nil {
		return r.users[best.UserID], true
	}
	if len(r.members) == 0 {
		return nil, false
	}
	k, _, _ := r.shift(t)
	if k < 0 {
		return nil, false
	}
	return r.members[k%len(r.members)], false
}

// onCallShift is a stretch of time with the same person on call.
type onCallShift struct {
	UserID   int64  `json:"user_id"` // 0 = nobody
	Email    string `json:"email"`
	Start    int64  `json:"start"` // unix ms
	End      int64  `json:"end"`
	Override bool   `json:"override"`
}

// shifts returns who is on call from from to to, in order: rotation shifts
// and overrides, merged where the same person goes on.
func (r *rotation) shifts(from, to time.Time) []onCallShift {
	bounds := []int64{from.UnixMilli(), to.UnixMilli()}
	if len(r.members) > 0 {
		_, _, end := r.shift(from)
		for end.Before(to) {
			bounds = append(bounds, end.UnixMilli())
			_, _, end = r.shift(end)
		}
	}
	for _, o := range r.overrides {
		for _, b := range []int64{o.StartAt, o.EndAt} {
			if b > from.UnixMilli() && b < to.UnixMilli() {
				bounds = append(bounds, b)
			}
		}
	}
	slices.Sort(bounds)
	bounds = slices.Compact(bounds)
	out := []onCallShift{}
	for i := 0; i+1 < len(bounds); i++ {
		u, ov := r.at(time.UnixMilli(bounds[i]))
		s := onCallShift{Start: bounds[i], End: bounds[i+1], Override: ov}
		if u != nil {
			s.UserID, s.Email = u.ID, u.Email
		}
		if n := len(out); n > 0 && out[n-1].UserID == s.UserID && out[n-1].Override == s.Override {
			out[n-1].End = s.End
			continue
		}
		out = append(out, s)
	}
	return out
}

// OnCallNow returns who is on call for a schedule at t, or an error saying
// why nobody is.
func OnCallNow(st *store.Store, scheduleID int64, t time.Time) (*store.User, error) {
	r, err := loadRotation(st, scheduleID, t)
	if errors.Is(err, store.ErrNotFound) {
		return nil, setupError("the on-call schedule of this channel no longer exists")
	} else if err != nil {
		return nil, err
	}
	u, _ := r.at(t)
	if u == nil {
		return nil, setupError(fmt.Sprintf("nobody is on call in %q right now", r.sc.Name))
	}
	return u, nil
}

// ---------- the oncall channel ----------

// validateOnCallConfig checks the settings of an oncall channel, without
// looking them up (see checkOnCallRefs).
func validateOnCallConfig(c map[string]string) error {
	for k, v := range c {
		c[k] = strings.TrimSpace(v)
	}
	if id, err := strconv.ParseInt(c["schedule_id"], 10, 64); err != nil || id <= 0 {
		return errors.New("an on-call schedule is required")
	}
	if c["app"] != "off" {
		c["app"] = "on"
	}
	for _, k := range []string{"email_channel", "twilio_channel"} {
		if c[k] == "" || c[k] == "0" {
			delete(c, k)
			continue
		}
		if id, err := strconv.ParseInt(c[k], 10, 64); err != nil || id <= 0 {
			return fmt.Errorf("invalid %s", strings.ReplaceAll(k, "_", " "))
		}
	}
	switch c["twilio_mode"] {
	case "", "sms":
		c["twilio_mode"] = "sms"
	case "call", "both":
	default:
		return errors.New("twilio mode must be sms, call or both")
	}
	if c["twilio_channel"] == "" {
		delete(c, "twilio_mode")
	}
	if c["app"] == "off" && c["email_channel"] == "" && c["twilio_channel"] == "" {
		return errors.New("pick at least one way to reach whoever is on call: app, email or Twilio")
	}
	return nil
}

// checkOnCallRefs checks that the schedule and the channels an oncall
// channel uses exist and have the right type.
func (h *Hub) checkOnCallRefs(n *store.Notifier) error {
	if n.Type != "oncall" {
		return nil
	}
	id, _ := strconv.ParseInt(n.Config["schedule_id"], 10, 64)
	if _, err := h.store.Schedule(id); err != nil {
		return errors.New("on-call schedule not found")
	}
	for k, typ := range map[string]string{"email_channel": "email", "twilio_channel": "twilio"} {
		if n.Config[k] == "" {
			continue
		}
		cid, _ := strconv.ParseInt(n.Config[k], 10, 64)
		nt, err := h.store.Notifier(cid)
		if err != nil || nt.Type != typ {
			return fmt.Errorf("%s channel not found", typ)
		}
		if typ == "twilio" && n.Config["twilio_mode"] != "sms" && !phoneRe.MatchString(nt.Config["from"]) {
			return errors.New("calls need a Twilio channel whose from is a phone number, not a messaging service")
		}
	}
	return nil
}

// onCallUsers lists the oncall channels that use a schedule or a channel
// (key "schedule_id", "email_channel" or "twilio_channel"), by name.
func (h *Hub) onCallUsers(key string, id int64) ([]string, error) {
	list, err := h.store.ListNotifiers()
	if err != nil {
		return nil, err
	}
	var names []string
	want := strconv.FormatInt(id, 10)
	for _, n := range list {
		if n.Type == "oncall" && n.Config[key] == want {
			names = append(names, n.Name)
		}
	}
	return names, nil
}

// channelInUse refuses to delete (or change the type of) an email or
// Twilio channel that an oncall channel sends through.
func (h *Hub) channelInUse(id int64) error {
	for _, k := range []string{"email_channel", "twilio_channel"} {
		names, err := h.onCallUsers(k, id)
		if err != nil {
			return err
		}
		if len(names) > 0 {
			return errors.New("the on-call channels " + strings.Join(names, ", ") + " send through this channel: change or delete them first")
		}
	}
	return nil
}

// sendOnCall delivers an event to whoever is on call in the channel's
// schedule.
func (n *Notifier) sendOnCall(ctx context.Context, nt *store.Notifier, e Event) error {
	c := nt.Config
	id, _ := strconv.ParseInt(c["schedule_id"], 10, 64)
	u, err := OnCallNow(n.store, id, time.Now())
	if err != nil {
		return err
	}
	var errs []error
	tried := 0
	if c["app"] != "off" {
		tried++
		if err := n.sendApp(ctx, map[string]string{"users": u.Email}, e); err != nil {
			errs = append(errs, fmt.Errorf("app: %w", err))
		}
	}
	if c["email_channel"] != "" {
		tried++
		if err := n.viaChannel(ctx, c["email_channel"], "email", func(cfg map[string]string) error {
			cfg["to"] = u.Email
			return n.sendEmail(ctx, cfg, e)
		}); err != nil {
			errs = append(errs, fmt.Errorf("email: %w", err))
		}
	}
	if c["twilio_channel"] != "" {
		tried++
		err := n.viaChannel(ctx, c["twilio_channel"], "twilio", func(cfg map[string]string) error {
			if u.Phone == "" {
				return setupError(u.Email + " has no phone number: add it in Settings, Account")
			}
			cfg["to"], cfg["mode"] = u.Phone, c["twilio_mode"]
			return n.sendTwilio(ctx, cfg, e, nt.ID)
		})
		if err != nil {
			errs = append(errs, fmt.Errorf("twilio: %w", err))
		}
	}
	if len(errs) == 0 {
		return nil
	}
	err = errors.Join(errs...)
	// A setup problem on every way is a setup problem (409 on tests).
	var se setupError
	if len(errs) == tried && errors.As(err, &se) {
		return setupError(fmt.Sprintf("%s is on call: %v", u.Email, err))
	}
	return fmt.Errorf("%s is on call: %w", u.Email, err)
}

// viaChannel runs send with a copy of the config of another channel.
func (n *Notifier) viaChannel(ctx context.Context, idStr, typ string, send func(map[string]string) error) error {
	id, _ := strconv.ParseInt(idStr, 10, 64)
	nt, err := n.store.Notifier(id)
	if err != nil || nt.Type != typ {
		return setupError("the " + typ + " channel of this on-call channel no longer exists")
	}
	cfg := make(map[string]string, len(nt.Config))
	for k, v := range nt.Config {
		cfg[k] = v
	}
	return send(cfg)
}

// ---------- API ----------

// scheduleView is a schedule with its upcoming overrides and who is on call.
type scheduleView struct {
	*store.Schedule
	Overrides []*store.Override `json:"overrides"`
	// Current is who is on call now (user_id 0 = nobody) until when;
	// Next who comes after.
	Current *onCallShift `json:"current"`
	Next    *onCallShift `json:"next"`
	// Channels are the oncall channels that alert this schedule.
	Channels []string `json:"channels"`
	// Users are the members and the users of the overrides, for their
	// emails (read-only users cannot list users).
	Users []scheduleUser `json:"users"`
}

type scheduleUser struct {
	ID    int64  `json:"id"`
	Email string `json:"email"`
	// HasPhone tells whether SMS and calls can reach them.
	HasPhone bool `json:"has_phone"`
}

func (h *Hub) scheduleView(sc *store.Schedule, users []*store.User, now time.Time) (scheduleView, error) {
	ov, err := h.store.Overrides(sc.ID, now.UnixMilli())
	if err != nil {
		return scheduleView{}, err
	}
	v := scheduleView{Schedule: sc, Overrides: ov}
	r := newRotation(sc, users, ov)
	list := r.shifts(now, now.Add(maxShiftsRange))
	if len(list) > 0 {
		v.Current = &list[0]
	}
	if len(list) > 1 {
		v.Next = &list[1]
	}
	if v.Channels, err = h.onCallUsers("schedule_id", sc.ID); err != nil {
		return v, err
	}
	if v.Channels == nil {
		v.Channels = []string{}
	}
	v.Users = []scheduleUser{}
	seen := map[int64]bool{}
	ids := slices.Clone(sc.Members)
	for _, o := range ov {
		ids = append(ids, o.UserID)
	}
	for _, id := range ids {
		if u := r.users[id]; u != nil && !seen[id] {
			seen[id] = true
			v.Users = append(v.Users, scheduleUser{ID: u.ID, Email: u.Email, HasPhone: u.Phone != ""})
		}
	}
	return v, nil
}

func (h *Hub) listSchedules(w http.ResponseWriter, r *http.Request, _ *store.User) {
	list, err := h.store.ListSchedules()
	if err != nil {
		h.internalErr(w, err)
		return
	}
	users, err := h.store.ListUsers()
	if err != nil {
		h.internalErr(w, err)
		return
	}
	now := time.Now()
	out := make([]scheduleView, 0, len(list))
	for _, sc := range list {
		v, err := h.scheduleView(sc, users, now)
		if err != nil {
			h.internalErr(w, err)
			return
		}
		out = append(out, v)
	}
	writeJSON(w, 200, out)
}

func (h *Hub) getSchedule(w http.ResponseWriter, r *http.Request, _ *store.User) {
	sc, ok := h.pathSchedule(w, r)
	if !ok {
		return
	}
	h.writeSchedule(w, sc, 200)
}

func (h *Hub) writeSchedule(w http.ResponseWriter, sc *store.Schedule, code int) {
	users, err := h.store.ListUsers()
	if err != nil {
		h.internalErr(w, err)
		return
	}
	v, err := h.scheduleView(sc, users, time.Now())
	if err != nil {
		h.internalErr(w, err)
		return
	}
	writeJSON(w, code, v)
}

func (h *Hub) pathSchedule(w http.ResponseWriter, r *http.Request) (*store.Schedule, bool) {
	id, ok := pathID(r)
	if !ok {
		writeErr(w, 400, "invalid id")
		return nil, false
	}
	sc, err := h.store.Schedule(id)
	if errors.Is(err, store.ErrNotFound) {
		writeErr(w, 404, "schedule not found")
		return nil, false
	} else if err != nil {
		h.internalErr(w, err)
		return nil, false
	}
	return sc, true
}

// validateSchedule checks and cleans a schedule.
func (h *Hub) validateSchedule(sc *store.Schedule) error {
	sc.Name = strings.TrimSpace(sc.Name)
	sc.Description = strings.TrimSpace(sc.Description)
	if sc.Name == "" || len(sc.Name) > 100 {
		return errors.New("name is required (at most 100 characters)")
	}
	if len(sc.Description) > 1000 {
		return errors.New("description is too long (at most 1000 characters)")
	}
	if sc.Timezone == "" {
		sc.Timezone = "UTC"
	}
	if _, err := time.LoadLocation(sc.Timezone); err != nil {
		return fmt.Errorf("unknown timezone %q", sc.Timezone)
	}
	if sc.ShiftUnit == "" {
		sc.ShiftUnit = store.ShiftWeeks
	}
	limit, ok := shiftMax[sc.ShiftUnit]
	if !ok {
		return errors.New("shift unit must be hours, days or weeks")
	}
	if sc.ShiftLen == 0 {
		sc.ShiftLen = 1
	}
	if sc.ShiftLen < 1 || sc.ShiftLen > limit {
		return fmt.Errorf("a shift lasts 1 to %d %s", limit, sc.ShiftUnit)
	}
	if sc.StartAt <= 0 {
		return errors.New("start_at is required")
	}
	if len(sc.Members) == 0 {
		return errors.New("add at least one member")
	}
	if len(sc.Members) > maxScheduleMembers {
		return fmt.Errorf("at most %d members", maxScheduleMembers)
	}
	for _, id := range sc.Members {
		if _, err := h.store.UserByID(id); err != nil {
			return fmt.Errorf("user %d not found", id)
		}
	}
	return nil
}

func (h *Hub) createSchedule(w http.ResponseWriter, r *http.Request, _ *store.User) {
	sc := &store.Schedule{Timezone: "UTC", ShiftLen: 1, ShiftUnit: store.ShiftWeeks, StartAt: time.Now().UnixMilli()}
	if readJSONOver(r, sc, sc) != nil {
		writeErr(w, 400, "invalid body")
		return
	}
	sc.ID = 0
	if err := h.validateSchedule(sc); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	if err := h.store.CreateSchedule(sc); err != nil {
		h.internalErr(w, err)
		return
	}
	h.writeSchedule(w, sc, 201)
}

func (h *Hub) updateSchedule(w http.ResponseWriter, r *http.Request, _ *store.User) {
	sc, ok := h.pathSchedule(w, r)
	if !ok {
		return
	}
	id, created := sc.ID, sc.CreatedAt
	if readJSONOver(r, sc, sc) != nil {
		writeErr(w, 400, "invalid body")
		return
	}
	sc.ID, sc.CreatedAt = id, created
	if err := h.validateSchedule(sc); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	if err := h.store.UpdateSchedule(sc); err != nil {
		h.internalErr(w, err)
		return
	}
	h.writeSchedule(w, sc, 200)
}

func (h *Hub) deleteSchedule(w http.ResponseWriter, r *http.Request, _ *store.User) {
	sc, ok := h.pathSchedule(w, r)
	if !ok {
		return
	}
	names, err := h.onCallUsers("schedule_id", sc.ID)
	if err != nil {
		h.internalErr(w, err)
		return
	}
	if len(names) > 0 {
		writeErr(w, 409, "the schedule is used by the channels "+strings.Join(names, ", ")+": change or delete them first")
		return
	}
	if err := h.store.DeleteSchedule(sc.ID); err != nil {
		h.internalErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

// scheduleShifts lists who is on call between from and to (unix ms,
// default the next 14 days, at most 92 days).
func (h *Hub) scheduleShifts(w http.ResponseWriter, r *http.Request, _ *store.User) {
	sc, ok := h.pathSchedule(w, r)
	if !ok {
		return
	}
	now := time.Now()
	from, to := now, now.Add(14*24*time.Hour)
	if v := r.URL.Query().Get("from"); v != "" {
		ms, err := strconv.ParseInt(v, 10, 64)
		if err != nil || ms <= 0 {
			writeErr(w, 400, "invalid from")
			return
		}
		from = time.UnixMilli(ms)
		to = from.Add(14 * 24 * time.Hour)
	}
	if v := r.URL.Query().Get("to"); v != "" {
		ms, err := strconv.ParseInt(v, 10, 64)
		if err != nil || ms <= 0 {
			writeErr(w, 400, "invalid to")
			return
		}
		to = time.UnixMilli(ms)
	}
	if !to.After(from) || to.Sub(from) > maxShiftsRange {
		writeErr(w, 400, "to must be after from, at most 92 days later")
		return
	}
	users, err := h.store.ListUsers()
	if err != nil {
		h.internalErr(w, err)
		return
	}
	ov, err := h.store.Overrides(sc.ID, from.UnixMilli())
	if err != nil {
		h.internalErr(w, err)
		return
	}
	writeJSON(w, 200, newRotation(sc, users, ov).shifts(from, to))
}

func (h *Hub) createOverride(w http.ResponseWriter, r *http.Request, _ *store.User) {
	sc, ok := h.pathSchedule(w, r)
	if !ok {
		return
	}
	var o store.Override
	if readJSON(r, &o) != nil {
		writeErr(w, 400, "invalid body")
		return
	}
	o.ID, o.ScheduleID = 0, sc.ID
	if _, err := h.store.UserByID(o.UserID); err != nil {
		writeErr(w, 400, "user not found")
		return
	}
	switch {
	case o.StartAt <= 0 || o.EndAt <= o.StartAt:
		writeErr(w, 400, "end_at must be after start_at")
		return
	case o.EndAt <= time.Now().UnixMilli():
		writeErr(w, 400, "the override is already over")
		return
	case time.Duration(o.EndAt-o.StartAt)*time.Millisecond > maxOverrideLen:
		writeErr(w, 400, "an override lasts at most one year")
		return
	}
	if err := h.store.CreateOverride(&o); err != nil {
		h.internalErr(w, err)
		return
	}
	writeJSON(w, 201, o)
}

func (h *Hub) deleteOverride(w http.ResponseWriter, r *http.Request, _ *store.User) {
	sc, ok := h.pathSchedule(w, r)
	if !ok {
		return
	}
	oid, err := strconv.ParseInt(r.PathValue("override"), 10, 64)
	if err != nil {
		writeErr(w, 400, "invalid override id")
		return
	}
	if err := h.store.DeleteOverride(sc.ID, oid); errors.Is(err, store.ErrNotFound) {
		writeErr(w, 404, "override not found")
		return
	} else if err != nil {
		h.internalErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

// ---------- phone number of the account ----------

// putAccountPhone sets the caller's phone number (E.164, "" = none), used
// for SMS and calls while they are on call.
func (h *Hub) putAccountPhone(w http.ResponseWriter, r *http.Request, u *store.User, _ *store.APIToken) {
	var b struct {
		Phone string `json:"phone"`
	}
	if readJSON(r, &b) != nil {
		writeErr(w, 400, "invalid body")
		return
	}
	phone := strings.Map(func(r rune) rune {
		if r == ' ' || r == '-' || r == '(' || r == ')' || r == '.' {
			return -1
		}
		return r
	}, b.Phone)
	if phone != "" && !phoneRe.MatchString(phone) {
		writeErr(w, 400, "the phone number must be in international format, like +391234567890")
		return
	}
	if err := h.store.SetUserPhone(u.ID, phone); err != nil {
		h.internalErr(w, err)
		return
	}
	u.Phone = phone
	writeJSON(w, 200, h.userView(u))
}
