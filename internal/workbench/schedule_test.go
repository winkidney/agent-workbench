package workbench

import (
	"testing"
	"time"
)

func toolUse(id, name string, input map[string]any) map[string]any {
	return map[string]any{"type": "assistant", "message": map[string]any{"id": "m-" + id, "role": "assistant", "content": []any{map[string]any{"type": "tool_use", "id": id, "name": name, "input": input}}}}
}

// turnEnds is the record that ends a Claude turn.
func turnEnds() map[string]any {
	return map[string]any{"type": "result", "subtype": "success", "usage": map[string]any{}}
}

func toolResult(id, text string, structured map[string]any, isError bool) map[string]any {
	v := map[string]any{"type": "user", "message": map[string]any{"role": "user", "content": []any{map[string]any{"type": "tool_result", "tool_use_id": id, "content": text, "is_error": isError}}}}
	if structured != nil {
		v["tool_use_result"] = structured
	}
	return v
}

// A /loop wakeup is a timer inside Claude's process: the idle engine stays
// awake for it (and the Sleep button asks), the page lists it with its time,
// and once it is past due the engine sleeps as usual.
func TestScheduledWakeupKeepsClaudeAwake(t *testing.T) {
	m, _, r := claudeReady(t)
	id := r.session.ID
	at := time.Now().Add(20 * time.Minute).Truncate(time.Millisecond)
	m.receive(id, r.generation, toolUse("w1", "ScheduleWakeup", map[string]any{"delaySeconds": 1200.0, "reason": "check the CI run", "prompt": "/loop check CI"}))
	m.receive(id, r.generation, toolResult("w1", "Next wakeup scheduled for 14:05 (in 1200s). End the turn now.", map[string]any{"scheduledFor": float64(at.UnixMilli())}, false))
	m.receive(id, r.generation, turnEnds())
	if !sweptAwake(m, r, 10*time.Minute) {
		t.Fatal("slept with a wakeup pending")
	}
	if err := m.Stop(id, true); err == nil {
		t.Fatal("the Sleep button slept with a wakeup pending")
	}
	m.mu.Lock()
	list := tasksOf(r)
	m.mu.Unlock()
	last := list[len(list)-1]
	if last.Kind != "schedule" || last.Status != "scheduled" || last.Description != "check the CI run" || last.Next != at.UTC().Format(time.RFC3339) {
		t.Fatalf("listed: %+v", last)
	}
	// Past its time and the grace for a late fire: no longer kept.
	m.mu.Lock()
	r.schedules["wakeup"].until = time.Now().Add(-time.Second)
	m.mu.Unlock()
	if sweptAwake(m, r, 10*time.Minute) {
		t.Fatal("stayed awake past the wakeup")
	}
}

// A recurring job keeps the engine awake until it is cancelled; read from
// the result's text when there is no structured result.
func TestRecurringCronKeepsClaudeAwakeUntilDeleted(t *testing.T) {
	m, _, r := claudeReady(t)
	id := r.session.ID
	m.receive(id, r.generation, toolUse("c1", "CronCreate", map[string]any{"cron": "*/10 * * * *", "prompt": "check the deploy", "recurring": true}))
	m.receive(id, r.generation, toolResult("c1", "Scheduled recurring job a1b2c3 (every 10 minutes). Next run: 14:10. Session-only (not written to disk, dies when Claude exits). Auto-expires after 3 days. Use CronDelete to cancel sooner.", nil, false))
	m.receive(id, r.generation, turnEnds())
	m.mu.Lock()
	s := r.schedules["a1b2c3"]
	m.mu.Unlock()
	if s == nil || !s.recurring || s.until.Sub(time.Now()) < 71*time.Hour || s.label != "every 10 minutes: check the deploy" {
		t.Fatalf("job: %+v", s)
	}
	if !sweptAwake(m, r, 50*time.Minute) {
		t.Fatal("slept with a recurring job")
	}
	// A refused call schedules nothing.
	m.receive(id, r.generation, toolUse("c2", "CronCreate", map[string]any{"cron": "bad"}))
	m.receive(id, r.generation, toolResult("c2", "Invalid cron expression", nil, true))
	m.receive(id, r.generation, turnEnds())
	m.receive(id, r.generation, toolUse("d1", "CronDelete", map[string]any{"id": "a1b2c3"}))
	m.receive(id, r.generation, toolResult("d1", "Cancelled job a1b2c3.", nil, false))
	m.receive(id, r.generation, turnEnds())
	if sweptAwake(m, r, 10*time.Minute) {
		t.Fatal("stayed awake after the job was cancelled")
	}
}

// A one-shot task is kept until it fires (it then deletes itself), and the
// engine ending takes every timer with it.
func TestOneShotCronFiresOnceAndTimersEndWithTheEngine(t *testing.T) {
	m, _, r := claudeReady(t)
	id := r.session.ID
	next := time.Now().Add(30 * time.Second)
	m.receive(id, r.generation, toolUse("o1", "CronCreate", map[string]any{"cron": "30 14 18 11 *", "prompt": "remind me", "recurring": false}))
	m.receive(id, r.generation, toolResult("o1", "Scheduled one-shot task z9 (once at 14:30).", map[string]any{"id": "z9", "recurring": false, "humanSchedule": "once at 14:30", "nextFireAt": float64(next.UnixMilli())}, false))
	m.receive(id, r.generation, turnEnds())
	if !sweptAwake(m, r, 10*time.Minute) {
		t.Fatal("slept before the one-shot task fired")
	}
	m.receive(id, r.generation, map[string]any{"type": "system", "subtype": "scheduled_task_fire", "content": "remind me"})
	m.mu.Lock()
	left := len(r.schedules)
	m.mu.Unlock()
	if left != 0 {
		t.Fatalf("a fired one-shot task is still kept: %d", left)
	}
	// Timers die with the process.
	m.receive(id, r.generation, toolUse("o2", "CronCreate", map[string]any{"cron": "*/5 * * * *", "recurring": true}))
	m.receive(id, r.generation, toolResult("o2", "Scheduled recurring job q1 (every 5 minutes).", nil, false))
	m.receive(id, r.generation, turnEnds())
	if err := m.SleepStoppingBackground(id); err != nil {
		t.Fatal(err)
	}
	m.mu.Lock()
	gone := r.schedules == nil && r.proc == nil
	m.mu.Unlock()
	if !gone {
		t.Fatal("timers outlived the engine")
	}
}
