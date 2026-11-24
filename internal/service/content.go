package service

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/wbollock/blair/internal/fracindex"
)

// ---- labels ----

type Label struct {
	ID             string  `json:"id"`
	Name           string  `json:"name"`
	Color          string  `json:"color"`
	ExternalSource *string `json:"external_source"`
	ExternalID     *string `json:"external_id"`
	CreatedAt      string  `json:"created_at"`
	UpdatedAt      string  `json:"updated_at"`
	Version        int     `json:"version"`
}

type LabelCreate struct {
	ID             string  `json:"id"`
	Name           string  `json:"name"`
	Color          string  `json:"color"`
	ExternalSource *string `json:"external_source"`
	ExternalID     *string `json:"external_id"`
}

type LabelPatch struct {
	Name           Opt[string] `json:"name"`
	Color          Opt[string] `json:"color"`
	ExternalSource Opt[string] `json:"external_source"`
	ExternalID     Opt[string] `json:"external_id"`
}

func scanLabel(row interface{ Scan(...any) error }) (*Label, error) {
	var l Label
	var es, eid sql.NullString
	err := row.Scan(&l.ID, &l.Name, &l.Color, &es, &eid, &l.CreatedAt, &l.UpdatedAt, &l.Version)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	l.ExternalSource, l.ExternalID = strPtr(es), strPtr(eid)
	return &l, err
}

const labelCols = `id, name, color, external_source, external_id, created_at, updated_at, version`

func (s *Service) label(ctx context.Context, q querier, u *User, id string) (*Label, error) {
	return scanLabel(q.QueryRowContext(ctx, `SELECT `+labelCols+` FROM labels WHERE owner_id = ? AND id = ?`, u.ID, id))
}

func (s *Service) ListLabels(ctx context.Context, u *User) ([]Label, error) {
	rows, err := s.db.R.QueryContext(ctx, `SELECT `+labelCols+` FROM labels WHERE owner_id = ? ORDER BY name`, u.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Label{}
	for rows.Next() {
		l, err := scanLabel(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *l)
	}
	return out, rows.Err()
}

func validLabelName(n string) (string, error) {
	n, err := validName("name", strings.TrimPrefix(strings.TrimSpace(n), "@"), 50)
	if err != nil {
		return "", err
	}
	if strings.ContainsAny(n, " \t\n") {
		return "", invalid("name", "invalid", "label names cannot contain spaces")
	}
	return n, nil
}

func (s *Service) createLabel(ctx context.Context, q querier, u *User, in LabelCreate) (*Label, error) {
	name, err := validLabelName(in.Name)
	if err != nil {
		return nil, err
	}
	if err := validExternal(in.ExternalSource, in.ExternalID); err != nil {
		return nil, err
	}
	id := in.ID
	if id == "" {
		id = newID()
	} else if !validID(id) {
		return nil, invalid("id", "invalid", "id must be a UUID")
	}
	now := s.now()
	_, err = q.ExecContext(ctx, `INSERT INTO labels (id, owner_id, name, color, external_source, external_id, created_at, updated_at) VALUES (?,?,?,?,?,?,?,?)`,
		id, u.ID, name, in.Color, nullStr(in.ExternalSource), nullStr(in.ExternalID), now, now)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") || strings.Contains(err.Error(), "PRIMARY") {
			return nil, conflict("a label with that name or id already exists")
		}
		return nil, err
	}
	return s.label(ctx, q, u, id)
}

func (s *Service) CreateLabel(ctx context.Context, u *User, in LabelCreate) (*Label, error) {
	return s.createLabel(ctx, s.db.W, u, in)
}

func (s *Service) updateLabel(ctx context.Context, q querier, u *User, id string, p LabelPatch, ifVersion int) (*Label, error) {
	l, err := s.label(ctx, q, u, id)
	if err != nil {
		return nil, err
	}
	if ifVersion > 0 && l.Version != ifVersion {
		return nil, &PreconditionError{Current: l}
	}
	if p.Name.Set {
		if l.Name, err = validLabelName(p.Name.V); err != nil {
			return nil, err
		}
	}
	if p.Color.Set {
		l.Color = p.Color.V
	}
	if p.ExternalSource.Set {
		l.ExternalSource = optPtr(p.ExternalSource)
	}
	if p.ExternalID.Set {
		l.ExternalID = optPtr(p.ExternalID)
	}
	if err := validExternal(l.ExternalSource, l.ExternalID); err != nil {
		return nil, err
	}
	_, err = q.ExecContext(ctx, `UPDATE labels SET name=?, color=?, external_source=?, external_id=?, updated_at=?, version=version+1 WHERE owner_id=? AND id=?`,
		l.Name, l.Color, nullStr(l.ExternalSource), nullStr(l.ExternalID), s.now(), u.ID, id)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return nil, conflict("a label with that name already exists")
		}
		return nil, err
	}
	return s.label(ctx, q, u, id)
}

