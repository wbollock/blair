package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/wbollock/blair/internal/auth"
	"github.com/wbollock/blair/internal/store"
)

var testNow = time.Date(2026, 11, 24, 19, 30, 0, 0, time.UTC) // 14:30 in New York

type fixture struct {
	s   *Service
	ctx context.Context
	now time.Time
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	db, err := store.Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	box, _ := auth.NewSecretBox(bytes.Repeat([]byte{1}, 32), "totp")
	f := &fixture{s: New(db, box, Options{Registration: "invite"}), ctx: context.Background(), now: testNow}
	f.s.SetClock(func() time.Time { return f.now })
	return f
}

func (f *fixture) user(t *testing.T, email string) *User {
	t.Helper()
	u, err := f.s.CreateUser(f.ctx, NewUser{Email: email, Password: "plum-orbit-kettle-91", Timezone: "America/New_York"}, false)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

// must unwraps a call, panicking (failing the test) on error.
func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}

func colByName(p *Project, name string) Column {
	for _, c := range p.Columns {
		if c.Name == name {
			return c
		}
	}
	panic("no column " + name)
}

func TestNewProjectGetsDefaultColumns(t *testing.T) {
	f := newFixture(t)
	u := f.user(t, "a@example.com")
	p := must(f.s.CreateProject(f.ctx, u, ProjectCreate{Name: "Homelab"}))
	if len(p.Columns) != 3 || p.Columns[0].Status != "todo" || p.Columns[1].Status != "doing" || p.Columns[2].Status != "done" {
		t.Fatalf("columns = %+v", p.Columns)
	}
}

func TestColumnStatusInvariants(t *testing.T) {
	f := newFixture(t)
	u := f.user(t, "a@example.com")
	p := must(f.s.CreateProject(f.ctx, u, ProjectCreate{Name: "Work"}))
	todo, doing, done := colByName(p, "To do"), colByName(p, "Doing"), colByName(p, "Done")

	// Create in a project without a column lands in the first todo column.
	r := must(f.s.CreateTask(f.ctx, u, TaskCreate{Title: "Ship", ProjectID: &p.ID}))
	if *r.Task.ColumnID != todo.ID || r.Task.Status != "todo" {
		t.Fatalf("create landed in %v/%s", *r.Task.ColumnID, r.Task.Status)
	}
	// Move to column sets status.
	tk := must(f.s.UpdateTask(f.ctx, u, r.Task.ID, TaskPatch{ColumnID: Some(doing.ID)}, 0))
	if tk.Status != "doing" {
		t.Fatalf("status after move = %s", tk.Status)
	}
	// Setting status moves to the matching column and stamps completed_at.
	tk = must(f.s.UpdateTask(f.ctx, u, tk.ID, TaskPatch{Status: Some("done")}, 0))
	if *tk.ColumnID != done.ID || tk.CompletedAt == nil {
		t.Fatalf("done: column %s completed %v", *tk.ColumnID, tk.CompletedAt)
	}
	// Status without a matching column keeps the column (badge case).
	tk = must(f.s.UpdateTask(f.ctx, u, tk.ID, TaskPatch{Status: Some("waiting")}, 0))
	if *tk.ColumnID != done.ID || tk.Status != "waiting" || tk.CompletedAt != nil {
		t.Fatalf("waiting: column %s status %s completed %v", *tk.ColumnID, tk.Status, tk.CompletedAt)
	}
	// Changing a column's status updates its tasks.
	must(f.s.UpdateColumn(f.ctx, u, doing.ID, ColumnPatch{Status: Some("waiting")}, 0))
	r2 := must(f.s.CreateTask(f.ctx, u, TaskCreate{Title: "Wait", ColumnID: &doing.ID}))
	if r2.Task.Status != "waiting" {
		t.Fatalf("task in waiting column has status %s", r2.Task.Status)
	}
	// Can't delete the last done column.
	var ce *ConflictError
	if err := f.s.DeleteColumn(f.ctx, u, done.ID, todo.ID); !errors.As(err, &ce) {
		t.Fatalf("deleting last done column: %v", err)
	}
	// Deleting a non-empty column needs a target and moves tasks with status.
	if err := f.s.DeleteColumn(f.ctx, u, doing.ID, ""); err == nil {
		t.Fatal("deleted a non-empty column without move_tasks_to")
	}
	if err := f.s.DeleteColumn(f.ctx, u, doing.ID, todo.ID); err != nil {
		t.Fatal(err)
	}
	moved := must(f.s.GetTask(f.ctx, u, r2.Task.ID))
	if *moved.ColumnID != todo.ID || moved.Status != "todo" {
		t.Fatalf("moved task %s/%s", *moved.ColumnID, moved.Status)
	}
}

