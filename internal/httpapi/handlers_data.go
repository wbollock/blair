package httpapi

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/wbollock/blair/internal/quickadd"
	"github.com/wbollock/blair/internal/service"
)

func list[T any](w http.ResponseWriter, r *http.Request, data []T, err error) {
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": data})
}

// ---- projects & columns ----

func (a *API) listProjects(w http.ResponseWriter, r *http.Request) {
	out, err := a.svc.ListProjects(r.Context(), userFrom(r.Context()), r.URL.Query().Get("include_archived") == "true")
	list(w, r, out, err)
}

func (a *API) getProject(w http.ResponseWriter, r *http.Request) {
	p, err := a.svc.GetProject(r.Context(), userFrom(r.Context()), chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	setETag(w, p.Version)
	writeJSON(w, http.StatusOK, p)
}

func (a *API) createProject(w http.ResponseWriter, r *http.Request) {
	var in service.ProjectCreate
	if err := decode(r, &in); err != nil {
		writeError(w, r, err)
		return
	}
	p, err := a.svc.CreateProject(r.Context(), userFrom(r.Context()), in)
	if err != nil {
		writeError(w, r, err)
		return
	}
	setETag(w, p.Version)
	w.Header().Set("Location", "/api/v1/projects/"+p.ID)
	writeJSON(w, http.StatusCreated, p)
}

func (a *API) patchProject(w http.ResponseWriter, r *http.Request) {
	var in service.ProjectPatch
	if err := decode(r, &in); err != nil {
		writeError(w, r, err)
		return
	}
	v, err := ifMatch(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	p, err := a.svc.UpdateProject(r.Context(), userFrom(r.Context()), chi.URLParam(r, "id"), in, v)
	if err != nil {
		writeError(w, r, err)
		return
	}
	setETag(w, p.Version)
	writeJSON(w, http.StatusOK, p)
}

func (a *API) deleteProject(w http.ResponseWriter, r *http.Request) {
	if err := a.svc.DeleteProject(r.Context(), userFrom(r.Context()), chi.URLParam(r, "id")); err != nil {
		writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) createColumn(w http.ResponseWriter, r *http.Request) {
	var in service.ColumnCreate
	if err := decode(r, &in); err != nil {
		writeError(w, r, err)
		return
	}
	c, err := a.svc.CreateColumn(r.Context(), userFrom(r.Context()), chi.URLParam(r, "id"), in)
	if err != nil {
		writeError(w, r, err)
		return
	}
	setETag(w, c.Version)
	writeJSON(w, http.StatusCreated, c)
}

func (a *API) patchColumn(w http.ResponseWriter, r *http.Request) {
	var in service.ColumnPatch
	if err := decode(r, &in); err != nil {
		writeError(w, r, err)
		return
	}
	v, err := ifMatch(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	c, err := a.svc.UpdateColumn(r.Context(), userFrom(r.Context()), chi.URLParam(r, "id"), in, v)
	if err != nil {
		writeError(w, r, err)
		return
	}
	setETag(w, c.Version)
	writeJSON(w, http.StatusOK, c)
}

func (a *API) deleteColumn(w http.ResponseWriter, r *http.Request) {
	if err := a.svc.DeleteColumn(r.Context(), userFrom(r.Context()), chi.URLParam(r, "id"), r.URL.Query().Get("move_tasks_to")); err != nil {
		writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---- tasks ----

func (a *API) listTasks(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := service.TaskFilter{
		Inbox:          q.Get("inbox") == "true",
		ProjectID:      q.Get("project_id"),
		ColumnID:       q.Get("column_id"),
		LabelID:        q.Get("label_id"),
		DueFrom:        q.Get("due_from"),
		DueTo:          q.Get("due_to"),
		Overdue:        q.Get("overdue") == "true",
		Q:              q.Get("q"),
		Deleted:        q.Get("deleted") == "true",
		CreatedSince:   q.Get("created_since"),
		UpdatedSince:   q.Get("updated_since"),
		ExternalSource: q.Get("external_source"),
		Sort:           q.Get("sort"),
		Cursor:         q.Get("cursor"),
	}
	if s := q.Get("status"); s != "" {
		f.Statuses = strings.Split(s, ",")
	}
	if s := q.Get("has_due"); s != "" {
		b := s == "true"
		f.HasDue = &b
	}
	if s := q.Get("limit"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n < 1 {
			writeProblem(w, r, http.StatusBadRequest, "bad_request", "Invalid limit", "limit must be a positive integer")
			return
		}
		f.Limit = n
	}
	page, err := a.svc.ListTasks(r.Context(), userFrom(r.Context()), f)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, page)
}

func (a *API) getTask(w http.ResponseWriter, r *http.Request) {
	t, err := a.svc.GetTask(r.Context(), userFrom(r.Context()), chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	setETag(w, t.Version)
	writeJSON(w, http.StatusOK, t)
}

type taskWithParse struct {
	*service.Task
	Parsed *quickadd.Result `json:"parsed,omitempty"`
}

func (a *API) createTask(w http.ResponseWriter, r *http.Request) {
	var in service.TaskCreate
	if err := decode(r, &in); err != nil {
		writeError(w, r, err)
		return
	}
	res, err := a.svc.CreateTask(r.Context(), userFrom(r.Context()), in)
	if err != nil {
		writeError(w, r, err)
		return
	}
	setETag(w, res.Task.Version)
	status := http.StatusCreated
	if res.Existing {
		status = http.StatusOK
	}
	w.Header().Set("Location", "/api/v1/tasks/"+res.Task.ID)
	writeJSON(w, status, taskWithParse{Task: res.Task, Parsed: res.Parsed})
}

func (a *API) patchTask(w http.ResponseWriter, r *http.Request) {
	var in service.TaskPatch
	if err := decode(r, &in); err != nil {
		writeError(w, r, err)
		return
	}
	v, err := ifMatch(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	t, err := a.svc.UpdateTask(r.Context(), userFrom(r.Context()), chi.URLParam(r, "id"), in, v)
	if err != nil {
		writeError(w, r, err)
		return
	}
	setETag(w, t.Version)
	writeJSON(w, http.StatusOK, t)
}

func (a *API) deleteTask(w http.ResponseWriter, r *http.Request) {
	if err := a.svc.DeleteTask(r.Context(), userFrom(r.Context()), chi.URLParam(r, "id"), r.URL.Query().Get("permanent") == "true"); err != nil {
		writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) moveTask(w http.ResponseWriter, r *http.Request) {
	var in service.MoveInput
	if err := decode(r, &in); err != nil {
		writeError(w, r, err)
		return
	}
	t, err := a.svc.MoveTask(r.Context(), userFrom(r.Context()), chi.URLParam(r, "id"), in)
	if err != nil {
		writeError(w, r, err)
		return
	}
	setETag(w, t.Version)
	writeJSON(w, http.StatusOK, t)
}

func (a *API) restoreTask(w http.ResponseWriter, r *http.Request) {
	t, err := a.svc.RestoreTask(r.Context(), userFrom(r.Context()), chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, t)
}

func (a *API) listChecklist(w http.ResponseWriter, r *http.Request) {
	out, err := a.svc.ListChecklist(r.Context(), userFrom(r.Context()), chi.URLParam(r, "id"))
	list(w, r, out, err)
}

func (a *API) createChecklist(w http.ResponseWriter, r *http.Request) {
	var in service.ChecklistCreate
	if err := decode(r, &in); err != nil {
		writeError(w, r, err)
		return
	}
	c, err := a.svc.CreateChecklistItem(r.Context(), userFrom(r.Context()), chi.URLParam(r, "id"), in)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, c)
}

func (a *API) patchChecklist(w http.ResponseWriter, r *http.Request) {
	var in service.ChecklistPatch
	if err := decode(r, &in); err != nil {
		writeError(w, r, err)
		return
	}
	c, err := a.svc.UpdateChecklistItem(r.Context(), userFrom(r.Context()), chi.URLParam(r, "id"), in)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, c)
}

func (a *API) deleteChecklist(w http.ResponseWriter, r *http.Request) {
	if err := a.svc.DeleteChecklistItem(r.Context(), userFrom(r.Context()), chi.URLParam(r, "id")); err != nil {
		writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---- labels ----

func (a *API) listLabels(w http.ResponseWriter, r *http.Request) {
	out, err := a.svc.ListLabels(r.Context(), userFrom(r.Context()))
	list(w, r, out, err)
}

func (a *API) createLabel(w http.ResponseWriter, r *http.Request) {
	var in service.LabelCreate
	if err := decode(r, &in); err != nil {
		writeError(w, r, err)
		return
	}
	l, err := a.svc.CreateLabel(r.Context(), userFrom(r.Context()), in)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, l)
}

func (a *API) patchLabel(w http.ResponseWriter, r *http.Request) {
	var in service.LabelPatch
	if err := decode(r, &in); err != nil {
		writeError(w, r, err)
		return
	}
	v, err := ifMatch(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	l, err := a.svc.UpdateLabel(r.Context(), userFrom(r.Context()), chi.URLParam(r, "id"), in, v)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, l)
}

func (a *API) deleteLabel(w http.ResponseWriter, r *http.Request) {
	if err := a.svc.DeleteLabel(r.Context(), userFrom(r.Context()), chi.URLParam(r, "id")); err != nil {
		writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---- notes ----

func (a *API) listNotes(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	out, err := a.svc.ListNotes(r.Context(), userFrom(r.Context()), service.NoteFilter{ProjectID: q.Get("project_id"), Q: q.Get("q"), Deleted: q.Get("deleted") == "true"})
	list(w, r, out, err)
}

func (a *API) getNote(w http.ResponseWriter, r *http.Request) {
	n, err := a.svc.GetNote(r.Context(), userFrom(r.Context()), chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	setETag(w, n.Version)
	writeJSON(w, http.StatusOK, n)
}

func (a *API) createNote(w http.ResponseWriter, r *http.Request) {
	var in service.NoteCreate
	if err := decode(r, &in); err != nil {
		writeError(w, r, err)
		return
	}
	n, existing, err := a.svc.CreateNote(r.Context(), userFrom(r.Context()), in)
	if err != nil {
		writeError(w, r, err)
		return
	}
	setETag(w, n.Version)
	status := http.StatusCreated
	if existing {
		status = http.StatusOK
	}
	writeJSON(w, status, n)
}

func (a *API) patchNote(w http.ResponseWriter, r *http.Request) {
	var in service.NotePatch
	if err := decode(r, &in); err != nil {
		writeError(w, r, err)
		return
	}
	v, err := ifMatch(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	n, err := a.svc.UpdateNote(r.Context(), userFrom(r.Context()), chi.URLParam(r, "id"), in, v)
	if err != nil {
		writeError(w, r, err)
		return
	}
	setETag(w, n.Version)
	writeJSON(w, http.StatusOK, n)
}

func (a *API) deleteNote(w http.ResponseWriter, r *http.Request) {
	if err := a.svc.DeleteNote(r.Context(), userFrom(r.Context()), chi.URLParam(r, "id"), r.URL.Query().Get("permanent") == "true"); err != nil {
		writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) restoreNote(w http.ResponseWriter, r *http.Request) {
	n, err := a.svc.RestoreNote(r.Context(), userFrom(r.Context()), chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, n)
}

type convertRequest struct {
	DeleteNote bool `json:"delete_note"`
}

func (a *API) convertNote(w http.ResponseWriter, r *http.Request) {
	var in convertRequest
	if err := decode(r, &in); err != nil {
		writeError(w, r, err)
		return
	}
	t, err := a.svc.ConvertNote(r.Context(), userFrom(r.Context()), chi.URLParam(r, "id"), in.DeleteNote)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, t)
}

// ---- search, quick-add, batch, export ----

func (a *API) search(w http.ResponseWriter, r *http.Request) {
	out, err := a.svc.Search(r.Context(), userFrom(r.Context()), r.URL.Query().Get("q"), 30)
	list(w, r, out, err)
}

type parseRequest struct {
	Text             string `json:"text"`
	ContextProjectID string `json:"context_project_id"`
}

func (a *API) parseQuickAdd(w http.ResponseWriter, r *http.Request) {
	var in parseRequest
	if err := decode(r, &in); err != nil {
		writeError(w, r, err)
		return
	}
	res, err := a.svc.ParseQuickAdd(r.Context(), userFrom(r.Context()), in.Text, in.ContextProjectID)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

type batchRequest struct {
	Ops []service.BatchOp `json:"ops"`
}

func (a *API) batch(w http.ResponseWriter, r *http.Request) {
	var in batchRequest
	if err := decode(r, &in); err != nil {
		writeError(w, r, err)
		return
	}
	for _, op := range in.Ops {
		if op.Type == "note" && !hasScope(r, "notes:write") {
			writeProblem(w, r, http.StatusForbidden, "insufficient_scope", "Insufficient scope", "note ops need the notes:write scope")
			return
		}
		if strings.EqualFold(op.Op, "delete") && op.Type != "note" && !hasScope(r, "tasks:delete") {
			writeProblem(w, r, http.StatusForbidden, "insufficient_scope", "Insufficient scope", "delete ops need the tasks:delete scope")
			return
		}
	}
	u := userFrom(r.Context())
	res, err := a.svc.Batch(r.Context(), u, in.Ops, r.URL.Query().Get("dry_run") == "true")
	if err != nil {
		writeError(w, r, err)
		return
	}
	if len(in.Ops) > 100 && res.Committed {
		a.svc.Audit(r.Context(), u.ID, "batch.applied", clientInfo(r), map[string]any{"ops": len(in.Ops)})
	}
	status := http.StatusOK
	for _, x := range res.Results {
		if x.Error != nil {
			status = http.StatusUnprocessableEntity
			break
		}
	}
	writeJSON(w, status, res)
}

func (a *API) export(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r.Context())
	format := r.URL.Query().Get("format")
	a.svc.Audit(r.Context(), u.ID, "export", clientInfo(r), map[string]any{"format": format})
	switch format {
	case "", "json":
		e, err := a.svc.Export(r.Context(), u)
		if err != nil {
			writeError(w, r, err)
			return
		}
		w.Header().Set("Content-Disposition", `attachment; filename="blair-export.json"`)
		writeJSON(w, http.StatusOK, e)
	case "csv":
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		w.Header().Set("Content-Disposition", `attachment; filename="blair-tasks.csv"`)
		if err := a.svc.ExportCSV(r.Context(), u, w); err != nil {
			writeError(w, r, err)
		}
	default:
		writeProblem(w, r, http.StatusBadRequest, "bad_request", "Unknown format", "format must be json or csv")
	}
}

// ---- daily plans ----

func (a *API) planDate(w http.ResponseWriter, r *http.Request) (string, bool) {
	date, err := a.svc.PlanDate(userFrom(r.Context()), chi.URLParam(r, "date"))
	if err != nil {
		writeError(w, r, err)
		return "", false
	}
	return date, true
}

func (a *API) getPlan(w http.ResponseWriter, r *http.Request) {
	date, ok := a.planDate(w, r)
	if !ok {
		return
	}
	p, err := a.svc.GetPlan(r.Context(), userFrom(r.Context()), date)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (a *API) putPlan(w http.ResponseWriter, r *http.Request) {
	date, ok := a.planDate(w, r)
	if !ok {
		return
	}
	var in struct {
		TaskIDs []string `json:"task_ids"`
	}
	if err := decode(r, &in); err != nil {
		writeError(w, r, err)
		return
	}
	p, err := a.svc.SetPlan(r.Context(), userFrom(r.Context()), date, in.TaskIDs)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (a *API) addToPlan(w http.ResponseWriter, r *http.Request) {
	date, ok := a.planDate(w, r)
	if !ok {
		return
	}
	var in struct {
		TaskIDs []string `json:"task_ids"`
	}
	if err := decode(r, &in); err != nil {
		writeError(w, r, err)
		return
	}
	p, err := a.svc.AddToPlan(r.Context(), userFrom(r.Context()), date, in.TaskIDs)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (a *API) removeFromPlan(w http.ResponseWriter, r *http.Request) {
	date, ok := a.planDate(w, r)
	if !ok {
		return
	}
	if err := a.svc.RemoveFromPlan(r.Context(), userFrom(r.Context()), date, chi.URLParam(r, "id")); err != nil {
		writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) slipping(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	minDays, err := strconv.Atoi(q.Get("min_days"))
	if err != nil || minDays < 1 {
		minDays = 3
	}
	days, err := strconv.Atoi(q.Get("days"))
	if err != nil || days < 1 || days > 365 {
		days = 30
	}
	out, err := a.svc.SlippingTasks(r.Context(), userFrom(r.Context()), minDays, days)
	list(w, r, out, err)
}