func (s *Service) UpdateLabel(ctx context.Context, u *User, id string, p LabelPatch, ifVersion int) (*Label, error) {
	return s.updateLabel(ctx, s.db.W, u, id, p, ifVersion)
}

func (s *Service) deleteLabel(ctx context.Context, q querier, u *User, id string) error {
	res, err := q.ExecContext(ctx, `DELETE FROM labels WHERE owner_id = ? AND id = ?`, u.ID, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Service) DeleteLabel(ctx context.Context, u *User, id string) error {
	return s.deleteLabel(ctx, s.db.W, u, id)
}

// ---- checklist ----

type ChecklistItem struct {
	ID        string `json:"id"`
	TaskID    string `json:"task_id"`
	Text      string `json:"text"`
	Done      bool   `json:"done"`
	Position  string `json:"position"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

type ChecklistCreate struct {
	ID   string `json:"id"`
	Text string `json:"text"`
	Done bool   `json:"done"`
}

type ChecklistPatch struct {
	Text    Opt[string] `json:"text"`
	Done    Opt[bool]   `json:"done"`
	AfterID Opt[string] `json:"after_id"`
}

func (s *Service) ListChecklist(ctx context.Context, u *User, taskID string) ([]ChecklistItem, error) {
	if _, err := s.task(ctx, s.db.R, u, taskID); err != nil {
		return nil, err
	}
	rows, err := s.db.R.QueryContext(ctx, `SELECT id, task_id, text, done, position, created_at, updated_at FROM checklist_items
		WHERE owner_id = ? AND task_id = ? ORDER BY position`, u.ID, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ChecklistItem{}
	for rows.Next() {
		var c ChecklistItem
		if err := rows.Scan(&c.ID, &c.TaskID, &c.Text, &c.Done, &c.Position, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Service) checklistItem(ctx context.Context, q querier, u *User, id string) (*ChecklistItem, error) {
	var c ChecklistItem
	err := q.QueryRowContext(ctx, `SELECT id, task_id, text, done, position, created_at, updated_at FROM checklist_items WHERE owner_id = ? AND id = ?`,
		u.ID, id).Scan(&c.ID, &c.TaskID, &c.Text, &c.Done, &c.Position, &c.CreatedAt, &c.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &c, err
}

func (s *Service) touchTask(ctx context.Context, q querier, u *User, taskID string) error {
	_, err := q.ExecContext(ctx, `UPDATE tasks SET updated_at = ?, version = version + 1 WHERE owner_id = ? AND id = ?`, s.now(), u.ID, taskID)
	return err
}

func (s *Service) createChecklistItem(ctx context.Context, q querier, u *User, taskID string, in ChecklistCreate) (*ChecklistItem, error) {
	if _, err := s.task(ctx, q, u, taskID); err != nil {
		return nil, err
	}
	text, err := validTitle(in.Text)
	if err != nil {
		return nil, invalid("text", "invalid", "text is required (single line, max 500)")
	}
	id := in.ID
	if id == "" {
		id = newID()
	} else if !validID(id) {
		return nil, invalid("id", "invalid", "id must be a UUID")
	}
	last, err := s.lastPosition(ctx, q, `SELECT max(position) FROM checklist_items WHERE owner_id = ? AND task_id = ?`, u.ID, taskID)
	if err != nil {
		return nil, err
	}
	now := s.now()
	if _, err := q.ExecContext(ctx, `INSERT INTO checklist_items (id, owner_id, task_id, text, done, position, created_at, updated_at) VALUES (?,?,?,?,?,?,?,?)`,
		id, u.ID, taskID, text, in.Done, fracindex.After(last), now, now); err != nil {
		return nil, err
	}
	if err := s.touchTask(ctx, q, u, taskID); err != nil {
		return nil, err
	}
	return s.checklistItem(ctx, q, u, id)
}

func (s *Service) CreateChecklistItem(ctx context.Context, u *User, taskID string, in ChecklistCreate) (c *ChecklistItem, err error) {
	err = s.db.Tx(ctx, func(tx *sql.Tx) error {
		c, err = s.createChecklistItem(ctx, tx, u, taskID, in)
		return err
	})
	return c, err
}

func (s *Service) UpdateChecklistItem(ctx context.Context, u *User, id string, p ChecklistPatch) (c *ChecklistItem, err error) {
	err = s.db.Tx(ctx, func(tx *sql.Tx) error {
		c, err = s.checklistItem(ctx, tx, u, id)
		if err != nil {
			return err
		}
		if p.Text.Set {
			if c.Text, err = validTitle(p.Text.V); err != nil {
				return invalid("text", "invalid", "text is required (single line, max 500)")
			}
		}
		if p.Done.Set {
			c.Done = p.Done.V
		}
		if p.AfterID.Set {
			after := ""
			if !p.AfterID.Null {
				a, err := s.checklistItem(ctx, tx, u, p.AfterID.V)
				if err != nil || a.TaskID != c.TaskID {
					return invalid("after_id", "not_found", "after_id must be an item of the same task")
				}
				after = a.Position
			}
			if c.Position, err = positionAfter(ctx, tx, `SELECT min(position) FROM checklist_items WHERE owner_id = ? AND task_id = ? AND id != ? AND position > ?`,
				after, u.ID, c.TaskID, id); err != nil {
				return err
			}
		}
		if _, err := tx.ExecContext(ctx, `UPDATE checklist_items SET text=?, done=?, position=?, updated_at=? WHERE owner_id=? AND id=?`,
			c.Text, c.Done, c.Position, s.now(), u.ID, id); err != nil {
			return err
		}
		if err := s.touchTask(ctx, tx, u, c.TaskID); err != nil {
			return err
		}
		c, err = s.checklistItem(ctx, tx, u, id)
		return err
	})
	return c, err
}

func (s *Service) DeleteChecklistItem(ctx context.Context, u *User, id string) error {
	return s.db.Tx(ctx, func(tx *sql.Tx) error {
		c, err := s.checklistItem(ctx, tx, u, id)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM checklist_items WHERE owner_id = ? AND id = ?`, u.ID, id); err != nil {
			return err
		}
		return s.touchTask(ctx, tx, u, c.TaskID)
	})
}