func TestMoveToProjectAndInbox(t *testing.T) {
	f := newFixture(t)
	u := f.user(t, "a@example.com")
	p := must(f.s.CreateProject(f.ctx, u, ProjectCreate{Name: "Work"}))
	r := must(f.s.CreateTask(f.ctx, u, TaskCreate{Title: "Inbox item"}))
	if r.Task.ProjectID != nil || r.Task.ColumnID != nil {
		t.Fatal("expected inbox task")
	}
	tk := must(f.s.UpdateTask(f.ctx, u, r.Task.ID, TaskPatch{ProjectID: Some(p.ID)}, 0))
	if *tk.ColumnID != colByName(p, "To do").ID {
		t.Fatal("moving into a project should pick the todo column")
	}
	tk = must(f.s.UpdateTask(f.ctx, u, tk.ID, TaskPatch{ProjectID: Opt[string]{Set: true, Null: true}}, 0))
	if tk.ProjectID != nil || tk.ColumnID != nil {
		t.Fatal("moving to inbox should clear project and column")
	}
}

func TestQuickAddCreate(t *testing.T) {
	f := newFixture(t)
	u := f.user(t, "a@example.com")
	p := must(f.s.CreateProject(f.ctx, u, ProjectCreate{Name: "Homelab"}))
	must(f.s.CreateLabel(f.ctx, u, LabelCreate{Name: "house"}))
	r := must(f.s.CreateTask(f.ctx, u, TaskCreate{QuickAdd: "Fix gutter #homelab /doing !high fri at 9am @house"}))
	tk := r.Task
	if tk.Title != "Fix gutter" || *tk.ProjectID != p.ID || *tk.ColumnID != colByName(p, "Doing").ID || tk.Status != "doing" ||
		tk.Priority != 3 || *tk.DueDate != "2026-11-27" || *tk.DueTime != "09:00" || len(tk.Labels) != 1 {
		t.Fatalf("quick-add task = %+v", tk)
	}
	if _, err := f.s.CreateTask(f.ctx, u, TaskCreate{QuickAdd: "Ship it #newproj"}); err == nil {
		t.Fatal("unknown project without create_missing should fail")
	}
	r = must(f.s.CreateTask(f.ctx, u, TaskCreate{QuickAdd: "Ship it #newproj @fresh", CreateMissing: true}))
	if r.Task.ProjectID == nil || len(r.Task.Labels) != 1 {
		t.Fatal("create_missing should create project and label")
	}
}

func TestIdempotentCreate(t *testing.T) {
	f := newFixture(t)
	u := f.user(t, "a@example.com")
	id := newID()
	a := must(f.s.CreateTask(f.ctx, u, TaskCreate{ID: id, Title: "Offline capture"}))
	b := must(f.s.CreateTask(f.ctx, u, TaskCreate{ID: id, Title: "Offline capture"}))
	if a.Existing || !b.Existing || b.Task.ID != id {
		t.Fatalf("replay: %v %v", a.Existing, b.Existing)
	}
	var ce *ConflictError
	if _, err := f.s.CreateTask(f.ctx, u, TaskCreate{ID: id, Title: "Different"}); !errors.As(err, &ce) {
		t.Fatalf("different content with same id: %v", err)
	}
}

