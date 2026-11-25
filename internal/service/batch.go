package service

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// MaxBatchOps bounds one batch request (SPEC §11.1).
const MaxBatchOps = 1000

type BatchOp struct {
	Op   string          `json:"op"`   // create | upsert | update | delete
	Type string          `json:"type"` // project | column | label | task | note
	Ref  string          `json:"ref"`
	ID   string          `json:"id"`
	Data json.RawMessage `json:"data"`
}

type BatchError struct {
	Code    string       `json:"code"`
	Message string       `json:"message"`
	Fields  []FieldError `json:"fields,omitempty"`
}

type BatchResult struct {
	Index  int         `json:"index"`
	Op     string      `json:"op"`
	Type   string      `json:"type"`
	Ref    string      `json:"ref,omitempty"`
	ID     string      `json:"id,omitempty"`
	Status string      `json:"status"` // created | updated | unchanged | deleted | error | skipped
	Error  *BatchError `json:"error,omitempty"`
}

type BatchResponse struct {
	DryRun    bool          `json:"dry_run"`
	Committed bool          `json:"committed"`
	Results   []BatchResult `json:"results"`
}

func decodeStrict(data []byte, v any) error {
	if len(data) == 0 {
		data = []byte("{}")
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return invalid("data", "invalid_json", err.Error())
	}
	return nil
}

// resolveRefs rewrites *_ref keys to *_id using ids created earlier in the batch.
func resolveRefs(data json.RawMessage, refs map[string]string, drop ...string) (json.RawMessage, map[string]json.RawMessage, error) {
	m := map[string]json.RawMessage{}
	if len(data) > 0 {
		if err := json.Unmarshal(data, &m); err != nil {
			return nil, nil, invalid("data", "invalid_json", "data must be a JSON object")
		}
	}
	for refKey, idKey := range map[string]string{"project_ref": "project_id", "column_ref": "column_id", "move_tasks_to_ref": "move_tasks_to"} {
		raw, ok := m[refKey]
		if !ok {
			continue
		}
		var ref string
		if err := json.Unmarshal(raw, &ref); err != nil {
			return nil, nil, invalid(refKey, "invalid", refKey+" must be a string")
		}
		id, ok := refs[ref]
		if !ok {
			return nil, nil, invalid(refKey, "unknown_ref", "no earlier op has ref "+strconv.Quote(ref))
		}
		m[idKey], _ = json.Marshal(id)
		delete(m, refKey)
	}
	for _, k := range drop {
		delete(m, k)
	}
	out, _ := json.Marshal(m)
	return out, m, nil
}

func externalKey(m map[string]json.RawMessage) (string, string, error) {
	var src, id string
	_ = json.Unmarshal(m["external_source"], &src)
	_ = json.Unmarshal(m["external_id"], &id)
	if src == "" || id == "" {
		return "", "", invalid("external_id", "required", "upsert needs data.external_source and data.external_id")
	}
	return src, id, nil
}

var batchTables = map[string]string{"project": "projects", "label": "labels", "task": "tasks", "note": "notes"}