// ---- notes ----

type Note struct {
	ID             string  `json:"id"`
	Title          string  `json:"title"`
	Body           string  `json:"body"`
	Pinned         bool    `json:"pinned"`
	ProjectID      *string `json:"project_id"`
	DeletedAt      *string `json:"deleted_at"`
	ExternalSource *string `json:"external_source"`
	ExternalID     *string `json:"external_id"`
	CreatedAt      string  `json:"created_at"`
	UpdatedAt      string  `json:"updated_at"`
	Version        int     `json:"version"`
}

type NoteCreate struct {
	ID             string  `json:"id"`
	Title          string  `json:"title"`
	Body           string  `json:"body"`
	Pinned         bool    `json:"pinned"`
	ProjectID      *string `json:"project_id"`
	ExternalSource *string `json:"external_source"`
	ExternalID     *string `json:"external_id"`
	CreatedAt      *string `json:"created_at"`
}

type NotePatch struct {
	Title          Opt[string] `json:"title"`
	Body           Opt[string] `json:"body"`
	Pinned         Opt[bool]   `json:"pinned"`
	ProjectID      Opt[string] `json:"project_id"`
	ExternalSource Opt[string] `json:"external_source"`
	ExternalID     Opt[string] `json:"external_id"`
}

const noteCols = `id, title, body, pinned, project_id, deleted_at, external_source, external_id, created_at, updated_at, version`

func scanNote(row interface{ Scan(...any) error }) (*Note, error) {
	var n Note
	var pid, del, es, eid sql.NullString
	err := row.Scan(&n.ID, &n.Title, &n.Body, &n.Pinned, &pid, &del, &es, &eid, &n.CreatedAt, &n.UpdatedAt, &n.Version)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	n.ProjectID, n.DeletedAt, n.ExternalSource, n.ExternalID = strPtr(pid), strPtr(del), strPtr(es), strPtr(eid)
	return &n, err
}

func (s *Service) note(ctx context.Context, q querier, u *User, id string) (*Note, error) {
	return scanNote(q.QueryRowContext(ctx, `SELECT `+noteCols+` FROM notes WHERE owner_id = ? AND id = ?`, u.ID, id))
}

func (s *Service) GetNote(ctx context.Context, u *User, id string) (*Note, error) {
	return s.note(ctx, s.db.R, u, id)
}

type NoteFilter struct {
	ProjectID string
	Q         string
	Deleted   bool
}