// TestUserIsolation: one user can never read or change another user's data.
func TestUserIsolation(t *testing.T) {
	f := newFixture(t)
	alice, mallory := f.user(t, "alice@example.com"), f.user(t, "mallory@example.com")
	p := must(f.s.CreateProject(f.ctx, alice, ProjectCreate{Name: "Secret"}))
	r := must(f.s.CreateTask(f.ctx, alice, TaskCreate{Title: "private", ProjectID: &p.ID}))
	l := must(f.s.CreateLabel(f.ctx, alice, LabelCreate{Name: "mine"}))
	n, _, err := f.s.CreateNote(f.ctx, alice, NoteCreate{Body: "diary"})
	if err != nil {
		t.Fatal(err)
	}
	c := must(f.s.CreateChecklistItem(f.ctx, alice, r.Task.ID, ChecklistCreate{Text: "step"}))

	notFound := func(name string, err error) {
		t.Helper()
		if !errors.Is(err, ErrNotFound) {
			var ve *ValidationError
			if !errors.As(err, &ve) {
				t.Errorf("%s: got %v, want not found", name, err)
			}
		}
	}
	_, err = f.s.GetProject(f.ctx, mallory, p.ID)
	notFound("get project", err)
	_, err = f.s.GetTask(f.ctx, mallory, r.Task.ID)
	notFound("get task", err)
	_, err = f.s.UpdateTask(f.ctx, mallory, r.Task.ID, TaskPatch{Title: Some("pwned")}, 0)
	notFound("update task", err)
	notFound("delete task", f.s.DeleteTask(f.ctx, mallory, r.Task.ID, true))
	_, err = f.s.GetNote(f.ctx, mallory, n.ID)
	notFound("get note", err)
	notFound("delete label", f.s.DeleteLabel(f.ctx, mallory, l.ID))
	_, err = f.s.UpdateChecklistItem(f.ctx, mallory, c.ID, ChecklistPatch{Text: Some("x")})
	notFound("update checklist", err)
	_, err = f.s.CreateTask(f.ctx, mallory, TaskCreate{Title: "sneak", ProjectID: &p.ID})
	notFound("create into foreign project", err)
	_, err = f.s.CreateTask(f.ctx, mallory, TaskCreate{Title: "sneak", Labels: []string{l.ID}})
	notFound("use foreign label", err)

	page := must(f.s.ListTasks(f.ctx, mallory, TaskFilter{}))
	if len(page.Data) != 0 {
		t.Fatal("mallory listed alice's tasks")
	}
	hits := must(f.s.Search(f.ctx, mallory, "private", 10))
	if len(hits) != 0 {
		t.Fatal("mallory found alice's task via search")
	}
	res := must(f.s.Batch(f.ctx, mallory, []BatchOp{{Op: "update", Type: "task", ID: r.Task.ID, Data: json.RawMessage(`{"title":"pwned"}`)}}, false))
	if res.Committed {
		t.Fatal("batch updated a foreign task")
	}
	if got := must(f.s.GetTask(f.ctx, alice, r.Task.ID)); got.Title != "private" {
		t.Fatal("alice's task was modified")
	}
}

func TestBatchUpsertIsIdempotentAndKeepsHistory(t *testing.T) {
	f := newFixture(t)
	u := f.user(t, "a@example.com")
	ops := []BatchOp{
		{Op: "upsert", Type: "project", Ref: "p", Data: json.RawMessage(`{"name":"Homelab","external_source":"vikunja","external_id":"3"}`)},
		{Op: "upsert", Type: "task", Data: json.RawMessage(`{"title":"Old done thing","project_ref":"p","status":"done",
			"completed_at":"2025-03-01T12:00:00Z","created_at":"2025-01-01T08:00:00Z","label_names":["ops"],"create_missing":true,
			"external_source":"vikunja","external_id":"412"}`)},
		{Op: "upsert", Type: "task", Data: json.RawMessage(`{"title":"Open thing","project_ref":"p","external_source":"vikunja","external_id":"413"}`)},
	}
	for run := 0; run < 2; run++ {
		res := must(f.s.Batch(f.ctx, u, ops, false))
		if !res.Committed {
			t.Fatalf("run %d not committed: %+v", run, res.Results)
		}
		want := "created"
		if run == 1 {
			want = "updated"
		}
		if res.Results[1].Status != want {
			t.Fatalf("run %d status %s", run, res.Results[1].Status)
		}
	}
	page := must(f.s.ListTasks(f.ctx, u, TaskFilter{ExternalSource: "vikunja"}))
	if len(page.Data) != 2 {
		t.Fatalf("got %d tasks after two runs, want 2", len(page.Data))
	}
	for _, tk := range page.Data {
		if tk.Title == "Old done thing" {
			if tk.CompletedAt == nil || *tk.CompletedAt != "2025-03-01T12:00:00.000Z" || tk.CreatedAt != "2025-01-01T08:00:00.000Z" {
				t.Fatalf("history lost: completed %v created %s", tk.CompletedAt, tk.CreatedAt)
			}
		}
	}
}

