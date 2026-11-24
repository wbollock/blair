package service

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/wbollock/blair/internal/fracindex"
)

type Column struct {
	ID        string `json:"id"`
	ProjectID string `json:"project_id"`
	Name      string `json:"name"`
	Status    string `json:"status"`
	Position  string `json:"position"`
	WIPLimit  *int   `json:"wip_limit"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
	Version   int    `json:"version"`
}

type Project struct {
	ID             string   `json:"id"`
	Name           string   `json:"name"`
	Description    string   `json:"description"`
	Color          string   `json:"color"`
	Icon           string   `json:"icon"`
	DefaultView    string   `json:"default_view"`
	Position       string   `json:"position"`
	ArchivedAt     *string  `json:"archived_at"`
	ExternalSource *string  `json:"external_source"`
	ExternalID     *string  `json:"external_id"`
	CreatedAt      string   `json:"created_at"`
	UpdatedAt      string   `json:"updated_at"`
	Version        int      `json:"version"`
	Columns        []Column `json:"columns"`
	OpenTasks      int      `json:"open_tasks"`
}

type ColumnCreate struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Status   string `json:"status"`
	WIPLimit *int   `json:"wip_limit"`
	AfterID  string `json:"after_id"`
}

type ProjectCreate struct {
	ID             string         `json:"id"`
	Name           string         `json:"name"`
	Description    string         `json:"description"`
	Color          string         `json:"color"`
	Icon           string         `json:"icon"`
	DefaultView    string         `json:"default_view"`
	Columns        []ColumnCreate `json:"columns"`
	ExternalSource *string        `json:"external_source"`
	ExternalID     *string        `json:"external_id"`
	CreatedAt      *string        `json:"created_at"`
}

type ProjectPatch struct {
	Name           Opt[string] `json:"name"`
	Description    Opt[string] `json:"description"`
	Color          Opt[string] `json:"color"`
	Icon           Opt[string] `json:"icon"`
	DefaultView    Opt[string] `json:"default_view"`
	Archived       Opt[bool]   `json:"archived"`
	AfterID        Opt[string] `json:"after_id"` // reorder: null = first
	ExternalSource Opt[string] `json:"external_source"`
	ExternalID     Opt[string] `json:"external_id"`
}

type ColumnPatch struct {
	Name     Opt[string] `json:"name"`
	Status   Opt[string] `json:"status"`
	WIPLimit Opt[int]    `json:"wip_limit"`
	AfterID  Opt[string] `json:"after_id"` // reorder: null = first
}

// DefaultColumns for a new project (SPEC §3.3 rule 2).
var DefaultColumns = []ColumnCreate{{Name: "To do", Status: "todo"}, {Name: "Doing", Status: "doing"}, {Name: "Done", Status: "done"}}

func validName(field, v string, max int) (string, error) {
	v = strings.TrimSpace(v)
	if v == "" {
		return "", invalid(field, "required", field+" is required")
	}
	if len([]rune(v)) > max {
		return "", invalid(field, "too_long", field+" is too long")
	}
	return v, nil
}

func validView(v string) bool { return v == "list" || v == "board" }

func validExternal(src, id *string) error {
	if (src == nil) != (id == nil) {
		return invalid("external_id", "pair", "external_source and external_id must be set together")
	}
	if src != nil && (*src == "" || len(*src) > 50 || *id == "" || len(*id) > 200) {
		return invalid("external_id", "invalid", "external_source (≤50) and external_id (≤200) must be non-empty")
	}
	return nil
}

func (s *Service) columns(ctx context.Context, q querier, owner, projectID string) ([]Column, error) {
	rows, err := q.QueryContext(ctx, `SELECT id, project_id, name, status, position, wip_limit, created_at, updated_at, version
		FROM columns WHERE owner_id = ? AND project_id = ? ORDER BY position`, owner, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Column{}
	for rows.Next() {
		var c Column
		var wip sql.NullInt64
		if err := rows.Scan(&c.ID, &c.ProjectID, &c.Name, &c.Status, &c.Position, &wip, &c.CreatedAt, &c.UpdatedAt, &c.Version); err != nil {
			return nil, err
		}
		c.WIPLimit = intPtr(wip)
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Service) column(ctx context.Context, q querier, owner, id string) (*Column, error) {
	var c Column
	var wip sql.NullInt64
	err := q.QueryRowContext(ctx, `SELECT id, project_id, name, status, position, wip_limit, created_at, updated_at, version
		FROM columns WHERE owner_id = ? AND id = ?`, owner, id).Scan(&c.ID, &c.ProjectID, &c.Name, &c.Status, &c.Position, &wip, &c.CreatedAt, &c.UpdatedAt, &c.Version)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	c.WIPLimit = intPtr(wip)
	return &c, err
}

const projectCols = `id, name, description, color, icon, default_view, position, archived_at, external_source, external_id, created_at, updated_at, version,
	(SELECT count(*) FROM tasks t WHERE t.project_id = projects.id AND t.deleted_at IS NULL AND t.status NOT IN ('done','cancelled'))`

func scanProject(row interface{ Scan(...any) error }) (*Project, error) {
	var p Project
	var arch, es, eid sql.NullString
	err := row.Scan(&p.ID, &p.Name, &p.Description, &p.Color, &p.Icon, &p.DefaultView, &p.Position, &arch, &es, &eid, &p.CreatedAt, &p.UpdatedAt, &p.Version, &p.OpenTasks)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	p.ArchivedAt, p.ExternalSource, p.ExternalID = strPtr(arch), strPtr(es), strPtr(eid)
	return &p, err
}

func (s *Service) project(ctx context.Context, q querier, owner, id string) (*Project, error) {
	p, err := scanProject(q.QueryRowContext(ctx, `SELECT `+projectCols+` FROM projects WHERE owner_id = ? AND id = ?`, owner, id))
	if err != nil {
		return nil, err
	}
	p.Columns, err = s.columns(ctx, q, owner, id)
	return p, err
}

func (s *Service) GetProject(ctx context.Context, u *User, id string) (*Project, error) {
	return s.project(ctx, s.db.R, u.ID, id)
}

func (s *Service) ListProjects(ctx context.Context, u *User, includeArchived bool) ([]Project, error) {
	query := `SELECT ` + projectCols + ` FROM projects WHERE owner_id = ?`
	if !includeArchived {
		query += ` AND archived_at IS NULL`
	}
	rows, err := s.db.R.QueryContext(ctx, query+` ORDER BY position`, u.ID)
	if err != nil {
		return nil, err
	}
	out := []Project{}
	for rows.Next() {
		p, err := scanProject(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, *p)
	}
	rows.Close()
	for i := range out {
		if out[i].Columns, err = s.columns(ctx, s.db.R, u.ID, out[i].ID); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (s *Service) lastPosition(ctx context.Context, q querier, query string, args ...any) (string, error) {
	var p sql.NullString
	err := q.QueryRowContext(ctx, query, args...).Scan(&p)
	return p.String, err
}

// positionAfter returns a key right after afterPos among sibling keys in table/scope.
func positionAfter(ctx context.Context, q querier, nextQuery string, afterPos string, args ...any) (string, error) {
	var next sql.NullString
	if err := q.QueryRowContext(ctx, nextQuery, append(args, afterPos)...).Scan(&next); err != nil {
		return "", err
	}
	return fracindex.Between(afterPos, next.String)
}

func (s *Service) createProject(ctx context.Context, q querier, u *User, in ProjectCreate) (*Project, error) {
	name, err := validName("name", in.Name, 100)
	if err != nil {
		return nil, err
	}
	if in.DefaultView == "" {
		in.DefaultView = "list"
	}
	if !validView(in.DefaultView) {
		return nil, invalid("default_view", "invalid", "list or board")
	}
	if err := validExternal(in.ExternalSource, in.ExternalID); err != nil {
		return nil, err
	}
	if len(in.Description) > 10000 || len(in.Color) > 32 || len(in.Icon) > 32 {
		return nil, invalid("description", "too_long", "description, color or icon too long")
	}
	id := in.ID
	if id == "" {
		id = newID()
	} else if !validID(id) {
		return nil, invalid("id", "invalid", "id must be a UUID")
	} else if existing, err := s.project(ctx, q, u.ID, id); err == nil {
		if existing.Name == name {
			return existing, nil // idempotent replay
		}
		return nil, conflict("a project with this id already exists")
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
	last, err := s.lastPosition(ctx, q, `SELECT max(position) FROM projects WHERE owner_id = ?`, u.ID)
	if err != nil {
		return nil, err
	}
	cols := in.Columns
	if len(cols) == 0 {
		cols = DefaultColumns
	}
	if err := checkColumnSet(cols); err != nil {
		return nil, err
	}
	_, err = q.ExecContext(ctx, `INSERT INTO projects (id, owner_id, name, description, color, icon, default_view, position, external_source, external_id, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`, id, u.ID, name, in.Description, in.Color, in.Icon, in.DefaultView,
		fracindex.After(last), nullStr(in.ExternalSource), nullStr(in.ExternalID), created, now)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return nil, conflict("a project with this id or external id already exists")
		}
		return nil, err
	}
	pos := ""
	for _, c := range cols {
		pos = fracindex.After(pos)
		cid := c.ID
		if cid == "" {
			cid = newID()
		}
		cname, err := validName("columns.name", c.Name, 50)
		if err != nil {
			return nil, err
		}
		if _, err := q.ExecContext(ctx, `INSERT INTO columns (id, owner_id, project_id, name, status, position, wip_limit, created_at, updated_at) VALUES (?,?,?,?,?,?,?,?,?)`,
			cid, u.ID, id, cname, c.Status, pos, c.WIPLimit, now, now); err != nil {
			return nil, err
		}
	}
	return s.project(ctx, q, u.ID, id)
}

func checkColumnSet(cols []ColumnCreate) error {
	var todo, done bool
	for _, c := range cols {
		if !validStatus(c.Status) {
			return invalid("columns.status", "invalid", "status must be one of todo, doing, waiting, done, cancelled")
		}
		todo = todo || c.Status == "todo"
		done = done || c.Status == "done"
	}
	if !todo || !done {
		return invalid("columns", "invariant", "a project needs at least one todo column and one done column")
	}
	return nil
}

func (s *Service) CreateProject(ctx context.Context, u *User, in ProjectCreate) (p *Project, err error) {
	err = s.db.Tx(ctx, func(tx *sql.Tx) error {
		p, err = s.createProject(ctx, tx, u, in)
		return err
	})
	return p, err
}

func (s *Service) updateProject(ctx context.Context, q querier, u *User, id string, in ProjectPatch, ifVersion int) (*Project, error) {
	p, err := s.project(ctx, q, u.ID, id)
	if err != nil {
		return nil, err
	}
	if ifVersion > 0 && p.Version != ifVersion {
		return nil, &PreconditionError{Current: p}
	}
	if in.Name.Set {
		if p.Name, err = validName("name", in.Name.V, 100); err != nil {
			return nil, err
		}
	}
	if in.Description.Set {
		p.Description = in.Description.V
	}
	if in.Color.Set {
		p.Color = in.Color.V
	}
	if in.Icon.Set {
		p.Icon = in.Icon.V
	}
	if in.DefaultView.Set {
		if !validView(in.DefaultView.V) {
			return nil, invalid("default_view", "invalid", "list or board")
		}
		p.DefaultView = in.DefaultView.V
	}
	if in.Archived.Set {
		if in.Archived.V && p.ArchivedAt == nil {
			now := s.now()
			p.ArchivedAt = &now
		} else if !in.Archived.V {
			p.ArchivedAt = nil
		}
	}
	if in.ExternalSource.Set || in.ExternalID.Set {
		if in.ExternalSource.Set {
			p.ExternalSource = optPtr(in.ExternalSource)
		}
		if in.ExternalID.Set {
			p.ExternalID = optPtr(in.ExternalID)
		}
		if err := validExternal(p.ExternalSource, p.ExternalID); err != nil {
			return nil, err
		}
	}
	if in.AfterID.Set {
		after := ""
		if !in.AfterID.Null {
			ap, err := s.project(ctx, q, u.ID, in.AfterID.V)
			if err != nil {
				return nil, invalid("after_id", "not_found", "after_id is not one of your projects")
			}
			after = ap.Position
		}
		if p.Position, err = positionAfter(ctx, q, `SELECT min(position) FROM projects WHERE owner_id = ? AND id != ? AND position > ?`, after, u.ID, id); err != nil {
			return nil, err
		}
	}
	_, err = q.ExecContext(ctx, `UPDATE projects SET name=?, description=?, color=?, icon=?, default_view=?, archived_at=?, position=?, external_source=?, external_id=?,
		updated_at=?, version=version+1 WHERE owner_id=? AND id=?`, p.Name, p.Description, p.Color, p.Icon, p.DefaultView, nullStr(p.ArchivedAt), p.Position,
		nullStr(p.ExternalSource), nullStr(p.ExternalID), s.now(), u.ID, id)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return nil, conflict("external id already used by another project")
		}
		return nil, err
	}
	return s.project(ctx, q, u.ID, id)
}

func optPtr(o Opt[string]) *string {
	if o.Null {
		return nil
	}
	v := o.V
	return &v
}

func (s *Service) UpdateProject(ctx context.Context, u *User, id string, in ProjectPatch, ifVersion int) (p *Project, err error) {
	err = s.db.Tx(ctx, func(tx *sql.Tx) error {
		p, err = s.updateProject(ctx, tx, u, id, in, ifVersion)
		return err
	})
	return p, err
}

// DeleteProject removes a project with its columns and tasks. Notes are kept (unlinked).
func (s *Service) deleteProject(ctx context.Context, q querier, u *User, id string) error {
	res, err := q.ExecContext(ctx, `DELETE FROM projects WHERE owner_id = ? AND id = ?`, u.ID, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Service) DeleteProject(ctx context.Context, u *User, id string) error {
	return s.deleteProject(ctx, s.db.W, u, id)
}

// ---- columns ----

func (s *Service) createColumn(ctx context.Context, q querier, u *User, projectID string, in ColumnCreate) (*Column, error) {
	if _, err := s.project(ctx, q, u.ID, projectID); err != nil {
		return nil, err
	}
	name, err := validName("name", in.Name, 50)
	if err != nil {
		return nil, err
	}
	if !validStatus(in.Status) {
		return nil, invalid("status", "invalid", "status must be one of todo, doing, waiting, done, cancelled")
	}
	if in.WIPLimit != nil && *in.WIPLimit < 1 {
		return nil, invalid("wip_limit", "invalid", "must be at least 1")
	}
	id := in.ID
	if id == "" {
		id = newID()
	} else if !validID(id) {
		return nil, invalid("id", "invalid", "id must be a UUID")
	}
	var pos string
	if in.AfterID != "" {
		after, err := s.column(ctx, q, u.ID, in.AfterID)
		if err != nil || after.ProjectID != projectID {
			return nil, invalid("after_id", "not_found", "after_id is not a column of this project")
		}
		pos, err = positionAfter(ctx, q, `SELECT min(position) FROM columns WHERE owner_id = ? AND project_id = ? AND position > ?`, after.Position, u.ID, projectID)
		if err != nil {
			return nil, err
		}
	} else {
		last, err := s.lastPosition(ctx, q, `SELECT max(position) FROM columns WHERE owner_id = ? AND project_id = ?`, u.ID, projectID)
		if err != nil {
			return nil, err
		}
		pos = fracindex.After(last)
	}
	now := s.now()
	_, err = q.ExecContext(ctx, `INSERT INTO columns (id, owner_id, project_id, name, status, position, wip_limit, created_at, updated_at) VALUES (?,?,?,?,?,?,?,?,?)`,
		id, u.ID, projectID, name, in.Status, pos, in.WIPLimit, now, now)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") || strings.Contains(err.Error(), "PRIMARY") {
			return nil, conflict("a column with this id already exists")
		}
		return nil, err
	}
	return s.column(ctx, q, u.ID, id)
}

func (s *Service) CreateColumn(ctx context.Context, u *User, projectID string, in ColumnCreate) (c *Column, err error) {
	err = s.db.Tx(ctx, func(tx *sql.Tx) error {
		c, err = s.createColumn(ctx, tx, u, projectID, in)
		return err
	})
	return c, err
}

// checkProjectInvariant enforces ≥1 todo and ≥1 done column (SPEC §3.3 rule 3).
func (s *Service) checkProjectInvariant(ctx context.Context, q querier, owner, projectID string) error {
	cols, err := s.columns(ctx, q, owner, projectID)
	if err != nil {
		return err
	}
	cc := make([]ColumnCreate, len(cols))
	for i, c := range cols {
		cc[i] = ColumnCreate{Status: c.Status}
	}
	if err := checkColumnSet(cc); err != nil {
		return conflict("a project must keep at least one todo column and one done column")
	}
	return nil
}

func (s *Service) updateColumn(ctx context.Context, q querier, u *User, id string, in ColumnPatch, ifVersion int) (*Column, error) {
	c, err := s.column(ctx, q, u.ID, id)
	if err != nil {
		return nil, err
	}
	if ifVersion > 0 && c.Version != ifVersion {
		return nil, &PreconditionError{Current: c}
	}
	if in.Name.Set {
		if c.Name, err = validName("name", in.Name.V, 50); err != nil {
			return nil, err
		}
	}
	statusChanged := false
	if in.Status.Set {
		if !validStatus(in.Status.V) {
			return nil, invalid("status", "invalid", "status must be one of todo, doing, waiting, done, cancelled")
		}
		statusChanged = in.Status.V != c.Status
		c.Status = in.Status.V
	}
	if in.WIPLimit.Set {
		if in.WIPLimit.Null {
			c.WIPLimit = nil
		} else if in.WIPLimit.V < 1 {
			return nil, invalid("wip_limit", "invalid", "must be at least 1")
		} else {
			v := in.WIPLimit.V
			c.WIPLimit = &v
		}
	}
	if in.AfterID.Set {
		after := ""
		if !in.AfterID.Null {
			ac, err := s.column(ctx, q, u.ID, in.AfterID.V)
			if err != nil || ac.ProjectID != c.ProjectID {
				return nil, invalid("after_id", "not_found", "after_id is not a column of this project")
			}
			after = ac.Position
		}
		if c.Position, err = positionAfter(ctx, q, `SELECT min(position) FROM columns WHERE owner_id = ? AND project_id = ? AND id != ? AND position > ?`,
			after, u.ID, c.ProjectID, id); err != nil {
			return nil, err
		}
	}
	now := s.now()
	if _, err := q.ExecContext(ctx, `UPDATE columns SET name=?, status=?, wip_limit=?, position=?, updated_at=?, version=version+1 WHERE owner_id=? AND id=?`,
		c.Name, c.Status, c.WIPLimit, c.Position, now, u.ID, id); err != nil {
		return nil, err
	}
	if statusChanged {
		if err := s.checkProjectInvariant(ctx, q, u.ID, c.ProjectID); err != nil {
			return nil, err
		}
		// Tasks in the column follow its status (rule 4).
		if closedStatus(c.Status) {
			_, err = q.ExecContext(ctx, `UPDATE tasks SET status=?, completed_at=coalesce(completed_at, ?), updated_at=?, version=version+1 WHERE owner_id=? AND column_id=?`,
				c.Status, now, now, u.ID, id)
		} else {
			_, err = q.ExecContext(ctx, `UPDATE tasks SET status=?, completed_at=NULL, updated_at=?, version=version+1 WHERE owner_id=? AND column_id=?`,
				c.Status, now, u.ID, id)
		}
		if err != nil {
			return nil, err
		}
	}
	return s.column(ctx, q, u.ID, id)
}

func (s *Service) UpdateColumn(ctx context.Context, u *User, id string, in ColumnPatch, ifVersion int) (c *Column, err error) {
	err = s.db.Tx(ctx, func(tx *sql.Tx) error {
		c, err = s.updateColumn(ctx, tx, u, id, in, ifVersion)
		return err
	})
	return c, err
}

// DeleteColumn requires moveTo when the column has tasks (rule 6).
func (s *Service) deleteColumn(ctx context.Context, q querier, u *User, id, moveTo string) error {
	c, err := s.column(ctx, q, u.ID, id)
	if err != nil {
		return err
	}
	var n int
	if err := q.QueryRowContext(ctx, `SELECT count(*) FROM tasks WHERE owner_id = ? AND column_id = ?`, u.ID, id).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		if moveTo == "" {
			return invalid("move_tasks_to", "required", "this column has tasks; choose a column to move them to")
		}
		target, err := s.column(ctx, q, u.ID, moveTo)
		if err != nil || target.ProjectID != c.ProjectID || target.ID == id {
			return invalid("move_tasks_to", "invalid", "move_tasks_to must be another column of this project")
		}
		now := s.now()
		last, err := s.lastPosition(ctx, q, `SELECT max(position) FROM tasks WHERE owner_id = ? AND column_id = ?`, u.ID, target.ID)
		if err != nil {
			return err
		}
		rows, err := q.QueryContext(ctx, `SELECT id FROM tasks WHERE owner_id = ? AND column_id = ? ORDER BY position`, u.ID, id)
		if err != nil {
			return err
		}
		var ids []string
		for rows.Next() {
			var tid string
			_ = rows.Scan(&tid)
			ids = append(ids, tid)
		}
		rows.Close()
		for _, tid := range ids {
			last = fracindex.After(last)
			var completed any // NULL clears completed_at for open statuses
			if closedStatus(target.Status) {
				completed = now
			}
			if _, err := q.ExecContext(ctx, `UPDATE tasks SET column_id=?, status=?, position=?,
				completed_at = CASE WHEN ? IS NULL THEN NULL ELSE coalesce(completed_at, ?) END,
				updated_at=?, version=version+1 WHERE id=?`,
				target.ID, target.Status, last, completed, completed, now, tid); err != nil {
				return err
			}
		}
	}
	if _, err := q.ExecContext(ctx, `DELETE FROM columns WHERE owner_id = ? AND id = ?`, u.ID, id); err != nil {
		return err
	}
	return s.checkProjectInvariant(ctx, q, u.ID, c.ProjectID)
}

func (s *Service) DeleteColumn(ctx context.Context, u *User, id, moveTo string) error {
	return s.db.Tx(ctx, func(tx *sql.Tx) error { return s.deleteColumn(ctx, tx, u, id, moveTo) })
}

// columnForStatus picks the first column (by position) with the status.
func columnForStatus(cols []Column, status string) *Column {
	for i := range cols {
		if cols[i].Status == status {
			return &cols[i]
		}
	}
	return nil
}

// columnForMove picks a column when a task lands in a project (rule 5).
func columnForMove(cols []Column, status string) *Column {
	if c := columnForStatus(cols, status); c != nil {
		return c
	}
	if status == "cancelled" {
		if c := columnForStatus(cols, "done"); c != nil {
			return c
		}
	}
	if c := columnForStatus(cols, "todo"); c != nil {
		return c
	}
	if len(cols) > 0 {
		return &cols[0]
	}
	return nil
}