// Batch applies ops in one transaction: all succeed or none are kept.
func (s *Service) Batch(ctx context.Context, u *User, ops []BatchOp, dryRun bool) (*BatchResponse, error) {
	ctx, sp := span(ctx, "Batch")
	defer sp.End()
	if len(ops) == 0 || len(ops) > MaxBatchOps {
		return nil, invalid("ops", "count", fmt.Sprintf("send between 1 and %d ops", MaxBatchOps))
	}
	tx, err := s.db.W.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	resp := &BatchResponse{DryRun: dryRun, Results: make([]BatchResult, len(ops))}
	refs := map[string]string{}
	failed := false
	for i, op := range ops {
		r := BatchResult{Index: i, Op: op.Op, Type: op.Type, Ref: op.Ref}
		id, status, err := s.applyOp(ctx, tx, u, op, refs)
		if err != nil {
			failed = true
			r.Status, r.Error = "error", toBatchError(err)
		} else {
			r.ID, r.Status = id, status
			if op.Ref != "" {
				refs[op.Ref] = id
			}
		}
		resp.Results[i] = r
	}
	if failed || dryRun {
		return resp, nil
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	resp.Committed = true
	return resp, nil
}

func toBatchError(err error) *BatchError {
	var ve *ValidationError
	var ce *ConflictError
	var pe *PreconditionError
	switch {
	case errors.As(err, &ve):
		return &BatchError{Code: "validation", Message: ve.Error(), Fields: ve.Fields}
	case errors.As(err, &ce):
		return &BatchError{Code: "conflict", Message: ce.Msg}
	case errors.As(err, &pe):
		return &BatchError{Code: "precondition_failed", Message: "version mismatch"}
	case errors.Is(err, ErrNotFound):
		return &BatchError{Code: "not_found", Message: "not found"}
	default:
		return &BatchError{Code: "internal", Message: "internal error"}
	}
}

func (s *Service) applyOp(ctx context.Context, q *sql.Tx, u *User, op BatchOp, refs map[string]string) (string, string, error) {
	if op.ID == "" && op.Ref != "" && (op.Op == "update" || op.Op == "delete") {
		op.ID = refs[op.Ref]
	}
	op.Op = strings.ToLower(op.Op)
	switch op.Op {
	case "create", "update", "delete":
	case "upsert":
		if op.Type == "column" {
			return "", "", invalid("op", "invalid", "columns do not support upsert")
		}
	default:
		return "", "", invalid("op", "invalid", "op must be create, upsert, update or delete")
	}
	if op.Type != "column" && batchTables[op.Type] == "" {
		return "", "", invalid("type", "invalid", "type must be project, column, label, task or note")
	}

	if op.Op == "upsert" {
		_, m, err := resolveRefs(op.Data, refs)
		if err != nil {
			return "", "", err
		}
		src, eid, err := externalKey(m)
		if err != nil {
			return "", "", err
		}
		var existing string
		err = q.QueryRowContext(ctx, `SELECT id FROM `+batchTables[op.Type]+` WHERE owner_id = ? AND external_source = ? AND external_id = ?`, u.ID, src, eid).Scan(&existing)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			op.Op = "create"
		case err != nil:
			return "", "", err
		default:
			op.Op, op.ID = "update", existing
			// Fields that only apply on create are dropped for the update.
			op.Data, _, _ = resolveRefs(op.Data, refs, "id", "created_at", "columns", "quick_add", "context_project_id")
		}
	}

	data, m, err := resolveRefs(op.Data, refs)
	if err != nil {
		return "", "", err
	}
	if (op.Op == "update" || op.Op == "delete") && op.ID == "" {
		return "", "", invalid("id", "required", "update and delete need an id (or the ref of an earlier op)")
	}
	created := func(id string, err error) (string, string, error) {
		if err != nil {
			return "", "", err
		}
		return id, "created", nil
	}
	updated := func(id string, err error) (string, string, error) {
		if err != nil {
			return "", "", err
		}
		return id, "updated", nil
	}

	switch op.Type + ":" + op.Op {
	case "project:create":
		var in ProjectCreate
		if err := decodeStrict(data, &in); err != nil {
			return "", "", err
		}
		p, err := s.createProject(ctx, q, u, in)
		if err != nil {
			return "", "", err
		}
		return created(p.ID, nil)
	case "project:update":
		var in ProjectPatch
		if err := decodeStrict(data, &in); err != nil {
			return "", "", err
		}
		_, err := s.updateProject(ctx, q, u, op.ID, in, 0)
		return updated(op.ID, err)
	case "project:delete":
		return op.ID, "deleted", s.deleteProject(ctx, q, u, op.ID)

	case "column:create":
		var in struct {
			ProjectID string `json:"project_id"`
			ColumnCreate
		}
		if err := decodeStrict(data, &in); err != nil {
			return "", "", err
		}
		c, err := s.createColumn(ctx, q, u, in.ProjectID, in.ColumnCreate)
		if err != nil {
			return "", "", err
		}
		return created(c.ID, nil)
	case "column:update":
		var in ColumnPatch
		if err := decodeStrict(data, &in); err != nil {
			return "", "", err
		}
		_, err := s.updateColumn(ctx, q, u, op.ID, in, 0)
		return updated(op.ID, err)
	case "column:delete":
		var moveTo string
		_ = json.Unmarshal(m["move_tasks_to"], &moveTo)
		return op.ID, "deleted", s.deleteColumn(ctx, q, u, op.ID, moveTo)

	case "label:create":
		var in LabelCreate
		if err := decodeStrict(data, &in); err != nil {
			return "", "", err
		}
		l, err := s.createLabel(ctx, q, u, in)
		if err != nil {
			return "", "", err
		}
		return created(l.ID, nil)
	case "label:update":
		var in LabelPatch
		if err := decodeStrict(data, &in); err != nil {
			return "", "", err
		}
		_, err := s.updateLabel(ctx, q, u, op.ID, in, 0)
		return updated(op.ID, err)
	case "label:delete":
		return op.ID, "deleted", s.deleteLabel(ctx, q, u, op.ID)

	case "task:create":
		var in TaskCreate
		if err := decodeStrict(data, &in); err != nil {
			return "", "", err
		}
		r, err := s.createTask(ctx, q, u, in)
		if err != nil {
			return "", "", err
		}
		if r.Existing {
			return r.Task.ID, "unchanged", nil
		}
		return created(r.Task.ID, nil)
	case "task:update":
		var in TaskPatch
		if err := decodeStrict(data, &in); err != nil {
			return "", "", err
		}
		_, err := s.updateTask(ctx, q, u, op.ID, in, 0, placement{})
		return updated(op.ID, err)
	case "task:delete":
		return op.ID, "deleted", s.deleteTask(ctx, q, u, op.ID, false)

	case "note:create":
		var in NoteCreate
		if err := decodeStrict(data, &in); err != nil {
			return "", "", err
		}
		n, existing, err := s.createNote(ctx, q, u, in)
		if err != nil {
			return "", "", err
		}
		if existing {
			return n.ID, "unchanged", nil
		}
		return created(n.ID, nil)
	case "note:update":
		var in NotePatch
		if err := decodeStrict(data, &in); err != nil {
			return "", "", err
		}
		_, err := s.updateNote(ctx, q, u, op.ID, in, 0)
		return updated(op.ID, err)
	case "note:delete":
		return op.ID, "deleted", s.deleteNote(ctx, q, u, op.ID, false)
	}
	return "", "", invalid("op", "invalid", "unsupported op")
}