func TestBatchIsAllOrNothingAndDryRun(t *testing.T) {
	f := newFixture(t)
	u := f.user(t, "a@example.com")
	ops := []BatchOp{
		{Op: "create", Type: "task", Data: json.RawMessage(`{"title":"good"}`)},
		{Op: "create", Type: "task", Data: json.RawMessage(`{"title":""}`)},
	}
	res := must(f.s.Batch(f.ctx, u, ops, false))
	if res.Committed || res.Results[1].Error == nil || res.Results[1].Error.Fields[0].Field != "title" {
		t.Fatalf("expected failure on op 1: %+v", res.Results)
	}
	res = must(f.s.Batch(f.ctx, u, ops[:1], true))
	if res.Committed || res.Results[0].Status != "created" {
		t.Fatalf("dry run: %+v", res)
	}
	if page := must(f.s.ListTasks(f.ctx, u, TaskFilter{})); len(page.Data) != 0 {
		t.Fatal("rolled back batches left tasks behind")
	}
}

func TestOptimisticConcurrency(t *testing.T) {
	f := newFixture(t)
	u := f.user(t, "a@example.com")
	r := must(f.s.CreateTask(f.ctx, u, TaskCreate{Title: "v1"}))
	must(f.s.UpdateTask(f.ctx, u, r.Task.ID, TaskPatch{Title: Some("v2")}, 1))
	var pe *PreconditionError
	if _, err := f.s.UpdateTask(f.ctx, u, r.Task.ID, TaskPatch{Title: Some("stale")}, 1); !errors.As(err, &pe) {
		t.Fatalf("stale write: %v", err)
	}
}

func TestMovePlacement(t *testing.T) {
	f := newFixture(t)
	u := f.user(t, "a@example.com")
	a := must(f.s.CreateTask(f.ctx, u, TaskCreate{Title: "a"})).Task
	b := must(f.s.CreateTask(f.ctx, u, TaskCreate{Title: "b"})).Task
	c := must(f.s.CreateTask(f.ctx, u, TaskCreate{Title: "c"})).Task
	must(f.s.MoveTask(f.ctx, u, c.ID, MoveInput{BeforeID: a.ID}))
	must(f.s.MoveTask(f.ctx, u, a.ID, MoveInput{AfterID: b.ID}))
	page := must(f.s.ListTasks(f.ctx, u, TaskFilter{Inbox: true}))
	var order string
	for _, tk := range page.Data {
		order += tk.Title
	}
	if order != "cba" {
		t.Fatalf("order = %s, want cba", order)
	}
}

func TestOverdueUsesUserTimezone(t *testing.T) {
	f := newFixture(t)
	u := f.user(t, "a@example.com")
	// 19:30 UTC is 14:30 in New York: a task due today at 14:00 is overdue, 15:00 is not.
	d := "2026-11-24"
	early, late := "14:00", "15:00"
	t1 := must(f.s.CreateTask(f.ctx, u, TaskCreate{Title: "early", DueDate: &d, DueTime: &early})).Task
	t2 := must(f.s.CreateTask(f.ctx, u, TaskCreate{Title: "late", DueDate: &d, DueTime: &late})).Task
	if !t1.IsOverdue || t2.IsOverdue {
		t.Fatalf("overdue: early=%v late=%v", t1.IsOverdue, t2.IsOverdue)
	}
}

