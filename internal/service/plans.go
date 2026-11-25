package service

import (
	"context"
	"database/sql"
	"slices"
	"time"
)

// Plan is the set of tasks picked to work on one day (ADR-0014). It is
// separate from due dates: a due date is when a task must happen, a plan is
// what you chose to do.
type Plan struct {
	Date    string   `json:"date"`
	TaskIDs []string `json:"task_ids"`
	Tasks   []*Task  `json:"tasks"`
}

// Slipping is an open task that was planned on several past days without being finished.
type Slipping struct {
	Task         *Task  `json:"task"`
	PlannedDays  int    `json:"planned_days"`
	FirstPlanned string `json:"first_planned"`
	LastPlanned  string `json:"last_planned"`
}

// PlanDate resolves "today" (in the user's timezone) or validates YYYY-MM-DD.
func (s *Service) PlanDate(u *User, date string) (string, error) {
	if date == "today" {
		return s.clock().In(u.Location()).Format(time.DateOnly), nil
	}
	if !validDate(date) {
		return "", invalid("date", "invalid", "use YYYY-MM-DD or today")
	}
	return date, nil
}

func (s *Service) GetPlan(ctx context.Context, u *User, date string) (*Plan, error) {
	return s.plan(ctx, s.db.R, u, date)
}