// ---- export (SPEC §11.2) ----

type ExportTask struct {
	*Task
	Checklist []ChecklistItem `json:"checklist"`
}

type Export struct {
	Format     string `json:"format"`
	ExportedAt string `json:"exported_at"`
	User       struct {
		Email       string          `json:"email"`
		DisplayName string          `json:"display_name"`
		Timezone    string          `json:"timezone"`
		WeekStart   string          `json:"week_start"`
		Settings    json.RawMessage `json:"settings"`
	} `json:"user"`
	Projects []Project    `json:"projects"`
	Labels   []Label      `json:"labels"`
	Tasks    []ExportTask `json:"tasks"`
	Notes    []Note       `json:"notes"`
	// Plans and Completions are history for the daily plan and repeating tasks.
	Plans       []ExportPlanItem   `json:"plans"`
	Completions []ExportCompletion `json:"completions"`
}

type ExportPlanItem struct {
	Date   string `json:"date"`
	TaskID string `json:"task_id"`
}

type ExportCompletion struct {
	TaskID      string  `json:"task_id"`
	DueDate     *string `json:"due_date"`
	CompletedAt string  `json:"completed_at"`
}

func (s *Service) allTasks(ctx context.Context, u *User) ([]*Task, error) {
	rows, err := s.db.R.QueryContext(ctx, taskSelect+` WHERE t.owner_id = ? ORDER BY t.created_at`, u.ID)
	if err != nil {
		return nil, err
	}
	var out []*Task
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, t)
	}
	rows.Close()
	return out, s.decorate(ctx, s.db.R, u, out)
}

