package service

import (
	"context"
	"database/sql"
	"encoding/base64"
	"errors"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/wbollock/blair/internal/fracindex"
	"github.com/wbollock/blair/internal/quickadd"
	"github.com/wbollock/blair/internal/recur"
	"github.com/wbollock/blair/internal/store"
)

type Task struct {
	ID             string   `json:"id"`
	Title          string   `json:"title"`
	Description    string   `json:"description"`
	Status         string   `json:"status"`
	Priority       int      `json:"priority"`
	ProjectID      *string  `json:"project_id"`
	ColumnID       *string  `json:"column_id"`
	DueDate        *string  `json:"due_date"`
	DueTime        *string  `json:"due_time"`
	RemindAt       *string  `json:"remind_at"`
	Recurrence     *string  `json:"recurrence"`
	Labels         []string `json:"labels"`
	Position       string   `json:"position"`
	CompletedAt    *string  `json:"completed_at"`
	DeletedAt      *string  `json:"deleted_at"`
	ExternalSource *string  `json:"external_source"`
	ExternalID     *string  `json:"external_id"`
	CreatedAt      string   `json:"created_at"`
	UpdatedAt      string   `json:"updated_at"`
	Version        int      `json:"version"`
	ChecklistTotal int      `json:"checklist_total"`
	ChecklistDone  int      `json:"checklist_done"`
	IsOverdue      bool     `json:"is_overdue"`
}

// TaskCreate is the create payload. Name-based fields make scripts and agents simple.
type TaskCreate struct {
	ID             string   `json:"id"`
	Title          string   `json:"title"`
	QuickAdd       string   `json:"quick_add"`
	Description    string   `json:"description"`
	Status         *string  `json:"status"`
	Priority       *int     `json:"priority"`
	ProjectID      *string  `json:"project_id"`
	ProjectName    *string  `json:"project_name"`
	ColumnID       *string  `json:"column_id"`
	ColumnName     *string  `json:"column_name"`
	DueDate        *string  `json:"due_date"`
	DueTime        *string  `json:"due_time"`
	RemindAt       *string  `json:"remind_at"`
	Recurrence     *string  `json:"recurrence"`
	Labels         []string `json:"labels"`
	LabelNames     []string `json:"label_names"`
	CreateMissing  bool     `json:"create_missing"`
	CompletedAt    *string  `json:"completed_at"`
	CreatedAt      *string  `json:"created_at"`
	ExternalSource *string  `json:"external_source"`
	ExternalID     *string  `json:"external_id"`
	// ContextProjectID is the project view the quick-add came from (resolves /column).
	ContextProjectID string `json:"context_project_id"`
}

type TaskPatch struct {
	Title          Opt[string]   `json:"title"`
	Description    Opt[string]   `json:"description"`
	Status         Opt[string]   `json:"status"`
	Priority       Opt[int]      `json:"priority"`
	ProjectID      Opt[string]   `json:"project_id"`
	ProjectName    Opt[string]   `json:"project_name"`
	ColumnID       Opt[string]   `json:"column_id"`
	ColumnName     Opt[string]   `json:"column_name"`
	DueDate        Opt[string]   `json:"due_date"`
	DueTime        Opt[string]   `json:"due_time"`
	RemindAt       Opt[string]   `json:"remind_at"`
	Recurrence     Opt[string]   `json:"recurrence"`
	Labels         Opt[[]string] `json:"labels"`
	LabelNames     Opt[[]string] `json:"label_names"`
	CreateMissing  bool          `json:"create_missing"`
	CompletedAt    Opt[string]   `json:"completed_at"`
	ExternalSource Opt[string]   `json:"external_source"`
	ExternalID     Opt[string]   `json:"external_id"`
}

// MoveInput places a task in a list/column relative to a neighbour.
type MoveInput struct {
	ProjectID Opt[string] `json:"project_id"`
	ColumnID  Opt[string] `json:"column_id"`
	BeforeID  string      `json:"before_id"`
	AfterID   string      `json:"after_id"`
}

const taskSelect = `SELECT t.id, t.title, t.description, t.status, t.priority, t.project_id, t.column_id, t.due_date, t.due_time, t.remind_at, t.recurrence, t.position,
	t.completed_at, t.deleted_at, t.external_source, t.external_id, t.created_at, t.updated_at, t.version,
	(SELECT count(*) FROM checklist_items c WHERE c.task_id = t.id),
	(SELECT count(*) FROM checklist_items c WHERE c.task_id = t.id AND c.done = 1)
	FROM tasks t`

func scanTask(row interface{ Scan(...any) error }) (*Task, error) {
	var t Task
	var pid, cid, dd, dt, ra, rc, ca, da, es, eid sql.NullString
	err := row.Scan(&t.ID, &t.Title, &t.Description, &t.Status, &t.Priority, &pid, &cid, &dd, &dt, &ra, &rc, &t.Position,
		&ca, &da, &es, &eid, &t.CreatedAt, &t.UpdatedAt, &t.Version, &t.ChecklistTotal, &t.ChecklistDone)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	t.ProjectID, t.ColumnID, t.DueDate, t.DueTime, t.RemindAt, t.Recurrence = strPtr(pid), strPtr(cid), strPtr(dd), strPtr(dt), strPtr(ra), strPtr(rc)
	t.CompletedAt, t.DeletedAt, t.ExternalSource, t.ExternalID = strPtr(ca), strPtr(da), strPtr(es), strPtr(eid)
	t.Labels = []string{}
	return &t, nil
}