func TestLoginWithTOTPAndRecovery(t *testing.T) {
	f := newFixture(t)
	u := f.user(t, "a@example.com")
	if _, err := f.s.Authenticate(f.ctx, "a@example.com", "wrong password here"); !errors.Is(err, ErrUnauthorized) {
		t.Fatal("wrong password accepted")
	}
	if _, err := f.s.Authenticate(f.ctx, "nobody@example.com", "plum-orbit-kettle-91"); !errors.Is(err, ErrUnauthorized) {
		t.Fatal("unknown user accepted")
	}
	setup := must(f.s.BeginTOTP(f.ctx, u))
	code, _ := auth.TOTPCodeAt(setup.Secret, f.now)
	u = must(f.s.GetUser(f.ctx, u.ID))
	codes := must(f.s.ConfirmTOTP(f.ctx, u, code))
	if len(codes) != 10 {
		t.Fatal("expected 10 recovery codes")
	}
	u = must(f.s.GetUser(f.ctx, u.ID))

	// Same code again (replay) must fail at login, the next step's code works.
	pre := must(f.s.CreateSession(f.ctx, u.ID, true, ClientInfo{}))
	sess, _ := ok2(f.s.LookupSession(f.ctx, pre))
	if _, _, err := f.s.CompleteMFA(f.ctx, sess, code, "", ClientInfo{}); err == nil {
		t.Fatal("replayed TOTP code accepted")
	}
	f.now = f.now.Add(30 * time.Second)
	next, _ := auth.TOTPCodeAt(setup.Secret, f.now)
	sess, _ = ok2(f.s.LookupSession(f.ctx, pre))
	full, _ := ok2(f.s.CompleteMFA(f.ctx, sess, next, "", ClientInfo{}))
	if _, _, err := f.s.LookupSession(f.ctx, pre); err == nil {
		t.Fatal("pre-auth session should be gone after MFA")
	}
	if s2, _, err := f.s.LookupSession(f.ctx, full); err != nil || s2.MFAPending {
		t.Fatal("full session not usable")
	}

	// Recovery code works once.
	pre2 := must(f.s.CreateSession(f.ctx, u.ID, true, ClientInfo{}))
	sess2, _ := ok2(f.s.LookupSession(f.ctx, pre2))
	ok2(f.s.CompleteMFA(f.ctx, sess2, "", codes[0], ClientInfo{}))
	pre3 := must(f.s.CreateSession(f.ctx, u.ID, true, ClientInfo{}))
	sess3, _ := ok2(f.s.LookupSession(f.ctx, pre3))
	if _, _, err := f.s.CompleteMFA(f.ctx, sess3, "", codes[0], ClientInfo{}); err == nil {
		t.Fatal("recovery code reused")
	}
}

func TestPreAuthSessionExpiresAndLimitsAttempts(t *testing.T) {
	f := newFixture(t)
	u := f.user(t, "a@example.com")
	setup := must(f.s.BeginTOTP(f.ctx, u))
	code, _ := auth.TOTPCodeAt(setup.Secret, f.now)
	must(f.s.ConfirmTOTP(f.ctx, must(f.s.GetUser(f.ctx, u.ID)), code))
	pre := must(f.s.CreateSession(f.ctx, u.ID, true, ClientInfo{}))
	for i := 0; i < MaxMFAAttempts; i++ {
		sess, _ := ok2(f.s.LookupSession(f.ctx, pre))
		_, _, _ = f.s.CompleteMFA(f.ctx, sess, "000000", "", ClientInfo{})
	}
	sess, _ := ok2(f.s.LookupSession(f.ctx, pre))
	f.now = f.now.Add(30 * time.Second)
	good, _ := auth.TOTPCodeAt(setup.Secret, f.now)
	if _, _, err := f.s.CompleteMFA(f.ctx, sess, good, "", ClientInfo{}); err == nil {
		t.Fatal("MFA accepted after attempt limit")
	}
	pre2 := must(f.s.CreateSession(f.ctx, u.ID, true, ClientInfo{}))
	f.now = f.now.Add(6 * time.Minute)
	if _, _, err := f.s.LookupSession(f.ctx, pre2); err == nil {
		t.Fatal("pre-auth session outlived 5 minutes")
	}
}

func TestPATScopesAndExpiry(t *testing.T) {
	f := newFixture(t)
	u := f.user(t, "a@example.com")
	exp := f.now.Add(time.Hour).Format(time.RFC3339)
	tok := must(f.s.CreateToken(f.ctx, u, NewToken{Name: "agent", Scopes: []string{"tasks:read", "tasks:write"}, ExpiresAt: &exp}))
	got, scopes, err := f.s.LookupToken(f.ctx, tok.Token)
	if err != nil || got.ID != u.ID || len(scopes) != 2 {
		t.Fatalf("lookup: %v %v", scopes, err)
	}
	f.now = f.now.Add(2 * time.Hour)
	if _, _, err := f.s.LookupToken(f.ctx, tok.Token); err == nil {
		t.Fatal("expired token accepted")
	}
	if _, err := f.s.CreateToken(f.ctx, u, NewToken{Name: "bad", Scopes: []string{"admin"}}); err == nil {
		t.Fatal("unknown scope accepted")
	}
}