func (s *Service) Export(ctx context.Context, u *User) (*Export, error) {
	e := &Export{Format: "blair.export.v1", ExportedAt: s.now()}
	e.User.Email, e.User.DisplayName, e.User.Timezone, e.User.WeekStart, e.User.Settings = u.Email, u.DisplayName, u.Timezone, u.WeekStart, u.Settings
	var err error
	if e.Projects, err = s.ListProjects(ctx, u, true); err != nil {
		return nil, err
	}
	if e.Labels, err = s.ListLabels(ctx, u); err != nil {
		return nil, err
	}
	tasks, err := s.allTasks(ctx, u)
	if err != nil {
		return nil, err
	}
	checklists := map[string][]ChecklistItem{}
	rows, err := s.db.R.QueryContext(ctx, `SELECT id, task_id, text, done, position, created_at, updated_at FROM checklist_items WHERE owner_id = ? ORDER BY position`, u.ID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var c ChecklistItem
		if err := rows.Scan(&c.ID, &c.TaskID, &c.Text, &c.Done, &c.Position, &c.CreatedAt, &c.UpdatedAt); err != nil {
			rows.Close()
			return nil, err
		}
		checklists[c.TaskID] = append(checklists[c.TaskID], c)
	}
	rows.Close()
	e.Tasks = make([]ExportTask, 0, len(tasks))
	for _, t := range tasks {
		cl := checklists[t.ID]
		if cl == nil {
			cl = []ChecklistItem{}
		}
		e.Tasks = append(e.Tasks, ExportTask{Task: t, Checklist: cl})
	}
	active, err := s.ListNotes(ctx, u, NoteFilter{})
	if err != nil {
		return nil, err
	}
	trashed, err := s.ListNotes(ctx, u, NoteFilter{Deleted: true})
	if err != nil {
		return nil, err
	}
	e.Notes = append(active, trashed...)
	if err := s.exportHistory(ctx, u, e); err != nil {
		return nil, err
	}
	return e, nil
}

// ExportCSV writes one row per task.
func (s *Service) ExportCSV(ctx context.Context, u *User, w io.Writer) error {
	e, err := s.Export(ctx, u)
	if err != nil {
		return err
	}
	projects, columns, labels := map[string]string{}, map[string]string{}, map[string]string{}
	for _, p := range e.Projects {
		projects[p.ID] = p.Name
		for _, c := range p.Columns {
			columns[c.ID] = c.Name
		}
	}
	for _, l := range e.Labels {
		labels[l.ID] = l.Name
	}
	cw := csv.NewWriter(w)
	_ = cw.Write([]string{"id", "title", "description", "status", "priority", "project", "column", "labels", "due_date", "due_time", "created_at", "completed_at", "deleted_at"})
	deref := func(p *string, m map[string]string) string {
		if p == nil {
			return ""
		}
		if m != nil {
			return m[*p]
		}
		return *p
	}
	for _, t := range e.Tasks {
		var ln []string
		for _, l := range t.Labels {
			ln = append(ln, labels[l])
		}
		if err := cw.Write([]string{t.ID, csvSafe(t.Title), csvSafe(t.Description), t.Status, strconv.Itoa(t.Priority), csvSafe(deref(t.ProjectID, projects)),
			csvSafe(deref(t.ColumnID, columns)), csvSafe(strings.Join(ln, ";")), deref(t.DueDate, nil), deref(t.DueTime, nil), t.CreatedAt,
			deref(t.CompletedAt, nil), deref(t.DeletedAt, nil)}); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}

// csvSafe neutralizes spreadsheet formula injection.
func csvSafe(v string) string {
	if v != "" && strings.ContainsRune("=+-@\t\r", rune(v[0])) {
		return "'" + v
	}
	return v
}

func (s *Service) exportHistory(ctx context.Context, u *User, e *Export) error {
	e.Plans, e.Completions = []ExportPlanItem{}, []ExportCompletion{}
	rows, err := s.db.R.QueryContext(ctx, `SELECT plan_date, task_id FROM plan_items WHERE owner_id = ? ORDER BY plan_date, position`, u.ID)
	if err != nil {
		return err
	}
	for rows.Next() {
		var p ExportPlanItem
		if err := rows.Scan(&p.Date, &p.TaskID); err != nil {
			rows.Close()
			return err
		}
		e.Plans = append(e.Plans, p)
	}
	rows.Close()
	rows, err = s.db.R.QueryContext(ctx, `SELECT task_id, due_date, completed_at FROM task_completions WHERE owner_id = ? ORDER BY completed_at`, u.ID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var c ExportCompletion
		var due sql.NullString
		if err := rows.Scan(&c.TaskID, &due, &c.CompletedAt); err != nil {
			return err
		}
		c.DueDate = strPtr(due)
		e.Completions = append(e.Completions, c)
	}
	return rows.Err()
}
