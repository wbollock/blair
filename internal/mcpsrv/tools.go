package mcpsrv

import (
	"context"
	"slices"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/wbollock/blair/internal/service"
)

var openStatuses = []string{"todo", "doing", "waiting"}

// ---- output shapes ----

// TaskOut is a task as the model sees it. Names replace internal ids where a name exists.
type TaskOut struct {
	ID             string   `json:"id"`
	Title          string   `json:"title"`
	Description    string   `json:"description,omitempty"`
	Status         string   `json:"status"`
	Priority       int      `json:"priority" jsonschema:"0 none to 4 urgent"`
	Project        string   `json:"project,omitempty"`
	ProjectID      *string  `json:"project_id"`
	ColumnID       *string  `json:"column_id"`
	DueDate        *string  `json:"due_date"`
	DueTime        *string  `json:"due_time"`
	Labels         []string `json:"labels"`
	Recurrence     *string  `json:"recurrence"`
	CompletedAt    *string  `json:"completed_at"`
	IsOverdue      bool     `json:"is_overdue"`
	ChecklistDone  int      `json:"checklist_done"`
	ChecklistTotal int      `json:"checklist_total"`
	UpdatedAt      string   `json:"updated_at"`
	Version        int      `json:"version" jsonschema:"pass this to update_task as version"`
}

type TasksOut struct {
	Tasks      []TaskOut `json:"tasks"`
	NextCursor string    `json:"next_cursor,omitempty" jsonschema:"pass as cursor to get the next page"`
}

type refs struct {
	projects map[string]service.Project
	labels   map[string]string
	list     []service.Project
}

func (s *server) refs(ctx context.Context, u *service.User, archived bool) (*refs, error) {
	ps, err := s.svc.ListProjects(ctx, u, archived)
	if err != nil {
		return nil, err
	}
	ls, err := s.svc.ListLabels(ctx, u)
	if err != nil {
		return nil, err
	}
	r := &refs{projects: map[string]service.Project{}, labels: map[string]string{}, list: ps}
	for _, p := range ps {
		r.projects[p.ID] = p
	}
	for _, l := range ls {
		r.labels[l.ID] = l.Name
	}
	return r, nil
}

func (r *refs) task(t *service.Task, full bool) TaskOut {
	o := TaskOut{ID: t.ID, Title: t.Title, Status: t.Status, Priority: t.Priority, ProjectID: t.ProjectID, ColumnID: t.ColumnID,
		DueDate: t.DueDate, DueTime: t.DueTime, Recurrence: t.Recurrence, CompletedAt: t.CompletedAt, IsOverdue: t.IsOverdue,
		ChecklistDone: t.ChecklistDone, ChecklistTotal: t.ChecklistTotal, UpdatedAt: t.UpdatedAt, Version: t.Version, Labels: []string{}}
	o.Description = t.Description
	if !full {
		if rs := []rune(o.Description); len(rs) > 300 {
			o.Description = string(rs[:300]) + "…"
		}
	}
	if t.ProjectID != nil {
		o.Project = r.projects[*t.ProjectID].Name
	}
	for _, id := range t.Labels {
		if n, ok := r.labels[id]; ok {
			o.Labels = append(o.Labels, n)
		}
	}
	return o
}

func (r *refs) tasks(ts []*service.Task) []TaskOut {
	out := make([]TaskOut, 0, len(ts))
	for _, t := range ts {
		out = append(out, r.task(t, false))
	}
	return out
}

// project finds a project by id or (case-insensitive) name.
func (r *refs) project(ref string) (*service.Project, error) {
	if p, ok := r.projects[ref]; ok {
		return &p, nil
	}
	for i := range r.list {
		if strings.EqualFold(r.list[i].Name, strings.TrimSpace(ref)) {
			return &r.list[i], nil
		}
	}
	return nil, bad("no project " + quote(ref) + "; call list_projects to see the names")
}

func quote(s string) string {
	if len(s) > 60 {
		s = s[:60] + "…"
	}
	return `"` + s + `"`
}

func (r *refs) label(name string) (string, error) {
	for id, n := range r.labels {
		if strings.EqualFold(n, strings.TrimSpace(name)) {
			return id, nil
		}
	}
	return "", bad("no label " + quote(name))
}

