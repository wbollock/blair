package service

import (
	"slices"
	"testing"
	"time"
)

func TestDailyPlanAndSlipping(t *testing.T) {
	f := newFixture(t) // 2026-11-24 in New York
	u, other := f.user(t, "a@example.com"), f.user(t, "b@example.com")
	mk := func(in TaskCreate) *Task { return must(f.s.CreateTask(f.ctx, u, in)).Task }
	report, review, chore := mk(TaskCreate{Title: "Write report"}), mk(TaskCreate{Title: "Review PR"}), mk(TaskCreate{QuickAdd: "Stretch every day"})

	if d := must(f.s.PlanDate(u, "today")); d != "2026-11-24" {
		t.Fatalf("today = %s", d)
	}
	p := must(f.s.AddToPlan(f.ctx, u, "2026-11-24", []string{report.ID, review.ID, report.ID}))
	if !slices.Equal(p.TaskIDs, []string{report.ID, review.ID}) || len(p.Tasks) != 2 {
		t.Fatalf("plan = %v", p.TaskIDs)
	}
	p = must(f.s.SetPlan(f.ctx, u, "2026-11-24", []string{review.ID, chore.ID}))
	if !slices.Equal(p.TaskIDs, []string{review.ID, chore.ID}) {
		t.Fatalf("replaced plan = %v", p.TaskIDs)
	}
	if _, err := f.s.AddToPlan(f.ctx, other, "2026-11-24", []string{report.ID}); err == nil {
		t.Fatal("another user planned someone else's task")
	}
	if err := f.s.RemoveFromPlan(f.ctx, u, "2026-11-24", chore.ID); err != nil {
		t.Fatal(err)
	}

	// Plan the report and the chore for three days, finishing only the chore.
	for _, day := range []string{"2026-11-24", "2026-11-25", "2026-11-26"} {
		must(f.s.AddToPlan(f.ctx, u, day, []string{report.ID, chore.ID}))
		f.now, _ = time.Parse(time.RFC3339, day+"T17:00:00Z")
		must(f.s.UpdateTask(f.ctx, u, chore.ID, TaskPatch{Status: Some("done")}, 0))
	}
	f.now = f.now.Add(24 * time.Hour) // 2026-11-27
	slip := must(f.s.SlippingTasks(f.ctx, u, 3, 30))
	if len(slip) != 1 || slip[0].Task.ID != report.ID || slip[0].PlannedDays != 3 || slip[0].FirstPlanned != "2026-11-24" || slip[0].LastPlanned != "2026-11-26" {
		t.Fatalf("slipping = %+v", slip)
	}
	// Finishing it takes it off the list.
	must(f.s.UpdateTask(f.ctx, u, report.ID, TaskPatch{Status: Some("done")}, 0))
	if slip := must(f.s.SlippingTasks(f.ctx, u, 3, 30)); len(slip) != 0 {
		t.Fatalf("done task still slipping: %+v", slip)
	}
	if e := must(f.s.Export(f.ctx, u)); len(e.Plans) != 7 || len(e.Completions) != 3 {
		t.Fatalf("export plans=%d completions=%d", len(e.Plans), len(e.Completions))
	}
}