func (s *Service) plan(ctx context.Context, q querier, u *User, date string) (*Plan, error) {
	p := &Plan{Date: date, TaskIDs: []string{}, Tasks: []*Task{}}
	rows, err := q.QueryContext(ctx, `SELECT task_id FROM plan_items WHERE owner_id = ? AND plan_date = ? ORDER BY position, added_at`, u.ID, date)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		p.TaskIDs = append(p.TaskIDs, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for _, id := range p.TaskIDs {
		t, err := s.task(ctx, q, u, id)
		if err != nil {
			return nil, err
		}
		p.Tasks = append(p.Tasks, t)
	}
	return p, nil
}

func (s *Service) checkPlanTasks(ctx context.Context, q querier, u *User, ids []string) error {
	if len(ids) > 200 {
		return invalid("task_ids", "too_many", "at most 200 tasks per day")
	}
	for _, id := range ids {
		var n int
		if err := q.QueryRowContext(ctx, `SELECT count(*) FROM tasks WHERE owner_id = ? AND id = ? AND deleted_at IS NULL`, u.ID, id).Scan(&n); err != nil {
			return err
		}
		if n == 0 {
			return invalid("task_ids", "not_found", "unknown task "+id)
		}
	}
	return nil
}

// SetPlan replaces a day's plan with ids, in that order.
func (s *Service) SetPlan(ctx context.Context, u *User, date string, ids []string) (p *Plan, err error) {
	ids = dedupe(ids)
	err = s.db.Tx(ctx, func(tx *sql.Tx) error {
		if err := s.checkPlanTasks(ctx, tx, u, ids); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM plan_items WHERE owner_id = ? AND plan_date = ? AND task_id NOT IN (`+placeholders(max(len(ids), 1))+`)`,
			append([]any{u.ID, date}, anyIDs(ids)...)...); err != nil {
			return err
		}
		now := s.now()
		for i, id := range ids {
			if _, err := tx.ExecContext(ctx, `INSERT INTO plan_items (owner_id, plan_date, task_id, position, added_at) VALUES (?,?,?,?,?)
				ON CONFLICT DO UPDATE SET position = excluded.position`, u.ID, date, id, i, now); err != nil {
				return err
			}
		}
		p, err = s.plan(ctx, tx, u, date)
		return err
	})
	return p, err
}

// AddToPlan appends tasks to a day's plan; tasks already in it keep their place.
func (s *Service) AddToPlan(ctx context.Context, u *User, date string, ids []string) (p *Plan, err error) {
	ids = dedupe(ids)
	err = s.db.Tx(ctx, func(tx *sql.Tx) error {
		if err := s.checkPlanTasks(ctx, tx, u, ids); err != nil {
			return err
		}
		var next int
		if err := tx.QueryRowContext(ctx, `SELECT coalesce(max(position) + 1, 0) FROM plan_items WHERE owner_id = ? AND plan_date = ?`, u.ID, date).Scan(&next); err != nil {
			return err
		}
		now := s.now()
		for _, id := range ids {
			res, err := tx.ExecContext(ctx, `INSERT INTO plan_items (owner_id, plan_date, task_id, position, added_at) VALUES (?,?,?,?,?) ON CONFLICT DO NOTHING`,
				u.ID, date, id, next, now)
			if err != nil {
				return err
			}
			if n, _ := res.RowsAffected(); n > 0 {
				next++
			}
		}
		p, err = s.plan(ctx, tx, u, date)
		return err
	})
	return p, err
}

func (s *Service) RemoveFromPlan(ctx context.Context, u *User, date, taskID string) error {
	_, err := s.db.W.ExecContext(ctx, `DELETE FROM plan_items WHERE owner_id = ? AND plan_date = ? AND task_id = ?`, u.ID, date, taskID)
	return err
}

// SlippingTasks lists open tasks planned on at least minDays days before today
// without being completed on those days. Completions of repeating tasks count,
// so a daily chore done each day it is planned never shows up.
func (s *Service) SlippingTasks(ctx context.Context, u *User, minDays, sinceDays int) ([]Slipping, error) {
	ctx, sp := span(ctx, "SlippingTasks")
	defer sp.End()
	loc := u.Location()
	today := s.clock().In(loc)
	since := today.AddDate(0, 0, -sinceDays).Format(time.DateOnly)
	rows, err := s.db.R.QueryContext(ctx, `SELECT p.task_id, p.plan_date FROM plan_items p JOIN tasks t ON t.id = p.task_id
		WHERE p.owner_id = ? AND p.plan_date >= ? AND p.plan_date < ? AND t.deleted_at IS NULL AND t.status IN ('todo','doing','waiting')
		ORDER BY p.plan_date`, u.ID, since, today.Format(time.DateOnly))
	if err != nil {
		return nil, err
	}
	planned := map[string][]string{}
	var order []string
	for rows.Next() {
		var id, day string
		if err := rows.Scan(&id, &day); err != nil {
			rows.Close()
			return nil, err
		}
		if _, ok := planned[id]; !ok {
			order = append(order, id)
		}
		planned[id] = append(planned[id], day)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	out := []Slipping{}
	for _, id := range order {
		doneOn := map[string]bool{}
		crows, err := s.db.R.QueryContext(ctx, `SELECT completed_at FROM task_completions WHERE owner_id = ? AND task_id = ?`, u.ID, id)
		if err != nil {
			return nil, err
		}
		for crows.Next() {
			var at string
			if crows.Scan(&at) == nil {
				if t, err := time.Parse(time.RFC3339Nano, at); err == nil {
					doneOn[t.In(loc).Format(time.DateOnly)] = true
				}
			}
		}
		crows.Close()
		var missed []string
		for _, day := range planned[id] {
			if !doneOn[day] {
				missed = append(missed, day)
			}
		}
		if len(missed) < minDays {
			continue
		}
		t, err := s.task(ctx, s.db.R, u, id)
		if err != nil {
			return nil, err
		}
		out = append(out, Slipping{Task: t, PlannedDays: len(missed), FirstPlanned: missed[0], LastPlanned: missed[len(missed)-1]})
	}
	slices.SortStableFunc(out, func(a, b Slipping) int { return b.PlannedDays - a.PlannedDays })
	return out, nil
}

func dedupe(ids []string) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if !slices.Contains(out, id) {
			out = append(out, id)
		}
	}
	return out
}

func anyIDs(ids []string) []any {
	if len(ids) == 0 {
		return []any{""}
	}
	out := make([]any, len(ids))
	for i, id := range ids {
		out[i] = id
	}
	return out
}