func clampLimit(n, def, max int) int {
	if n <= 0 {
		return def
	}
	return min(n, max)
}

func (s *server) addTools(m *mcp.Server) {
	// ---- reads (tasks:read) ----
	register(s, m, &mcp.Tool{
		Name:        "list_tasks",
		Title:       "List tasks",
		Description: "List tasks, newest information first by due date. By default only open tasks (todo, doing, waiting). Filter by project, label, status or due date.",
	}, "tasks:read", false, s.listTasks)

	register(s, m, &mcp.Tool{
		Name:        "search",
		Title:       "Search tasks",
		Description: "Full-text search over task titles and descriptions, including finished tasks.",
	}, "tasks:read", false, s.search)

	register(s, m, &mcp.Tool{
		Name:        "get_task",
		Title:       "Get a task",
		Description: "Get one task with its full description and checklist. The version it returns is needed to update the task.",
	}, "tasks:read", false, s.getTask)

	register(s, m, &mcp.Tool{
		Name:        "today",
		Title:       "Today",
		Description: "What is on for today (in the user's time zone): tasks picked for today's plan, tasks due today, and how many are overdue.",
	}, "tasks:read", false, s.today)

	register(s, m, &mcp.Tool{
		Name:        "overdue",
		Title:       "Overdue tasks",
		Description: "Open tasks whose due date has passed, oldest first.",
	}, "tasks:read", false, s.overdue)

	register(s, m, &mcp.Tool{
		Name:        "slipping",
		Title:       "Slipping tasks",
		Description: "Open tasks that were planned for several past days without being finished.",
	}, "tasks:read", false, s.slipping)

	register(s, m, &mcp.Tool{
		Name:        "list_projects",
		Title:       "List projects",
		Description: "List projects with their columns (each column maps to a status) and open task counts. Use these names in create_task, update_task and move_task.",
	}, "tasks:read", false, s.listProjects)

	// ---- writes (tasks:write); none of them deletes anything ----
	register(s, m, &mcp.Tool{
		Name:        "create_task",
		Title:       "Create a task",
		Description: "Create a task. Project, column and labels are given by name and must already exist (see list_projects). Without a project the task goes to the Inbox.",
	}, "tasks:write", true, s.createTask)

	register(s, m, &mcp.Tool{
		Name:  "update_task",
		Title: "Update a task",
		Description: "Change fields of a task. Pass the version from get_task or the last result; if the task changed in between you get a version mismatch " +
			"and must read it again. Only the fields you send change; list a field in clear to remove its value.",
	}, "tasks:write", true, s.updateTask)

	register(s, m, &mcp.Tool{
		Name:        "complete_task",
		Title:       "Complete a task",
		Description: "Mark a task done. A repeating task rolls forward to its next date instead.",
	}, "tasks:write", true, s.completeTask)

	register(s, m, &mcp.Tool{
		Name:        "move_task",
		Title:       "Move a task",
		Description: "Move a task to another project and/or column, optionally placing it before or after another task. Moving into a column sets the task's status to match.",
	}, "tasks:write", true, s.moveTask)

	register(s, m, &mcp.Tool{
		Name:        "plan_today",
		Title:       "Plan today",
		Description: "Add tasks to today's plan and return the plan. This only adds; it never removes tasks from the plan.",
	}, "tasks:write", true, s.planToday)
}

// ---- reads ----

type listTasksIn struct {
	Project string   `json:"project,omitempty" jsonschema:"project name or id; leave out for every project"`
	Inbox   bool     `json:"inbox,omitempty" jsonschema:"only tasks that have no project"`
	Label   string   `json:"label,omitempty" jsonschema:"label name"`
	Status  []string `json:"status,omitempty" jsonschema:"any of todo, doing, waiting, done, cancelled; default is the open ones"`
	DueFrom string   `json:"due_from,omitempty" jsonschema:"YYYY-MM-DD, inclusive"`
	DueTo   string   `json:"due_to,omitempty" jsonschema:"YYYY-MM-DD, inclusive"`
	Sort    string   `json:"sort,omitempty" jsonschema:"due (default), priority, updated, created, position or completed"`
	Limit   int      `json:"limit,omitempty" jsonschema:"1 to 100, default 50"`
	Cursor  string   `json:"cursor,omitempty" jsonschema:"next_cursor from the previous page"`
}