// decorate loads labels and computes is_overdue in the user's timezone.
func (s *Service) decorate(ctx context.Context, q querier, u *User, tasks []*Task) error {
	if len(tasks) == 0 {
		return nil
	}
	now := s.clock().In(u.Location())
	today, hm := now.Format("2006-01-02"), now.Format("15:04")
	byID := make(map[string]*Task, len(tasks))
	ids := make([]any, 0, len(tasks))
	for _, t := range tasks {
		byID[t.ID] = t
		ids = append(ids, t.ID)
		if !closedStatus(t.Status) && t.DueDate != nil {
			t.IsOverdue = *t.DueDate < today || (*t.DueDate == today && t.DueTime != nil && *t.DueTime < hm)
		}
	}
	for chunk := range slices.Chunk(ids, 500) {
		rows, err := q.QueryContext(ctx, `SELECT tl.task_id, tl.label_id FROM task_labels tl JOIN labels l ON l.id = tl.label_id
			WHERE tl.task_id IN (`+placeholders(len(chunk))+`) ORDER BY l.name`, chunk...)
		if err != nil {
			return err
		}
		for rows.Next() {
			var tid, lid string
			if err := rows.Scan(&tid, &lid); err != nil {
				rows.Close()
				return err
			}
			byID[tid].Labels = append(byID[tid].Labels, lid)
		}
		rows.Close()
	}
	return nil
}

func (s *Service) task(ctx context.Context, q querier, u *User, id string) (*Task, error) {
	t, err := scanTask(q.QueryRowContext(ctx, taskSelect+` WHERE t.owner_id = ? AND t.id = ?`, u.ID, id))
	if err != nil {
		return nil, err
	}
	return t, s.decorate(ctx, q, u, []*Task{t})
}

func (s *Service) GetTask(ctx context.Context, u *User, id string) (*Task, error) {
	return s.task(ctx, s.db.R, u, id)
}

// ---- name resolution ----

func (s *Service) projectByName(ctx context.Context, q querier, u *User, name string, create bool) (*Project, error) {
	var id string
	err := q.QueryRowContext(ctx, `SELECT id FROM projects WHERE owner_id = ? AND name = ? COLLATE NOCASE ORDER BY archived_at IS NOT NULL, position LIMIT 1`, u.ID, strings.TrimSpace(name)).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		if !create {
			return nil, invalid("project_name", "unknown_project", "no project named "+name+" (send create_missing: true to create it)")
		}
		return s.createProject(ctx, q, u, ProjectCreate{Name: name})
	}
	if err != nil {
		return nil, err
	}
	return s.project(ctx, q, u.ID, id)
}

