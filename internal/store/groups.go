package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"slices"
	"strings"
)

// Group is a named set of monitors. A monitor belongs to at most one group.
// A group can sit inside another one (ParentID), up to MaxGroupDepth levels.
type Group struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	// ParentID is the group this one is a subgroup of, 0 = top level.
	ParentID  int64 `json:"parent_id"`
	Monitors  int   `json:"monitors"` // number of monitors in the group
	CreatedAt int64 `json:"created_at"`
}

// Tag is a free label; a monitor can have any number of tags.
type Tag struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	Color     string `json:"color"`
	Monitors  int    `json:"monitors"` // number of tagged monitors
	CreatedAt int64  `json:"created_at"`
}

// MaxGroupDepth is how many levels groups can nest: a group, its
// subgroups and theirs.
const MaxGroupDepth = 3

// ---------- groups ----------

func (s *Store) ListGroups() ([]*Group, error) {
	rows, err := s.db.Query(`SELECT g.id, g.name, g.description, g.parent_id, g.created_at, (SELECT COUNT(*) FROM monitors m WHERE m.group_id = g.id)
		FROM monitor_groups g ORDER BY LOWER(g.name)`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Group{}
	for rows.Next() {
		g := &Group{}
		if err := rows.Scan(&g.ID, &g.Name, &g.Description, &g.ParentID, &g.CreatedAt, &g.Monitors); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

func (s *Store) Group(id int64) (*Group, error) {
	g := &Group{}
	err := s.db.QueryRow(`SELECT g.id, g.name, g.description, g.parent_id, g.created_at, (SELECT COUNT(*) FROM monitors m WHERE m.group_id = g.id)
		FROM monitor_groups g WHERE g.id = ?`, id).Scan(&g.ID, &g.Name, &g.Description, &g.ParentID, &g.CreatedAt, &g.Monitors)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return g, err
}

func (s *Store) CreateGroup(g *Group) error {
	g.CreatedAt = now()
	id, err := s.db.insert(s.db, `INSERT INTO monitor_groups (name, description, parent_id, created_at) VALUES (?, ?, ?, ?)`, g.Name, g.Description, g.ParentID, g.CreatedAt)
	if err != nil {
		return uniqueErr(err)
	}
	g.ID = id
	return nil
}

func (s *Store) UpdateGroup(g *Group) error {
	res, err := s.db.Exec(`UPDATE monitor_groups SET name = ?, description = ?, parent_id = ? WHERE id = ?`, g.Name, g.Description, g.ParentID, g.ID)
	if err != nil {
		return uniqueErr(err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// SetGroupParent moves a group under another one (0 = top level). The
// caller checks that parent exists and makes no cycle.
func (s *Store) SetGroupParent(id, parent int64) error {
	_, err := s.db.Exec(`UPDATE monitor_groups SET parent_id = ? WHERE id = ?`, parent, id)
	return err
}

// DeleteGroup removes a group. Its monitors stay, without a group, its
// subgroups move up one level (to the deleted group's parent), and the
// group is dropped from maintenance windows and status pages.
func (s *Store) DeleteGroup(id int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var parent int64
	if err := tx.QueryRow(`SELECT parent_id FROM monitor_groups WHERE id = ?`, id).Scan(&parent); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if _, err := tx.Exec(`UPDATE monitor_groups SET parent_id = ? WHERE parent_id = ?`, parent, id); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM monitor_groups WHERE id = ?`, id); err != nil {
		return err
	}
	if err := dropMaintRef(tx, "group_ids", id); err != nil {
		return err
	}
	return tx.Commit()
}

// SetGroupMembers makes ids the exact list of monitors in a group.
func (s *Store) SetGroupMembers(groupID int64, ids []int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`UPDATE monitors SET group_id = NULL WHERE group_id = ?`, groupID); err != nil {
		return err
	}
	for _, id := range ids {
		if _, err := tx.Exec(`UPDATE monitors SET group_id = ? WHERE id = ?`, groupID, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ---------- tags ----------

func (s *Store) ListTags() ([]*Tag, error) {
	rows, err := s.db.Query(`SELECT t.id, t.name, t.color, t.created_at, (SELECT COUNT(*) FROM monitor_tags mt WHERE mt.tag_id = t.id)
		FROM tags t ORDER BY LOWER(t.name)`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Tag{}
	for rows.Next() {
		t := &Tag{}
		if err := rows.Scan(&t.ID, &t.Name, &t.Color, &t.CreatedAt, &t.Monitors); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *Store) Tag(id int64) (*Tag, error) {
	t := &Tag{}
	err := s.db.QueryRow(`SELECT t.id, t.name, t.color, t.created_at, (SELECT COUNT(*) FROM monitor_tags mt WHERE mt.tag_id = t.id)
		FROM tags t WHERE t.id = ?`, id).Scan(&t.ID, &t.Name, &t.Color, &t.CreatedAt, &t.Monitors)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return t, err
}

func (s *Store) CreateTag(t *Tag) error {
	t.CreatedAt = now()
	id, err := s.db.insert(s.db, `INSERT INTO tags (name, color, created_at) VALUES (?, ?, ?)`, t.Name, t.Color, t.CreatedAt)
	if err != nil {
		return uniqueErr(err)
	}
	t.ID = id
	return nil
}

func (s *Store) UpdateTag(t *Tag) error {
	res, err := s.db.Exec(`UPDATE tags SET name = ?, color = ? WHERE id = ?`, t.Name, t.Color, t.ID)
	if err != nil {
		return uniqueErr(err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteTag removes a tag from every monitor and maintenance window.
func (s *Store) DeleteTag(id int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM tags WHERE id = ?`, id); err != nil {
		return err
	}
	if err := dropMaintRef(tx, "tag_ids", id); err != nil {
		return err
	}
	return tx.Commit()
}

// ---------- helpers ----------

func nullID(id int64) any {
	if id <= 0 {
		return nil
	}
	return id
}

// dropMaintRef removes an id from a JSON id list column of maintenances, so
// a later group or tag reusing the id is not covered by accident.
func dropMaintRef(tx *tx, col string, id int64) error {
	rows, err := tx.Query(`SELECT id, ` + col + ` FROM maintenances`)
	if err != nil {
		return err
	}
	type upd struct {
		id   int64
		list []int64
	}
	var todo []upd
	for rows.Next() {
		var mid int64
		var raw string
		if err := rows.Scan(&mid, &raw); err != nil {
			rows.Close()
			return err
		}
		var ids []int64
		_ = json.Unmarshal([]byte(raw), &ids)
		if slices.Contains(ids, id) {
			todo = append(todo, upd{mid, slices.DeleteFunc(ids, func(x int64) bool { return x == id })})
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, u := range todo {
		b, _ := json.Marshal(u.list)
		if _, err := tx.Exec(`UPDATE maintenances SET `+col+` = ? WHERE id = ?`, string(b), u.id); err != nil {
			return err
		}
	}
	return nil
}

// monitorTags returns the tag ids of every monitor.
func (s *Store) monitorTags() (map[int64][]int64, error) {
	rows, err := s.db.Query(`SELECT monitor_id, tag_id FROM monitor_tags ORDER BY monitor_id, tag_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64][]int64{}
	for rows.Next() {
		var m, t int64
		if err := rows.Scan(&m, &t); err != nil {
			return nil, err
		}
		out[m] = append(out[m], t)
	}
	return out, rows.Err()
}

func (s *Store) loadMonitorTags(m *Monitor) error {
	rows, err := s.db.Query(`SELECT tag_id FROM monitor_tags WHERE monitor_id = ? ORDER BY tag_id`, m.ID)
	if err != nil {
		return err
	}
	defer rows.Close()
	m.TagIDs = []int64{}
	for rows.Next() {
		var t int64
		if err := rows.Scan(&t); err != nil {
			return err
		}
		m.TagIDs = append(m.TagIDs, t)
	}
	return rows.Err()
}

func setMonitorTags(tx *tx, monitorID int64, tagIDs []int64) error {
	if _, err := tx.Exec(`DELETE FROM monitor_tags WHERE monitor_id = ?`, monitorID); err != nil {
		return err
	}
	for _, t := range tagIDs {
		if _, err := tx.Exec(`INSERT OR IGNORE INTO monitor_tags (monitor_id, tag_id) VALUES (?, ?)`, monitorID, t); err != nil {
			return err
		}
	}
	return nil
}

// GroupTree answers questions about how groups nest. Build it with
// NewGroupTree from ListGroups.
type GroupTree struct {
	parent   map[int64]int64
	children map[int64][]int64 // by name, as ListGroups returns them
	name     map[int64]string
}

// NewGroupTree indexes a list of groups. Parents that do not exist count
// as top level.
func NewGroupTree(groups []*Group) *GroupTree {
	t := &GroupTree{parent: map[int64]int64{}, children: map[int64][]int64{}, name: map[int64]string{}}
	for _, g := range groups {
		t.name[g.ID] = g.Name
	}
	for _, g := range groups {
		p := g.ParentID
		if _, ok := t.name[p]; !ok || p == g.ID {
			p = 0
		}
		t.parent[g.ID] = p
		t.children[p] = append(t.children[p], g.ID)
	}
	return t
}

// Has reports whether the group exists.
func (t *GroupTree) Has(id int64) bool { _, ok := t.name[id]; return ok }

// Parent returns the parent of a group, 0 for a top-level one.
func (t *GroupTree) Parent(id int64) int64 { return t.parent[id] }

// Children returns the direct subgroups of a group (0 = the top-level
// groups), by name.
func (t *GroupTree) Children(id int64) []int64 { return t.children[id] }

// Ancestors returns the parent of a group, its parent and so on up to the
// top. A cycle in stored data stops the walk.
func (t *GroupTree) Ancestors(id int64) []int64 {
	var out []int64
	seen := map[int64]bool{id: true}
	for p := t.parent[id]; p != 0 && !seen[p]; p = t.parent[p] {
		seen[p] = true
		out = append(out, p)
	}
	return out
}

// Depth is the level of a group: 1 for a top-level group.
func (t *GroupTree) Depth(id int64) int { return len(t.Ancestors(id)) + 1 }

// Height is how many levels a group and its subgroups take: 1 for a group
// without subgroups.
func (t *GroupTree) Height(id int64) int {
	return t.height(id, map[int64]bool{})
}

func (t *GroupTree) height(id int64, seen map[int64]bool) int {
	if seen[id] {
		return 0
	}
	seen[id] = true
	h := 0
	for _, c := range t.children[id] {
		h = max(h, t.height(c, seen))
	}
	return h + 1
}

// Descendants returns the subgroups of a group, theirs and so on, parents
// before their children.
func (t *GroupTree) Descendants(id int64) []int64 {
	var out []int64
	seen := map[int64]bool{id: true}
	var walk func(int64)
	walk = func(g int64) {
		for _, c := range t.children[g] {
			if !seen[c] {
				seen[c] = true
				out = append(out, c)
				walk(c)
			}
		}
	}
	walk(id)
	return out
}

// Expand returns ids with every subgroup of each, sorted, without
// duplicates. Selecting a group selects its subgroups too.
func (t *GroupTree) Expand(ids []int64) []int64 {
	out := slices.Clone(ids)
	for _, id := range ids {
		out = append(out, t.Descendants(id)...)
	}
	slices.Sort(out)
	return slices.Compact(out)
}

// Within reports whether group g is group id or one of its subgroups.
func (t *GroupTree) Within(g, id int64) bool {
	return g == id || slices.Contains(t.Ancestors(g), id)
}

// Path is the group's name after its parents', joined by sep.
func (t *GroupTree) Path(id int64, sep string) string {
	parts := []string{t.name[id]}
	for _, a := range t.Ancestors(id) {
		parts = append([]string{t.name[a]}, parts...)
	}
	return strings.Join(parts, sep)
}