func (s *server) listTasks(ctx context.Context, u *service.User, in listTasksIn) (TasksOut, string, error) {
	r, err := s.refs(ctx, u, true)
	if err != nil {
		return TasksOut{}, "", err
	}
	f := service.TaskFilter{Inbox: in.Inbox, DueFrom: in.DueFrom, DueTo: in.DueTo, Sort: in.Sort, Cursor: in.Cursor,
		Limit: clampLimit(in.Limit, 50, 100), Statuses: in.Status}
	if f.Sort == "" {
		f.Sort = "due"
	}
	if len(f.Statuses) == 0 {
		f.Statuses = openStatuses
	}
	if in.Project != "" {
		p, err := r.project(in.Project)
		if err != nil {
			return TasksOut{}, "", err
		}
		f.ProjectID = p.ID
	}
	if in.Label != "" {
		if f.LabelID, err = r.label(in.Label); err != nil {
			return TasksOut{}, "", err
		}
	}
	page, err := s.svc.ListTasks(ctx, u, f)
	if err != nil {
		return TasksOut{}, "", err
	}
	out := TasksOut{Tasks: r.tasks(page.Data)}
	if page.NextCursor != nil {
		out.NextCursor = *page.NextCursor
	}
	return out, "", nil
}

type searchIn struct {
	Query string `json:"query" jsonschema:"words to look for"`
	Limit int    `json:"limit,omitempty" jsonschema:"1 to 50, default 20"`
}

func (s *server) search(ctx context.Context, u *service.User, in searchIn) (TasksOut, string, error) {
	if strings.TrimSpace(in.Query) == "" {
		return TasksOut{}, "", bad("query is required")
	}
	r, err := s.refs(ctx, u, true)
	if err != nil {
		return TasksOut{}, "", err
	}
	page, err := s.svc.ListTasks(ctx, u, service.TaskFilter{Q: in.Query, Sort: "updated", Limit: clampLimit(in.Limit, 20, 50)})
	if err != nil {
		return TasksOut{}, "", err
	}
	return TasksOut{Tasks: r.tasks(page.Data)}, "", nil
}

type idIn struct {
	ID string `json:"id" jsonschema:"task id"`
}

type checklistOut struct {
	ID   string `json:"id"`
	Text string `json:"text"`
	Done bool   `json:"done"`
}

type getTaskOut struct {
	Task      TaskOut        `json:"task"`
	Checklist []checklistOut `json:"checklist"`
}

func (s *server) getTask(ctx context.Context, u *service.User, in idIn) (getTaskOut, string, error) {
	t, err := s.svc.GetTask(ctx, u, in.ID)
	if err != nil {
		return getTaskOut{}, "", err
	}
	items, err := s.svc.ListChecklist(ctx, u, in.ID)
	if err != nil {
		return getTaskOut{}, "", err
	}
	r, err := s.refs(ctx, u, true)
	if err != nil {
		return getTaskOut{}, "", err
	}
	out := getTaskOut{Task: r.task(t, true), Checklist: []checklistOut{}}
	for _, it := range items {
		out.Checklist = append(out.Checklist, checklistOut{ID: it.ID, Text: it.Text, Done: it.Done})
	}
	return out, "", nil
}

type noIn struct{}

type todayOut struct {
	Date         string    `json:"date"`
	Planned      []TaskOut `json:"planned"`
	DueToday     []TaskOut `json:"due_today"`
	OverdueCount int       `json:"overdue_count"`
}

func (s *server) today(ctx context.Context, u *service.User, _ noIn) (todayOut, string, error) {
	date, err := s.svc.PlanDate(u, "today")
	if err != nil {
		return todayOut{}, "", err
	}
	r, err := s.refs(ctx, u, true)
	if err != nil {
		return todayOut{}, "", err
	}
	plan, err := s.svc.GetPlan(ctx, u, date)
	if err != nil {
		return todayOut{}, "", err
	}
	due, err := s.svc.ListTasks(ctx, u, service.TaskFilter{DueFrom: date, DueTo: date, Statuses: openStatuses, Sort: "due", Limit: 100})
	if err != nil {
		return todayOut{}, "", err
	}
	over, err := s.svc.ListTasks(ctx, u, service.TaskFilter{Overdue: true, Limit: 1000})
	if err != nil {
		return todayOut{}, "", err
	}
	return todayOut{Date: date, Planned: r.tasks(plan.Tasks), DueToday: r.tasks(due.Data), OverdueCount: len(over.Data)}, "", nil
}