func (s *Service) labelIDsByName(ctx context.Context, q querier, u *User, names []string, create bool) ([]string, error) {
	var ids []string
	for _, n := range names {
		n = strings.TrimSpace(strings.TrimPrefix(n, "@"))
		if n == "" {
			continue
		}
		var id string
		err := q.QueryRowContext(ctx, `SELECT id FROM labels WHERE owner_id = ? AND name = ?`, u.ID, n).Scan(&id)
		if errors.Is(err, sql.ErrNoRows) {
			if !create {
				return nil, invalid("label_names", "unknown_label", "no label named "+n+" (send create_missing: true to create it)")
			}
			l, err := s.createLabel(ctx, q, u, LabelCreate{Name: n})
			if err != nil {
				return nil, err
			}
			id = l.ID
		} else if err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, nil
}

func (s *Service) checkLabels(ctx context.Context, q querier, u *User, ids []string) ([]string, error) {
	out := []string{}
	for _, id := range ids {
		if slices.Contains(out, id) {
			continue
		}
		var n int
		if err := q.QueryRowContext(ctx, `SELECT count(*) FROM labels WHERE owner_id = ? AND id = ?`, u.ID, id).Scan(&n); err != nil {
			return nil, err
		}
		if n == 0 {
			return nil, invalid("labels", "not_found", "unknown label id "+id)
		}
		out = append(out, id)
	}
	return out, nil
}

func (s *Service) setLabels(ctx context.Context, q querier, taskID string, ids []string) error {
	if _, err := q.ExecContext(ctx, `DELETE FROM task_labels WHERE task_id = ?`, taskID); err != nil {
		return err
	}
	for _, id := range ids {
		if _, err := q.ExecContext(ctx, `INSERT INTO task_labels (task_id, label_id) VALUES (?, ?)`, taskID, id); err != nil {
			return err
		}
	}
	return nil
}

// quickAddContext builds the parser context from the user's projects and labels.
func (s *Service) quickAddContext(ctx context.Context, q querier, u *User, contextProjectID string) (quickadd.Context, map[string]*Project, error) {
	qc := quickadd.Context{Now: s.clock(), Loc: u.Location(), WeekStart: time.Monday}
	if u.WeekStart == "sunday" {
		qc.WeekStart = time.Sunday
	}
	byName := map[string]*Project{}
	rows, err := q.QueryContext(ctx, `SELECT id FROM projects WHERE owner_id = ? AND archived_at IS NULL ORDER BY position`, u.ID)
	if err != nil {
		return qc, nil, err
	}
	var ids []string
	for rows.Next() {
		var id string
		_ = rows.Scan(&id)
		ids = append(ids, id)
	}
	rows.Close()
	for _, id := range ids {
		p, err := s.project(ctx, q, u.ID, id)
		if err != nil {
			return qc, nil, err
		}
		qp := quickadd.Project{Name: p.Name}
		for _, c := range p.Columns {
			qp.Columns = append(qp.Columns, c.Name)
		}
		qc.Projects = append(qc.Projects, qp)
		byName[p.Name] = p
		if id == contextProjectID {
			qc.CurrentProject = p.Name
		}
	}
	lrows, err := q.QueryContext(ctx, `SELECT name FROM labels WHERE owner_id = ? ORDER BY name`, u.ID)
	if err != nil {
		return qc, nil, err
	}
	defer lrows.Close()
	for lrows.Next() {
		var n string
		_ = lrows.Scan(&n)
		qc.Labels = append(qc.Labels, n)
	}
	return qc, byName, lrows.Err()
}

// ParseQuickAdd parses without creating (POST /quick-add/parse).
func (s *Service) ParseQuickAdd(ctx context.Context, u *User, input, contextProjectID string) (*quickadd.Result, error) {
	qc, _, err := s.quickAddContext(ctx, s.db.R, u, contextProjectID)
	if err != nil {
		return nil, err
	}
	r, err := quickadd.Parse(input, qc)
	if errors.Is(err, quickadd.ErrEmptyTitle) {
		return &r, nil
	}
	return &r, err
}

// ---- create ----

func validTitle(t string) (string, error) {
	t = strings.TrimSpace(t)
	if t == "" {
		return "", invalid("title", "required", "title is required")
	}
	if len([]rune(t)) > 500 {
		return "", invalid("title", "too_long", "title is at most 500 characters")
	}
	if strings.ContainsAny(t, "\n\r") {
		return "", invalid("title", "invalid", "title must be a single line")
	}
	return t, nil
}

// CreateResult reports whether a create replayed an existing task (idempotency).
type CreateResult struct {
	Task     *Task
	Parsed   *quickadd.Result
	Existing bool
}

func (s *Service) createTask(ctx context.Context, q querier, u *User, in TaskCreate) (*CreateResult, error) {
	res := &CreateResult{}
	if in.QuickAdd != "" {
		qc, byName, err := s.quickAddContext(ctx, q, u, in.ContextProjectID)
		if err != nil {
			return nil, err
		}
		r, err := quickadd.Parse(in.QuickAdd, qc)
		if errors.Is(err, quickadd.ErrEmptyTitle) {
			return nil, invalid("quick_add", "empty_title", "add some text besides the tokens")
		} else if err != nil {
			return nil, err
		}
		res.Parsed = &r
		// Explicit fields win over parsed ones.
		if in.Title == "" {
			in.Title = r.Title
		}
		if in.ProjectID == nil && in.ProjectName == nil {
			if r.Project != "" {
				id := byName[r.Project].ID
				in.ProjectID = &id
			} else if r.ProjectNew != "" {
				if !in.CreateMissing {
					return nil, invalid("quick_add", "unknown_project", "no project named "+r.ProjectNew+" (send create_missing: true to create it)")
				}
				in.ProjectName = &r.ProjectNew
			} else if in.ContextProjectID != "" {
				in.ProjectID = &in.ContextProjectID
			}
		}
		if in.ColumnID == nil && in.ColumnName == nil {
			if r.Column != "" {
				in.ColumnName = &r.Column
			} else if r.ColumnNew != "" && in.CreateMissing {
				in.ColumnName = &r.ColumnNew // resolved against the new project's columns
			}
		}
		if in.Priority == nil && r.Priority != nil {
			in.Priority = r.Priority
		}
		if in.Recurrence == nil && r.Recurrence != "" {
			in.Recurrence = &r.Recurrence
		}
		if in.DueDate == nil && r.DueDate != "" {
			in.DueDate = &r.DueDate
			if r.DueTime != "" {
				in.DueTime = &r.DueTime
			}
		}
		if len(in.Labels) == 0 {
			in.LabelNames = append(in.LabelNames, r.Labels...)
			if len(r.LabelsNew) > 0 {
				if !in.CreateMissing {
					return nil, invalid("quick_add", "unknown_label", "no label named "+r.LabelsNew[0]+" (send create_missing: true to create it)")
				}
				in.LabelNames = append(in.LabelNames, r.LabelsNew...)
			}
		}
	}

	title, err := validTitle(in.Title)
	if err != nil {
		return nil, err
	}
	if len(in.Description) > 100_000 {
		return nil, invalid("description", "too_long", "description is at most 100,000 characters")
	}
	if err := validExternal(in.ExternalSource, in.ExternalID); err != nil {
		return nil, err
	}

	id := in.ID
	if id == "" {
		id = newID()
	} else if !validID(id) {
		return nil, invalid("id", "invalid", "id must be a UUID")
	} else {
		// Idempotent replay (SPEC §10.6): same id + same title returns the existing task.
		var owner, existingTitle string
		err := q.QueryRowContext(ctx, `SELECT owner_id, title FROM tasks WHERE id = ?`, id).Scan(&owner, &existingTitle)
		if err == nil {
			if owner == u.ID && existingTitle == title {
				t, err := s.task(ctx, q, u, id)
				if err != nil {
					return nil, err
				}
				res.Task, res.Existing = t, true
				return res, nil
			}
			return nil, conflict("a task with this id already exists")
		} else if !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
	}

	status := "todo"
	if in.Status != nil {
		if !validStatus(*in.Status) {
			return nil, invalid("status", "invalid", "status must be one of todo, doing, waiting, done, cancelled")
		}
		status = *in.Status
	}
	priority := 0
	if in.Priority != nil {
		if *in.Priority < 0 || *in.Priority > 4 {
			return nil, invalid("priority", "invalid", "priority must be 0–4")
		}
		priority = *in.Priority
	}
	if in.DueDate != nil && !validDate(*in.DueDate) {
		return nil, invalid("due_date", "invalid", "due_date must be YYYY-MM-DD")
	}
	if in.DueTime != nil && (in.DueDate == nil || !validClock(*in.DueTime)) {
		return nil, invalid("due_time", "invalid", "due_time must be HH:MM and needs a due_date")
	}
	if in.Recurrence != nil {
		rule, err := recur.Parse(*in.Recurrence)
		if err != nil {
			return nil, invalid("recurrence", "invalid", err.Error())
		}
		canon := rule.String()
		in.Recurrence = &canon
		if in.DueDate == nil {
			first := s.firstOccurrence(u, rule)
			in.DueDate = &first
		}
	}
	if in.RemindAt != nil {
		v, ok := validTimestamp(*in.RemindAt)
		if !ok {
			return nil, invalid("remind_at", "invalid", "must be an RFC 3339 timestamp")
		}
		in.RemindAt = &v
	}

	// Resolve project and column (SPEC §3.3).
	var proj *Project
	if in.ProjectID != nil {
		if proj, err = s.project(ctx, q, u.ID, *in.ProjectID); err != nil {
			return nil, invalid("project_id", "not_found", "unknown project")
		}
	} else if in.ProjectName != nil {
		if proj, err = s.projectByName(ctx, q, u, *in.ProjectName, in.CreateMissing); err != nil {
			return nil, err
		}
	}
	var col *Column
	if in.ColumnID != nil {
		if col, err = s.column(ctx, q, u.ID, *in.ColumnID); err != nil {
			return nil, invalid("column_id", "not_found", "unknown column")
		}
		if proj == nil {
			if proj, err = s.project(ctx, q, u.ID, col.ProjectID); err != nil {
				return nil, err
			}
		} else if col.ProjectID != proj.ID {
			return nil, invalid("column_id", "mismatch", "column does not belong to the project")
		}
	} else if in.ColumnName != nil {
		if proj == nil {
			return nil, invalid("column_name", "no_project", "column_name needs a project")
		}
		for i := range proj.Columns {
			if strings.EqualFold(proj.Columns[i].Name, *in.ColumnName) {
				col = &proj.Columns[i]
			}
		}
		if col == nil {
			return nil, invalid("column_name", "not_found", "no column named "+*in.ColumnName+" in "+proj.Name)
		}
	}
	if proj != nil {
		if col != nil {
			status = col.Status // moving to a column sets its status
		} else {
			col = columnForMove(proj.Columns, status)
		}
	}

	labelIDs, err := s.checkLabels(ctx, q, u, in.Labels)
	if err != nil {
		return nil, err
	}
	if len(in.LabelNames) > 0 {
		named, err := s.labelIDsByName(ctx, q, u, in.LabelNames, in.CreateMissing)
		if err != nil {
			return nil, err
		}
		for _, l := range named {
			if !slices.Contains(labelIDs, l) {
				labelIDs = append(labelIDs, l)
			}
		}
	}

	now := s.now()
	created := now
	if in.CreatedAt != nil {
		v, ok := validTimestamp(*in.CreatedAt)
		if !ok {
			return nil, invalid("created_at", "invalid", "must be an RFC 3339 timestamp")
		}
		created = v
	}
	var completed *string
	if closedStatus(status) {
		c := now
		if in.CompletedAt != nil {
			v, ok := validTimestamp(*in.CompletedAt)
			if !ok {
				return nil, invalid("completed_at", "invalid", "must be an RFC 3339 timestamp")
			}
			c = v
		}
		completed = &c
	}

	var projID, colID *string
	if proj != nil {
		projID = &proj.ID
	}
	if col != nil {
		colID = &col.ID
	}
	last, err := s.lastPosition(ctx, q, `SELECT max(position) FROM tasks WHERE owner_id = ? AND project_id IS ? AND column_id IS ?`, u.ID, nullStr(projID), nullStr(colID))
	if err != nil {
		return nil, err
	}
	_, err = q.ExecContext(ctx, `INSERT INTO tasks (id, owner_id, title, description, status, priority, project_id, column_id, due_date, due_time, remind_at, recurrence, position,
		completed_at, external_source, external_id, created_at, updated_at) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		id, u.ID, title, in.Description, status, priority, nullStr(projID), nullStr(colID), nullStr(in.DueDate), nullStr(in.DueTime), nullStr(in.RemindAt), nullStr(in.Recurrence),
		fracindex.After(last), nullStr(completed), nullStr(in.ExternalSource), nullStr(in.ExternalID), created, now)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return nil, conflict("a task with this external id already exists (use upsert)")
		}
		return nil, err
	}
	if err := s.setLabels(ctx, q, id, labelIDs); err != nil {
		return nil, err
	}
	if res.Task, err = s.task(ctx, q, u, id); err != nil {
		return nil, err
	}
	return res, nil
}

func (s *Service) CreateTask(ctx context.Context, u *User, in TaskCreate) (res *CreateResult, err error) {
	ctx, sp := span(ctx, "CreateTask")
	defer sp.End()
	err = s.db.Tx(ctx, func(tx *sql.Tx) error {
		res, err = s.createTask(ctx, tx, u, in)
		return err
	})
	return res, err
}

// ---- update / move ----

type placement struct {
	beforeID, afterID string
	set               bool
}

func (s *Service) updateTask(ctx context.Context, q querier, u *User, id string, p TaskPatch, ifVersion int, place placement) (*Task, error) {
	cur, err := s.task(ctx, q, u, id)
	if err != nil {
		return nil, err
	}
	if ifVersion > 0 && cur.Version != ifVersion {
		return nil, &PreconditionError{Current: cur}
	}
	if cur.DeletedAt != nil {
		return nil, conflict("task is in the trash; restore it first")
	}
	t := *cur
	if p.Title.Set {
		if t.Title, err = validTitle(p.Title.V); err != nil {
			return nil, err
		}
	}
	if p.Description.Set {
		if len(p.Description.V) > 100_000 {
			return nil, invalid("description", "too_long", "description is at most 100,000 characters")
		}
		t.Description = p.Description.V
	}
	if p.Priority.Set {
		if p.Priority.Null {
			t.Priority = 0
		} else if p.Priority.V < 0 || p.Priority.V > 4 {
			return nil, invalid("priority", "invalid", "priority must be 0–4")
		} else {
			t.Priority = p.Priority.V
		}
	}
	if p.DueDate.Set {
		if p.DueDate.Null {
			t.DueDate, t.DueTime = nil, nil
		} else if !validDate(p.DueDate.V) {
			return nil, invalid("due_date", "invalid", "due_date must be YYYY-MM-DD")
		} else {
			t.DueDate = optPtr(p.DueDate)
		}
	}
	if p.DueTime.Set {
		if p.DueTime.Null {
			t.DueTime = nil
		} else if t.DueDate == nil || !validClock(p.DueTime.V) {
			return nil, invalid("due_time", "invalid", "due_time must be HH:MM and needs a due_date")
		} else {
			t.DueTime = optPtr(p.DueTime)
		}
	}
	if p.Recurrence.Set {
		if p.Recurrence.Null {
			t.Recurrence = nil
		} else if rule, err := recur.Parse(p.Recurrence.V); err != nil {
			return nil, invalid("recurrence", "invalid", err.Error())
		} else {
			canon := rule.String()
			t.Recurrence = &canon
			if t.DueDate == nil {
				first := s.firstOccurrence(u, rule)
				t.DueDate = &first
			}
		}
	}
	if p.RemindAt.Set {
		if p.RemindAt.Null {
			t.RemindAt = nil
		} else if v, ok := validTimestamp(p.RemindAt.V); !ok {
			return nil, invalid("remind_at", "invalid", "must be an RFC 3339 timestamp")
		} else {
			t.RemindAt = &v
		}
	}
	if p.ExternalSource.Set {
		t.ExternalSource = optPtr(p.ExternalSource)
	}
	if p.ExternalID.Set {
		t.ExternalID = optPtr(p.ExternalID)
	}
	if err := validExternal(t.ExternalSource, t.ExternalID); err != nil {
		return nil, err
	}

	// ---- project / column / status (SPEC §3.3) ----
	newStatus := t.Status
	if p.Status.Set {
		if !validStatus(p.Status.V) {
			return nil, invalid("status", "invalid", "status must be one of todo, doing, waiting, done, cancelled")
		}
		newStatus = p.Status.V
	}
	projChanged := false
	var proj *Project
	switch {
	case p.ProjectID.Set && p.ProjectID.Null:
		t.ProjectID = nil
	case p.ProjectID.Set:
		if proj, err = s.project(ctx, q, u.ID, p.ProjectID.V); err != nil {
			return nil, invalid("project_id", "not_found", "unknown project")
		}
	case p.ProjectName.Set && !p.ProjectName.Null:
		if proj, err = s.projectByName(ctx, q, u, p.ProjectName.V, p.CreateMissing); err != nil {
			return nil, err
		}
	case t.ProjectID != nil:
		if proj, err = s.project(ctx, q, u.ID, *t.ProjectID); err != nil {
			return nil, err
		}
	}
	if proj != nil {
		projChanged = t.ProjectID == nil || *t.ProjectID != proj.ID
		t.ProjectID = &proj.ID
	} else if cur.ProjectID != nil {
		projChanged = true
	}

	var col *Column
	switch {
	case p.ColumnID.Set && !p.ColumnID.Null:
		if col, err = s.column(ctx, q, u.ID, p.ColumnID.V); err != nil {
			return nil, invalid("column_id", "not_found", "unknown column")
		}
	case p.ColumnName.Set && !p.ColumnName.Null && proj != nil:
		for i := range proj.Columns {
			if strings.EqualFold(proj.Columns[i].Name, p.ColumnName.V) {
				col = &proj.Columns[i]
			}
		}
		if col == nil {
			return nil, invalid("column_name", "not_found", "no column named "+p.ColumnName.V)
		}
	}
	if col != nil {
		if proj == nil || (col.ProjectID != proj.ID) {
			if p.ProjectID.Set || p.ProjectName.Set {
				return nil, invalid("column_id", "mismatch", "column does not belong to the project")
			}
			// The column implies its project.
			if proj, err = s.project(ctx, q, u.ID, col.ProjectID); err != nil {
				return nil, err
			}
			projChanged = t.ProjectID == nil || *t.ProjectID != proj.ID
			t.ProjectID = &proj.ID
		}
		newStatus = col.Status
		t.ColumnID = &col.ID
	} else if t.ProjectID == nil {
		if p.ColumnID.Set && !p.ColumnID.Null {
			return nil, invalid("column_id", "no_project", "inbox tasks have no column")
		}
		t.ColumnID = nil
	} else if projChanged {
		if c := columnForMove(proj.Columns, newStatus); c != nil {
			t.ColumnID = &c.ID
		}
	} else if newStatus != t.Status {
		if c := columnForStatus(proj.Columns, newStatus); c != nil {
			t.ColumnID = &c.ID
		} // else: keep the column; card shows a status badge
	}

	// Completing a repeating task moves it to its next date instead (ADR-0013).
	if t.Recurrence != nil && newStatus == "done" && !closedStatus(cur.Status) {
		if err := s.rollOver(ctx, q, u, &t, proj); err != nil {
			return nil, err
		}
		newStatus = "todo"
	}

	now := s.now()
	if closedStatus(newStatus) {
		if !closedStatus(cur.Status) {
			t.CompletedAt = &now
		}
		if p.CompletedAt.Set && !p.CompletedAt.Null {
			v, ok := validTimestamp(p.CompletedAt.V)
			if !ok {
				return nil, invalid("completed_at", "invalid", "must be an RFC 3339 timestamp")
			}
			t.CompletedAt = &v
		}
	} else {
		t.CompletedAt = nil
	}
	t.Status = newStatus

	listChanged := !eqPtr(cur.ProjectID, t.ProjectID) || !eqPtr(cur.ColumnID, t.ColumnID)
	if place.set {
		if t.Position, err = s.placeTask(ctx, q, u, &t, place); err != nil {
			return nil, err
		}
	} else if listChanged {
		last, err := s.lastPosition(ctx, q, `SELECT max(position) FROM tasks WHERE owner_id = ? AND project_id IS ? AND column_id IS ?`,
			u.ID, nullStr(t.ProjectID), nullStr(t.ColumnID))
		if err != nil {
			return nil, err
		}
		t.Position = fracindex.After(last)
	}

	if p.Labels.Set || p.LabelNames.Set {
		var ids []string
		if p.Labels.Set && !p.Labels.Null {
			if ids, err = s.checkLabels(ctx, q, u, p.Labels.V); err != nil {
				return nil, err
			}
		}
		if p.LabelNames.Set && !p.LabelNames.Null {
			named, err := s.labelIDsByName(ctx, q, u, p.LabelNames.V, p.CreateMissing)
			if err != nil {
				return nil, err
			}
			for _, l := range named {
				if !slices.Contains(ids, l) {
					ids = append(ids, l)
				}
			}
		}
		if err := s.setLabels(ctx, q, id, ids); err != nil {
			return nil, err
		}
	}

	// A new reminder time re-arms the reminder (reminded_at is cleared).
	_, err = q.ExecContext(ctx, `UPDATE tasks SET title=?, description=?, status=?, priority=?, project_id=?, column_id=?, due_date=?, due_time=?,
		reminded_at=CASE WHEN remind_at IS ? THEN reminded_at ELSE NULL END, remind_at=?, recurrence=?,
		position=?, completed_at=?, external_source=?, external_id=?, updated_at=?, version=version+1 WHERE owner_id=? AND id=?`,
		t.Title, t.Description, t.Status, t.Priority, nullStr(t.ProjectID), nullStr(t.ColumnID), nullStr(t.DueDate), nullStr(t.DueTime),
		nullStr(t.RemindAt), nullStr(t.RemindAt), nullStr(t.Recurrence),
		t.Position, nullStr(t.CompletedAt), nullStr(t.ExternalSource), nullStr(t.ExternalID), now, u.ID, id)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return nil, conflict("external id already used by another task")
		}
		return nil, err
	}
	return s.task(ctx, q, u, id)
}

// firstOccurrence is a rule's first date on or after today in the user's timezone.
func (s *Service) firstOccurrence(u *User, rule recur.Rule) string {
	now := s.clock().In(u.Location())
	return rule.Next(time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC), true).Format(time.DateOnly)
}

// rollOver logs a completion of a repeating task and moves it to its next
// occurrence after its due date or today, whichever is later. A reminder moves
// by the same number of days, the checklist is reset, and a project task goes
// back to its To do column.
func (s *Service) rollOver(ctx context.Context, q querier, u *User, t *Task, proj *Project) error {
	rule, err := recur.Parse(*t.Recurrence)
	if err != nil {
		return err
	}
	now := s.clock().In(u.Location())
	base := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	var oldDue time.Time
	if t.DueDate != nil {
		oldDue, _ = time.Parse(time.DateOnly, *t.DueDate)
		if oldDue.After(base) {
			base = oldDue
		}
	}
	if _, err := q.ExecContext(ctx, `INSERT INTO task_completions (id, owner_id, task_id, due_date, completed_at) VALUES (?,?,?,?,?)`,
		newID(), u.ID, t.ID, nullStr(t.DueDate), s.now()); err != nil {
		return err
	}
	next := rule.Next(base, false)
	if t.RemindAt != nil && !oldDue.IsZero() {
		if at, err := time.Parse(time.RFC3339Nano, *t.RemindAt); err == nil {
			shifted := store.FormatTime(at.AddDate(0, 0, int(next.Sub(oldDue).Hours()/24)))
			t.RemindAt = &shifted
		}
	}
	nd := next.Format(time.DateOnly)
	t.DueDate = &nd
	if proj != nil {
		if c := columnForStatus(proj.Columns, "todo"); c != nil {
			t.ColumnID = &c.ID
		}
	}
	_, err = q.ExecContext(ctx, `UPDATE checklist_items SET done = 0 WHERE task_id = ? AND owner_id = ?`, t.ID, u.ID)
	return err
}

func eqPtr(a, b *string) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// placeTask computes a position before/after a neighbour in the task's list.
func (s *Service) placeTask(ctx context.Context, q querier, u *User, t *Task, place placement) (string, error) {
	scope := `owner_id = ? AND project_id IS ? AND column_id IS ? AND deleted_at IS NULL AND id != ?`
	args := []any{u.ID, nullStr(t.ProjectID), nullStr(t.ColumnID), t.ID}
	neighbour := func(nid, field string) (string, error) {
		n, err := s.task(ctx, q, u, nid)
		if err != nil || !eqPtr(n.ProjectID, t.ProjectID) || !eqPtr(n.ColumnID, t.ColumnID) {
			return "", invalid(field, "not_found", field+" must be a task in the target list")
		}
		return n.Position, nil
	}
	switch {
	case place.afterID != "":
		a, err := neighbour(place.afterID, "after_id")
		if err != nil {
			return "", err
		}
		var b sql.NullString
		if err := q.QueryRowContext(ctx, `SELECT min(position) FROM tasks WHERE `+scope+` AND position > ?`, append(args, a)...).Scan(&b); err != nil {
			return "", err
		}
		return fracindex.Between(a, b.String)
	case place.beforeID != "":
		b, err := neighbour(place.beforeID, "before_id")
		if err != nil {
			return "", err
		}
		var a sql.NullString
		if err := q.QueryRowContext(ctx, `SELECT max(position) FROM tasks WHERE `+scope+` AND position < ?`, append(args, b)...).Scan(&a); err != nil {
			return "", err
		}
		return fracindex.Between(a.String, b)
	default:
		last, err := s.lastPosition(ctx, q, `SELECT max(position) FROM tasks WHERE `+scope, args...)
		if err != nil {
			return "", err
		}
		return fracindex.After(last), nil
	}
}

func (s *Service) UpdateTask(ctx context.Context, u *User, id string, p TaskPatch, ifVersion int) (t *Task, err error) {
	ctx, sp := span(ctx, "UpdateTask")
	defer sp.End()
	err = s.db.Tx(ctx, func(tx *sql.Tx) error {
		t, err = s.updateTask(ctx, tx, u, id, p, ifVersion, placement{})
		return err
	})
	return t, err
}

// MoveTask moves to a project/column and orders relative to a neighbour.
func (s *Service) MoveTask(ctx context.Context, u *User, id string, m MoveInput) (t *Task, err error) {
	err = s.db.Tx(ctx, func(tx *sql.Tx) error {
		t, err = s.updateTask(ctx, tx, u, id, TaskPatch{ProjectID: m.ProjectID, ColumnID: m.ColumnID}, 0,
			placement{set: true, beforeID: m.BeforeID, afterID: m.AfterID})
		return err
	})
	return t, err
}

// DeleteTask moves a task to the trash, or removes it when permanent.
func (s *Service) deleteTask(ctx context.Context, q querier, u *User, id string, permanent bool) error {
	var res sql.Result
	var err error
	if permanent {
		res, err = q.ExecContext(ctx, `DELETE FROM tasks WHERE owner_id = ? AND id = ?`, u.ID, id)
	} else {
		now := s.now()
		res, err = q.ExecContext(ctx, `UPDATE tasks SET deleted_at = coalesce(deleted_at, ?), updated_at = ?, version = version + 1 WHERE owner_id = ? AND id = ?`, now, now, u.ID, id)
	}
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Service) DeleteTask(ctx context.Context, u *User, id string, permanent bool) error {
	return s.deleteTask(ctx, s.db.W, u, id, permanent)
}

func (s *Service) RestoreTask(ctx context.Context, u *User, id string) (*Task, error) {
	now := s.now()
	res, err := s.db.W.ExecContext(ctx, `UPDATE tasks SET deleted_at = NULL, updated_at = ?, version = version + 1 WHERE owner_id = ? AND id = ? AND deleted_at IS NOT NULL`, now, u.ID, id)
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, ErrNotFound
	}
	return s.task(ctx, s.db.R, u, id)
}

// ---- list ----

type TaskFilter struct {
	Inbox          bool
	ProjectID      string
	ColumnID       string
	Statuses       []string
	LabelID        string
	DueFrom        string
	DueTo          string
	HasDue         *bool
	Overdue        bool
	Q              string
	Deleted        bool
	CreatedSince   string
	UpdatedSince   string
	ExternalSource string
	Sort           string
	Limit          int
	Cursor         string
}

type TaskPage struct {
	Data       []*Task `json:"data"`
	NextCursor *string `json:"next_cursor"`
}

// ftsQuery turns user text into a safe FTS5 prefix query.
func ftsQuery(q string) string {
	var parts []string
	for _, w := range strings.Fields(q) {
		w = strings.ReplaceAll(w, `"`, `""`)
		parts = append(parts, `"`+w+`"*`)
	}
	return strings.Join(parts, " ")
}

func (s *Service) ListTasks(ctx context.Context, u *User, f TaskFilter) (*TaskPage, error) {
	ctx, sp := span(ctx, "ListTasks")
	defer sp.End()
	where := []string{"t.owner_id = ?"}
	args := []any{u.ID}
	if f.Deleted {
		where = append(where, "t.deleted_at IS NOT NULL")
	} else {
		where = append(where, "t.deleted_at IS NULL")
	}
	if f.Inbox {
		where = append(where, "t.project_id IS NULL")
	}
	if f.ProjectID != "" {
		where, args = append(where, "t.project_id = ?"), append(args, f.ProjectID)
	}
	if f.ColumnID != "" {
		where, args = append(where, "t.column_id = ?"), append(args, f.ColumnID)
	}
	if len(f.Statuses) > 0 {
		for _, st := range f.Statuses {
			if !validStatus(st) {
				return nil, invalid("status", "invalid", "unknown status "+st)
			}
			args = append(args, st)
		}
		where = append(where, "t.status IN ("+placeholders(len(f.Statuses))+")")
	}
	if f.LabelID != "" {
		where, args = append(where, "EXISTS (SELECT 1 FROM task_labels tl WHERE tl.task_id = t.id AND tl.label_id = ?)"), append(args, f.LabelID)
	}
	for _, d := range []struct{ v, op, field string }{{f.DueFrom, ">=", "due_from"}, {f.DueTo, "<=", "due_to"}} {
		if d.v == "" {
			continue
		}
		if !validDate(d.v) {
			return nil, invalid(d.field, "invalid", "must be YYYY-MM-DD")
		}
		where, args = append(where, "t.due_date "+d.op+" ?"), append(args, d.v)
	}
	if f.HasDue != nil {
		if *f.HasDue {
			where = append(where, "t.due_date IS NOT NULL")
		} else {
			where = append(where, "t.due_date IS NULL")
		}
	}
	if f.Overdue {
		today := s.clock().In(u.Location()).Format("2006-01-02")
		where, args = append(where, "t.due_date < ? AND t.status NOT IN ('done','cancelled')"), append(args, today)
	}
	if f.Q != "" {
		where = append(where, "t.id IN (SELECT ref_id FROM search_fts WHERE search_fts MATCH ? AND kind = 'task' AND owner_id = ?)")
		args = append(args, ftsQuery(f.Q), u.ID)
	}
	for _, ts := range []struct{ v, col, field string }{{f.CreatedSince, "created_at", "created_since"}, {f.UpdatedSince, "updated_at", "updated_since"}} {
		if ts.v == "" {
			continue
		}
		v, ok := validTimestamp(ts.v)
		if !ok {
			return nil, invalid(ts.field, "invalid", "must be an RFC 3339 timestamp")
		}
		where, args = append(where, "t."+ts.col+" >= ?"), append(args, v)
	}
	if f.ExternalSource != "" {
		where, args = append(where, "t.external_source = ?"), append(args, f.ExternalSource)
	}
	order := map[string]string{
		"":          "t.position, t.created_at",
		"position":  "t.position, t.created_at",
		"due":       "t.due_date IS NULL, t.due_date, t.due_time IS NULL, t.due_time, t.priority DESC, t.position",
		"priority":  "t.priority DESC, t.due_date IS NULL, t.due_date, t.position",
		"created":   "t.created_at DESC, t.id",
		"updated":   "t.updated_at DESC, t.id",
		"completed": "t.completed_at DESC, t.id",
	}[f.Sort]
	if order == "" {
		return nil, invalid("sort", "invalid", "sort must be position, due, priority, created, updated or completed")
	}
	limit := f.Limit
	if limit <= 0 {
		limit = 200
	}
	if limit > 1000 {
		limit = 1000
	}
	offset := 0
	if f.Cursor != "" {
		b, err := base64.RawURLEncoding.DecodeString(f.Cursor)
		if err == nil {
			offset, err = strconv.Atoi(string(b))
		}
		if err != nil || offset < 0 {
			return nil, invalid("cursor", "invalid", "invalid cursor")
		}
	}
	rows, err := s.db.R.QueryContext(ctx, taskSelect+" WHERE "+strings.Join(where, " AND ")+" ORDER BY "+order+" LIMIT ? OFFSET ?",
		append(args, limit+1, offset)...)
	if err != nil {
		return nil, err
	}
	page := &TaskPage{Data: []*Task{}}
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		page.Data = append(page.Data, t)
	}
	rows.Close()
	if len(page.Data) > limit {
		page.Data = page.Data[:limit]
		c := base64.RawURLEncoding.EncodeToString([]byte(strconv.Itoa(offset + limit)))
		page.NextCursor = &c
	}
	return page, s.decorate(ctx, s.db.R, u, page.Data)
}

// CountOverdue counts the user's open tasks whose due date has passed, without loading them.
func (s *Service) CountOverdue(ctx context.Context, u *User) (int, error) {
	today := s.clock().In(u.Location()).Format("2006-01-02")
	var n int
	err := s.db.R.QueryRowContext(ctx, `SELECT count(*) FROM tasks WHERE owner_id = ? AND deleted_at IS NULL AND due_date < ? AND status NOT IN ('done','cancelled')`, u.ID, today).Scan(&n)
	return n, err
}
