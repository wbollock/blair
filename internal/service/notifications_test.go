package service

import (
	"strings"
	"testing"
	"time"
)

func TestRemindersFireExactlyOnceAndRearm(t *testing.T) {
	f := newFixture(t)
	a, b := f.user(t, "a@example.com"), f.user(t, "b@example.com")
	at := "2026-11-24T20:00:00Z"
	r := must(f.s.CreateTask(f.ctx, a, TaskCreate{Title: "Call the plumber", RemindAt: &at}))
	done := must(f.s.CreateTask(f.ctx, a, TaskCreate{Title: "Already done", RemindAt: &at}))
	must(f.s.UpdateTask(f.ctx, a, done.Task.ID, TaskPatch{Status: Some("done")}, 0))

	if n := must(f.s.FireReminders(f.ctx)); n != 0 {
		t.Fatalf("fired %d before remind_at", n)
	}
	f.now = f.now.Add(time.Hour)
	if n := must(f.s.FireReminders(f.ctx)); n != 1 {
		t.Fatalf("fired %d, want 1 (completed tasks never fire)", n)
	}
	if n := must(f.s.FireReminders(f.ctx)); n != 0 {
		t.Fatalf("fired again: %d", n)
	}
	page := must(f.s.ListNotifications(f.ctx, a, true, 0))
	if page.UnreadCount != 1 || page.Data[0].Kind != "reminder" || *page.Data[0].TaskTitle != "Call the plumber" {
		t.Fatalf("notifications = %+v", page)
	}
	if other := must(f.s.ListNotifications(f.ctx, b, false, 0)); len(other.Data) != 0 {
		t.Fatalf("another user sees %d notifications", len(other.Data))
	}

	// An unrelated edit keeps the fired state; a new time re-arms it.
	must(f.s.UpdateTask(f.ctx, a, r.Task.ID, TaskPatch{Priority: Some(2)}, 0))
	if n := must(f.s.FireReminders(f.ctx)); n != 0 {
		t.Fatalf("an unrelated edit re-fired the reminder")
	}
	later := "2026-11-24T21:00:00Z"
	tk := must(f.s.UpdateTask(f.ctx, a, r.Task.ID, TaskPatch{RemindAt: Some(later)}, 0))
	if *tk.RemindAt != "2026-11-24T21:00:00.000Z" {
		t.Fatalf("remind_at = %v", *tk.RemindAt)
	}
	f.now = f.now.Add(time.Hour)
	if n := must(f.s.FireReminders(f.ctx)); n != 1 {
		t.Fatalf("re-armed reminder fired %d times", n)
	}

	if err := f.s.MarkNotificationsRead(f.ctx, a, nil); err != nil {
		t.Fatal(err)
	}
	if p := must(f.s.ListNotifications(f.ctx, a, true, 0)); p.UnreadCount != 0 {
		t.Fatalf("unread after mark-all = %d", p.UnreadCount)
	}
}

func TestSecurityNotifications(t *testing.T) {
	f := newFixture(t)
	u := f.user(t, "a@example.com")
	ff := ClientInfo{IP: "203.0.113.9", UserAgent: "Mozilla/5.0 (X11; Linux x86_64; rv:150.0) Gecko/20100101 Firefox/150.0"}
	f.s.Audit(f.ctx, u.ID, "login.success", ff, nil)
	f.s.Audit(f.ctx, u.ID, "login.success", ff, nil) // same browser again: no new notification
	f.s.Audit(f.ctx, u.ID, "login.mfa_failure", ff, nil)
	f.s.Audit(f.ctx, u.ID, "logout", ff, nil) // not notable
	page := must(f.s.ListNotifications(f.ctx, u, false, 0))
	if len(page.Data) != 2 {
		t.Fatalf("got %d notifications: %+v", len(page.Data), page.Data)
	}
	if !strings.Contains(page.Data[1].Text, "Firefox on Linux (203.0.113.9)") || page.Data[0].Kind != "security.login.mfa_failure" {
		t.Fatalf("notifications = %+v", page.Data)
	}
}