type limitIn struct {
	Limit int `json:"limit,omitempty" jsonschema:"1 to 100, default 50"`
}

func (s *server) overdue(ctx context.Context, u *service.User, in limitIn) (TasksOut, string, error) {
	r, err := s.refs(ctx, u, true)
	if err != nil {
		return TasksOut{}, "", err
	}
	page, err := s.svc.ListTasks(ctx, u, service.TaskFilter{Overdue: true, Sort: "due", Limit: clampLimit(in.Limit, 50, 100)})
	if err != nil {
		return TasksOut{}, "", err
	}
	return TasksOut{Tasks: r.tasks(page.Data)}, "", nil
}

type slippingIn struct {
	MinDays int `json:"min_days,omitempty" jsonschema:"planned on at least this many past days, default 3"`
	Days    int `json:"days,omitempty" jsonschema:"look back this many days, 1 to 365, default 30"`
}

type slippingItem struct {
	Task         TaskOut `json:"task"`
	PlannedDays  int     `json:"planned_days"`
	FirstPlanned string  `json:"first_planned"`
	LastPlanned  string  `json:"last_planned"`
}

type slippingOut struct {
	Slipping []slippingItem `json:"slipping"`
}

func (s *server) slipping(ctx context.Context, u *service.User, in slippingIn) (slippingOut, string, error) {
	if in.MinDays < 1 {
		in.MinDays = 3
	}
	if in.Days < 1 || in.Days > 365 {
		in.Days = 30
	}
	r, err := s.refs(ctx, u, true)
	if err != nil {
		return slippingOut{}, "", err
	}
	items, err := s.svc.SlippingTasks(ctx, u, in.MinDays, in.Days)
	if err != nil {
		return slippingOut{}, "", err
	}
	out := slippingOut{Slipping: []slippingItem{}}
	for _, it := range items {
		out.Slipping = append(out.Slipping, slippingItem{Task: r.task(it.Task, false), PlannedDays: it.PlannedDays, FirstPlanned: it.FirstPlanned, LastPlanned: it.LastPlanned})
	}
	return out, "", nil
}

type projectsIn struct {
	IncludeArchived bool `json:"include_archived,omitempty"`
}

type columnOut struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Status   string `json:"status"`
	WIPLimit *int   `json:"wip_limit"`
}

type projectOut struct {
	ID        string      `json:"id"`
	Name      string      `json:"name"`
	Archived  bool        `json:"archived"`
	OpenTasks int         `json:"open_tasks"`
	Columns   []columnOut `json:"columns"`
}

type projectsOut struct {
	Projects []projectOut `json:"projects"`
}

func (s *server) listProjects(ctx context.Context, u *service.User, in projectsIn) (projectsOut, string, error) {
	ps, err := s.svc.ListProjects(ctx, u, in.IncludeArchived)
	if err != nil {
		return projectsOut{}, "", err
	}
	out := projectsOut{Projects: []projectOut{}}
	for _, p := range ps {
		po := projectOut{ID: p.ID, Name: p.Name, Archived: p.ArchivedAt != nil, OpenTasks: p.OpenTasks, Columns: []columnOut{}}
		for _, c := range p.Columns {
			po.Columns = append(po.Columns, columnOut{ID: c.ID, Name: c.Name, Status: c.Status, WIPLimit: c.WIPLimit})
		}
		out.Projects = append(out.Projects, po)
	}
	return out, "", nil
}

// ---- writes ----

type createTaskIn struct {
	Title       string   `json:"title"`
	Description string   `json:"description,omitempty" jsonschema:"markdown"`
	Project     string   `json:"project,omitempty" jsonschema:"project name or id; leave out for the Inbox"`
	Column      string   `json:"column,omitempty" jsonschema:"column name within the project; sets the status to match"`
	Priority    *int     `json:"priority,omitempty" jsonschema:"0 none, 1 low, 2 medium, 3 high, 4 urgent"`
	DueDate     string   `json:"due_date,omitempty" jsonschema:"YYYY-MM-DD"`
	DueTime     string   `json:"due_time,omitempty" jsonschema:"HH:MM in the user's time zone; needs due_date"`
	Labels      []string `json:"labels,omitempty" jsonschema:"existing label names"`
}

func nonEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func (s *server) createTask(ctx context.Context, u *service.User, in createTaskIn) (TaskOut, string, error) {
	r, err := s.refs(ctx, u, false)
	if err != nil {
		return TaskOut{}, "", err
	}
	c := service.TaskCreate{Title: in.Title, Description: in.Description, Priority: in.Priority,
		DueDate: nonEmpty(in.DueDate), DueTime: nonEmpty(in.DueTime), LabelNames: in.Labels, ColumnName: nonEmpty(in.Column)}
	if in.Project != "" {
		p, err := r.project(in.Project)
		if err != nil {
			return TaskOut{}, "", err
		}
		c.ProjectID = &p.ID
	}
	for _, l := range in.Labels {
		if _, err := r.label(l); err != nil {
			return TaskOut{}, "", err
		}
	}
	res, err := s.svc.CreateTask(ctx, u, c)
	if err != nil {
		return TaskOut{}, "", err
	}
	if r, err = s.refs(ctx, u, false); err != nil {
		return TaskOut{}, "", err
	}
	return r.task(res.Task, true), res.Task.ID, nil
}

type updateTaskIn struct {
	ID          string    `json:"id"`
	Version     int       `json:"version" jsonschema:"the task's current version, from get_task or the previous result"`
	Title       *string   `json:"title,omitempty"`
	Description *string   `json:"description,omitempty" jsonschema:"markdown; replaces the whole description"`
	Status      *string   `json:"status,omitempty" jsonschema:"todo, doing, waiting, done or cancelled"`
	Priority    *int      `json:"priority,omitempty" jsonschema:"0 none to 4 urgent"`
	DueDate     *string   `json:"due_date,omitempty" jsonschema:"YYYY-MM-DD"`
	DueTime     *string   `json:"due_time,omitempty" jsonschema:"HH:MM; needs a due date"`
	Project     *string   `json:"project,omitempty" jsonschema:"project name or id to move the task to"`
	Column      *string   `json:"column,omitempty" jsonschema:"column name in the (new) project"`
	Labels      *[]string `json:"labels,omitempty" jsonschema:"the full list of existing label names; replaces the current labels"`
	Clear       []string  `json:"clear,omitempty" jsonschema:"fields to empty: due_date, due_time, project or description"`
}

func (s *server) updateTask(ctx context.Context, u *service.User, in updateTaskIn) (TaskOut, string, error) {
	if in.Version < 1 {
		return TaskOut{}, "", bad("version is required: call get_task and pass the version it returns")
	}
	r, err := s.refs(ctx, u, false)
	if err != nil {
		return TaskOut{}, "", err
	}
	var p service.TaskPatch
	if in.Title != nil {
		p.Title = service.Some(*in.Title)
	}
	if in.Description != nil {
		p.Description = service.Some(*in.Description)
	}
	if in.Status != nil {
		p.Status = service.Some(*in.Status)
	}
	if in.Priority != nil {
		p.Priority = service.Some(*in.Priority)
	}
	if in.DueDate != nil {
		p.DueDate = service.Some(*in.DueDate)
	}
	if in.DueTime != nil {
		p.DueTime = service.Some(*in.DueTime)
	}
	if in.Project != nil {
		pr, err := r.project(*in.Project)
		if err != nil {
			return TaskOut{}, "", err
		}
		p.ProjectID = service.Some(pr.ID)
	}
	if in.Column != nil {
		p.ColumnName = service.Some(*in.Column)
	}
	if in.Labels != nil {
		for _, l := range *in.Labels {
			if _, err := r.label(l); err != nil {
				return TaskOut{}, "", err
			}
		}
		p.LabelNames = service.Some(*in.Labels)
	}
	for _, f := range in.Clear {
		switch f {
		case "due_date":
			p.DueDate = service.Opt[string]{Set: true, Null: true}
		case "due_time":
			p.DueTime = service.Opt[string]{Set: true, Null: true}
		case "project":
			p.ProjectID = service.Opt[string]{Set: true, Null: true}
		case "description":
			p.Description = service.Some("")
		default:
			return TaskOut{}, "", bad("clear can only list due_date, due_time, project or description")
		}
	}
	t, err := s.svc.UpdateTask(ctx, u, in.ID, p, in.Version)
	if err != nil {
		return TaskOut{}, "", err
	}
	return r.task(t, true), t.ID, nil
}