func TestSetupAndInvites(t *testing.T) {
	f := newFixture(t)
	tok := must(f.s.SetupToken(f.ctx))
	if _, err := f.s.CompleteSetup(f.ctx, "wrong", NewUser{Email: "x@example.com", Password: "plum-orbit-kettle-91"}); err == nil {
		t.Fatal("bad setup token accepted")
	}
	admin := must(f.s.CompleteSetup(f.ctx, tok, NewUser{Email: "admin@example.com", Password: "plum-orbit-kettle-91"}))
	if !admin.IsAdmin {
		t.Fatal("setup user should be admin")
	}
	if _, err := f.s.CompleteSetup(f.ctx, tok, NewUser{Email: "y@example.com", Password: "plum-orbit-kettle-91"}); err == nil {
		t.Fatal("setup token reusable")
	}
	inv := must(f.s.CreateInvite(f.ctx, admin, "friend@example.com"))
	if _, err := f.s.Register(f.ctx, inv.Token, NewUser{Email: "other@example.com", Password: "plum-orbit-kettle-91"}); err == nil {
		t.Fatal("invite used for a different email")
	}
	must(f.s.Register(f.ctx, inv.Token, NewUser{Email: "friend@example.com", Password: "plum-orbit-kettle-91"}))
	if _, err := f.s.Register(f.ctx, inv.Token, NewUser{Email: "friend2@example.com", Password: "plum-orbit-kettle-91"}); err == nil {
		t.Fatal("invite reused")
	}
	if _, err := f.s.Register(f.ctx, "garbage", NewUser{Email: "z@example.com", Password: "plum-orbit-kettle-91"}); !errors.Is(err, ErrForbidden) {
		t.Fatal("garbage invite accepted")
	}
}

func TestExportRoundTripThroughBatch(t *testing.T) {
	f := newFixture(t)
	src := f.user(t, "src@example.com")
	p := must(f.s.CreateProject(f.ctx, src, ProjectCreate{Name: "Homelab"}))
	must(f.s.CreateTask(f.ctx, src, TaskCreate{QuickAdd: "Back up caddy !high tomorrow #homelab"}))
	_ = p
	e := must(f.s.Export(f.ctx, src))
	if e.Format != "blair.export.v1" || len(e.Tasks) != 1 || len(e.Projects) != 1 {
		t.Fatalf("export: %+v", e)
	}
	// Re-import into another account via batch using names.
	dst := f.user(t, "dst@example.com")
	var ops []BatchOp
	for _, pr := range e.Projects {
		d, _ := json.Marshal(map[string]any{"name": pr.Name, "external_source": "blair", "external_id": pr.ID})
		ops = append(ops, BatchOp{Op: "upsert", Type: "project", Ref: pr.ID, Data: d})
	}
	for _, tk := range e.Tasks {
		d, _ := json.Marshal(map[string]any{"title": tk.Title, "priority": tk.Priority, "due_date": tk.DueDate, "project_ref": *tk.ProjectID,
			"status": tk.Status, "created_at": tk.CreatedAt, "external_source": "blair", "external_id": tk.ID})
		ops = append(ops, BatchOp{Op: "upsert", Type: "task", Data: d})
	}
	res := must(f.s.Batch(f.ctx, dst, ops, false))
	if !res.Committed {
		t.Fatalf("import failed: %+v", res.Results)
	}
	e2 := must(f.s.Export(f.ctx, dst))
	if len(e2.Tasks) != 1 || e2.Tasks[0].Title != "Back up caddy" || e2.Tasks[0].Priority != 3 || *e2.Tasks[0].DueDate != *e.Tasks[0].DueDate {
		t.Fatalf("round trip mismatch: %+v", e2.Tasks[0].Task)
	}
}

// ok2 unwraps a two-value call, panicking (failing the test) on error.
func ok2[A, B any](a A, b B, err error) (A, B) {
	if err != nil {
		panic(err)
	}
	return a, b
}