func (s *Service) ListNotes(ctx context.Context, u *User, f NoteFilter) ([]Note, error) {
	where := []string{"owner_id = ?"}
	args := []any{u.ID}
	if f.Deleted {
		where = append(where, "deleted_at IS NOT NULL")
	} else {
		where = append(where, "deleted_at IS NULL")
	}
	if f.ProjectID != "" {
		where, args = append(where, "project_id = ?"), append(args, f.ProjectID)
	}
	if f.Q != "" {
		where = append(where, "id IN (SELECT ref_id FROM search_fts WHERE search_fts MATCH ? AND kind = 'note' AND owner_id = ?)")
		args = append(args, ftsQuery(f.Q), u.ID)
	}
	rows, err := s.db.R.QueryContext(ctx, `SELECT `+noteCols+` FROM notes WHERE `+strings.Join(where, " AND ")+` ORDER BY pinned DESC, updated_at DESC LIMIT 1000`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Note{}
	for rows.Next() {
		n, err := scanNote(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *n)
	}
	return out, rows.Err()
}

func (s *Service) validateNote(title, body string) error {
	if len([]rune(title)) > 500 {
		return invalid("title", "too_long", "title is at most 500 characters")
	}
	if len(body) > 200_000 {
		return invalid("body", "too_long", "body is at most 200,000 characters")
	}
	if strings.TrimSpace(title) == "" && strings.TrimSpace(body) == "" {
		return invalid("body", "required", "a note needs a title or a body")
	}
	return nil
}

func (s *Service) createNote(ctx context.Context, q querier, u *User, in NoteCreate) (*Note, bool, error) {
	if err := s.validateNote(in.Title, in.Body); err != nil {
		return nil, false, err
	}
	if err := validExternal(in.ExternalSource, in.ExternalID); err != nil {
		return nil, false, err
	}
	if in.ProjectID != nil {
		if _, err := s.project(ctx, q, u.ID, *in.ProjectID); err != nil {
			return nil, false, invalid("project_id", "not_found", "unknown project")
		}
	}
	id := in.ID
	if id == "" {
		id = newID()
	} else if !validID(id) {
		return nil, false, invalid("id", "invalid", "id must be a UUID")
	} else if n, err := s.note(ctx, q, u, id); err == nil {
		if n.Body == in.Body && n.Title == in.Title {
			return n, true, nil
		}
		return nil, false, conflict("a note with this id already exists")
	}
	now := s.now()
	created := now
	if in.CreatedAt != nil {
		v, ok := validTimestamp(*in.CreatedAt)
		if !ok {
			return nil, false, invalid("created_at", "invalid", "must be an RFC 3339 timestamp")
		}
		created = v
	}
	_, err := q.ExecContext(ctx, `INSERT INTO notes (id, owner_id, title, body, pinned, project_id, external_source, external_id, created_at, updated_at) VALUES (?,?,?,?,?,?,?,?,?,?)`,
		id, u.ID, strings.TrimSpace(in.Title), in.Body, in.Pinned, nullStr(in.ProjectID), nullStr(in.ExternalSource), nullStr(in.ExternalID), created, now)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") || strings.Contains(err.Error(), "PRIMARY") {
			return nil, false, conflict("a note with this id or external id already exists")
		}
		return nil, false, err
	}
	n, err := s.note(ctx, q, u, id)
	return n, false, err
}

func (s *Service) CreateNote(ctx context.Context, u *User, in NoteCreate) (*Note, bool, error) {
	return s.createNote(ctx, s.db.W, u, in)
}

func (s *Service) updateNote(ctx context.Context, q querier, u *User, id string, p NotePatch, ifVersion int) (*Note, error) {
	n, err := s.note(ctx, q, u, id)
	if err != nil {
		return nil, err
	}
	if ifVersion > 0 && n.Version != ifVersion {
		return nil, &PreconditionError{Current: n}
	}
	if p.Title.Set {
		n.Title = strings.TrimSpace(p.Title.V)
	}
	if p.Body.Set {
		n.Body = p.Body.V
	}
	if p.Pinned.Set {
		n.Pinned = p.Pinned.V
	}
	if p.ProjectID.Set {
		if p.ProjectID.Null {
			n.ProjectID = nil
		} else {
			if _, err := s.project(ctx, q, u.ID, p.ProjectID.V); err != nil {
				return nil, invalid("project_id", "not_found", "unknown project")
			}
			n.ProjectID = optPtr(p.ProjectID)
		}
	}
	if p.ExternalSource.Set {
		n.ExternalSource = optPtr(p.ExternalSource)
	}
	if p.ExternalID.Set {
		n.ExternalID = optPtr(p.ExternalID)
	}
	if err := validExternal(n.ExternalSource, n.ExternalID); err != nil {
		return nil, err
	}
	if err := s.validateNote(n.Title, n.Body); err != nil {
		return nil, err
	}
	_, err = q.ExecContext(ctx, `UPDATE notes SET title=?, body=?, pinned=?, project_id=?, external_source=?, external_id=?, updated_at=?, version=version+1 WHERE owner_id=? AND id=?`,
		n.Title, n.Body, n.Pinned, nullStr(n.ProjectID), nullStr(n.ExternalSource), nullStr(n.ExternalID), s.now(), u.ID, id)
	if err != nil {
		return nil, err
	}
	return s.note(ctx, q, u, id)
}