func (s *server) completeTask(ctx context.Context, u *service.User, in idIn) (TaskOut, string, error) {
	cur, err := s.svc.GetTask(ctx, u, in.ID)
	if err != nil {
		return TaskOut{}, "", err
	}
	t := cur
	if cur.Status != "done" {
		if t, err = s.svc.UpdateTask(ctx, u, in.ID, service.TaskPatch{Status: service.Some("done")}, 0); err != nil {
			return TaskOut{}, "", err
		}
	}
	r, err := s.refs(ctx, u, false)
	if err != nil {
		return TaskOut{}, "", err
	}
	return r.task(t, true), t.ID, nil
}

type moveTaskIn struct {
	ID       string  `json:"id"`
	Project  *string `json:"project,omitempty" jsonschema:"project name or id; default is the task's current project"`
	Column   *string `json:"column,omitempty" jsonschema:"column name in that project"`
	BeforeID string  `json:"before_id,omitempty" jsonschema:"place the task just before this task"`
	AfterID  string  `json:"after_id,omitempty" jsonschema:"place the task just after this task"`
}

func (s *server) moveTask(ctx context.Context, u *service.User, in moveTaskIn) (TaskOut, string, error) {
	r, err := s.refs(ctx, u, false)
	if err != nil {
		return TaskOut{}, "", err
	}
	cur, err := s.svc.GetTask(ctx, u, in.ID)
	if err != nil {
		return TaskOut{}, "", err
	}
	m := service.MoveInput{BeforeID: in.BeforeID, AfterID: in.AfterID}
	var target *service.Project
	switch {
	case in.Project != nil:
		if target, err = r.project(*in.Project); err != nil {
			return TaskOut{}, "", err
		}
		m.ProjectID = service.Some(target.ID)
	case cur.ProjectID != nil:
		if p, ok := r.projects[*cur.ProjectID]; ok {
			target = &p
		}
	}
	if in.Column != nil {
		if target == nil {
			return TaskOut{}, "", bad("the task has no project; give a project to move it into")
		}
		i := slices.IndexFunc(target.Columns, func(c service.Column) bool { return strings.EqualFold(c.Name, strings.TrimSpace(*in.Column)) })
		if i < 0 {
			return TaskOut{}, "", bad("no column " + quote(*in.Column) + " in " + quote(target.Name) + "; call list_projects")
		}
		m.ColumnID = service.Some(target.Columns[i].ID)
	}
	t, err := s.svc.MoveTask(ctx, u, in.ID, m)
	if err != nil {
		return TaskOut{}, "", err
	}
	return r.task(t, true), t.ID, nil
}

type planIn struct {
	AddTaskIDs []string `json:"add_task_ids,omitempty" jsonschema:"ids of tasks to add to today's plan (up to 25); leave out to just read the plan"`
}

type planOut struct {
	Date  string    `json:"date"`
	Tasks []TaskOut `json:"tasks"`
}

func (s *server) planToday(ctx context.Context, u *service.User, in planIn) (planOut, string, error) {
	if len(in.AddTaskIDs) > 25 {
		return planOut{}, "", bad("add at most 25 tasks at a time")
	}
	date, err := s.svc.PlanDate(u, "today")
	if err != nil {
		return planOut{}, "", err
	}
	var p *service.Plan
	if len(in.AddTaskIDs) > 0 {
		p, err = s.svc.AddToPlan(ctx, u, date, in.AddTaskIDs)
	} else {
		p, err = s.svc.GetPlan(ctx, u, date)
	}
	if err != nil {
		return planOut{}, "", err
	}
	r, err := s.refs(ctx, u, true)
	if err != nil {
		return planOut{}, "", err
	}
	return planOut{Date: p.Date, Tasks: r.tasks(p.Tasks)}, date, nil
}