func (s *Service) UpdateNote(ctx context.Context, u *User, id string, p NotePatch, ifVersion int) (*Note, error) {
	return s.updateNote(ctx, s.db.W, u, id, p, ifVersion)
}

func (s *Service) deleteNote(ctx context.Context, q querier, u *User, id string, permanent bool) error {
	var res sql.Result
	var err error
	if permanent {
		res, err = q.ExecContext(ctx, `DELETE FROM notes WHERE owner_id = ? AND id = ?`, u.ID, id)
	} else {
		now := s.now()
		res, err = q.ExecContext(ctx, `UPDATE notes SET deleted_at = coalesce(deleted_at, ?), updated_at = ? WHERE owner_id = ? AND id = ?`, now, now, u.ID, id)
	}
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Service) DeleteNote(ctx context.Context, u *User, id string, permanent bool) error {
	return s.deleteNote(ctx, s.db.W, u, id, permanent)
}

func (s *Service) RestoreNote(ctx context.Context, u *User, id string) (*Note, error) {
	res, err := s.db.W.ExecContext(ctx, `UPDATE notes SET deleted_at = NULL, updated_at = ? WHERE owner_id = ? AND id = ? AND deleted_at IS NOT NULL`, s.now(), u.ID, id)
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, ErrNotFound
	}
	return s.note(ctx, s.db.R, u, id)
}

// ConvertNote creates a task from a note: title = note title or first line.
func (s *Service) ConvertNote(ctx context.Context, u *User, id string, deleteNote bool) (t *Task, err error) {
	err = s.db.Tx(ctx, func(tx *sql.Tx) error {
		n, err := s.note(ctx, tx, u, id)
		if err != nil {
			return err
		}
		title, desc := n.Title, n.Body
		if title == "" {
			first, rest, _ := strings.Cut(strings.TrimSpace(n.Body), "\n")
			title, desc = strings.TrimLeft(first, "# "), strings.TrimSpace(rest)
		}
		if len([]rune(title)) > 500 {
			title = string([]rune(title)[:500])
		}
		res, err := s.createTask(ctx, tx, u, TaskCreate{Title: title, Description: desc, ProjectID: n.ProjectID})
		if err != nil {
			return err
		}
		t = res.Task
		if deleteNote {
			return s.deleteNote(ctx, tx, u, id, false)
		}
		return nil
	})
	return t, err
}

// ---- search ----

type SearchHit struct {
	Kind    string `json:"kind"`
	ID      string `json:"id"`
	Title   string `json:"title"`
	Snippet string `json:"snippet"`
}

func (s *Service) Search(ctx context.Context, u *User, q string, limit int) ([]SearchHit, error) {
	if strings.TrimSpace(q) == "" {
		return []SearchHit{}, nil
	}
	if limit <= 0 || limit > 100 {
		limit = 30
	}
	rows, err := s.db.R.QueryContext(ctx, `SELECT f.kind, f.ref_id, f.title, snippet(search_fts, 4, '', '', '…', 12)
		FROM search_fts f
		LEFT JOIN tasks t ON f.kind = 'task' AND t.id = f.ref_id
		LEFT JOIN notes n ON f.kind = 'note' AND n.id = f.ref_id
		WHERE search_fts MATCH ? AND f.owner_id = ? AND coalesce(t.deleted_at, n.deleted_at) IS NULL
		ORDER BY rank LIMIT ?`, ftsQuery(q), u.ID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SearchHit{}
	for rows.Next() {
		var h SearchHit
		if err := rows.Scan(&h.Kind, &h.ID, &h.Title, &h.Snippet); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}
