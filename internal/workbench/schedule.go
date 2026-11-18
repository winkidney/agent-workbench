package workbench

import (
	"regexp"
	"sort"
	"strconv"
	"time"
)

// Scheduled work: Claude Code's session crons (CronCreate, /loop and
// ScheduleWakeup) are timers inside its own process. No process shows them
// and its background task set leaves them out, so they are read from the
// tool calls that make and cancel them, and keep an idle engine awake until
// they are due: sleep would end the process and its timers with it.
//
//   - ScheduleWakeup: one pending wakeup at a time (a new one replaces it),
//     at scheduledFor in the tool's result. Kept until shortly after, since
//     a fire can come late or not at all.
//   - CronCreate: a one-shot task until shortly after its next run; a
//     recurring job until CronDelete or its expiry (Claude's own).
//   - CronDelete cancels; a fired one-shot task deletes itself.

const (
	// After a wakeup's or one-shot task's time: how long it still keeps the
	// engine awake for a fire that is late.
	scheduleGrace = 10 * time.Minute
	// A recurring job Claude did not say when it expires.
	cronExpiry = 7 * 24 * time.Hour
	// A one-shot task with no known run time.
	oneShotUnknown = 24 * time.Hour
)

// schedule is one timer that will wake the session.
type schedule struct {
	id        string
	kind      string // wakeup | cron
	recurring bool
	label     string
	created   time.Time
	next      time.Time
	until     time.Time
}

var (
	cronCreated  = regexp.MustCompile(`Scheduled (recurring job|one-shot task) (\S+) \(([^)]*)\)`)
	cronExpires  = regexp.MustCompile(`Auto-expires after (\d+) days`)
	cronCanceled = regexp.MustCompile(`Cancelled job (\S+?)\.`)
	wakeIn       = regexp.MustCompile(`Next wakeup scheduled for .*?\(in (\d+)s\)`)
)

// trackSchedules reads Claude's scheduling tool calls and their results
// (m.mu held).
func trackSchedules(r *runtime, v map[string]any, now time.Time) {
	switch str(v["type"]) {
	case "assistant":
		for _, raw := range asList(obj(v["message"])["content"]) {
			b := obj(raw)
			if str(b["type"]) != "tool_use" {
				continue
			}
			switch name := str(b["name"]); name {
			case "ScheduleWakeup", "CronCreate", "CronDelete":
				if r.scheduleCalls == nil {
					r.scheduleCalls = map[string]scheduleCall{}
				}
				r.scheduleCalls[str(b["id"])] = scheduleCall{name: name, input: obj(b["input"])}
			}
		}
	case "user":
		for _, raw := range asList(obj(v["message"])["content"]) {
			b := obj(raw)
			if str(b["type"]) != "tool_result" {
				continue
			}
			id := str(b["tool_use_id"])
			call, ok := r.scheduleCalls[id]
			if !ok {
				continue
			}
			delete(r.scheduleCalls, id)
			if b["is_error"] == true {
				continue
			}
			scheduled(r, call, obj(v["tool_use_result"]), resultText(b["content"]), now)
		}
	case "system":
		if str(v["subtype"]) == "scheduled_task_fire" {
			// A wakeup that came due has fired; one-shot tasks then delete
			// themselves (whichever was due).
			for id, s := range r.schedules {
				if !s.recurring && !s.next.IsZero() && !s.next.After(now.Add(time.Minute)) {
					delete(r.schedules, id)
				}
			}
		}
	}
}

type scheduleCall struct {
	name  string
	input map[string]any
}

func scheduled(r *runtime, call scheduleCall, result map[string]any, text string, now time.Time) {
	if r.schedules == nil {
		r.schedules = map[string]*schedule{}
	}
	switch call.name {
	case "ScheduleWakeup":
		at := msTime(result["scheduledFor"])
		if at.IsZero() {
			if m := wakeIn.FindStringSubmatch(text); m != nil {
				n, _ := strconv.Atoi(m[1])
				at = now.Add(time.Duration(n) * time.Second)
			}
		}
		if at.IsZero() {
			if n := num(call.input["delaySeconds"]); n > 0 {
				at = now.Add(time.Duration(n) * time.Second)
			}
		}
		if at.IsZero() {
			return
		}
		label := str(call.input["reason"])
		if label == "" {
			label = "/loop wakeup"
		}
		r.schedules["wakeup"] = &schedule{id: "wakeup", kind: "wakeup", label: label, created: now, next: at, until: at.Add(scheduleGrace)}
	case "CronCreate":
		id := str(result["id"])
		recurring := result["recurring"] == true
		human := str(result["humanSchedule"])
		if m := cronCreated.FindStringSubmatch(text); m != nil {
			if id == "" {
				id = m[2]
			}
			if _, ok := result["recurring"]; !ok {
				recurring = m[1] == "recurring job"
			}
			if human == "" {
				human = m[3]
			}
		}
		if id == "" {
			return
		}
		s := &schedule{id: id, kind: "cron", recurring: recurring, created: now, next: msTime(result["nextFireAt"])}
		s.label = human
		if p := str(call.input["prompt"]); p != "" {
			if s.label != "" {
				s.label += ": "
			}
			s.label += clip(p, 80)
		}
		switch {
		case recurring:
			s.until = now.Add(cronExpiry)
			if m := cronExpires.FindStringSubmatch(text); m != nil {
				if days, _ := strconv.Atoi(m[1]); days > 0 {
					s.until = now.Add(time.Duration(days) * 24 * time.Hour)
				}
			}
		case !s.next.IsZero():
			s.until = s.next.Add(scheduleGrace)
		default:
			s.until = now.Add(oneShotUnknown)
		}
		r.schedules[id] = s
	case "CronDelete":
		id := str(call.input["id"])
		if m := cronCanceled.FindStringSubmatch(text); m != nil {
			id = m[1]
		}
		delete(r.schedules, id)
	}
}

// scheduledWork is whether a timer will wake the session (m.mu held): it
// keeps the idle engine awake. Those past their time are let go.
func scheduledWork(r *runtime, now time.Time) bool {
	for id, s := range r.schedules {
		if !now.Before(s.until) {
			delete(r.schedules, id)
		}
	}
	return len(r.schedules) > 0
}

// schedulesOf lists them for the page, the next due first.
func schedulesOf(r *runtime, now time.Time) []BackgroundTask {
	scheduledWork(r, now)
	out := []BackgroundTask{}
	for _, s := range r.schedules {
		t := BackgroundTask{ID: "schedule:" + s.id, Source: "claude", Kind: "schedule", Description: s.label, Status: "scheduled"}
		if !s.next.IsZero() {
			t.Next = s.next.UTC().Format(time.RFC3339)
		}
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Next != out[j].Next {
			return out[i].Next != "" && (out[j].Next == "" || out[i].Next < out[j].Next)
		}
		return out[i].ID < out[j].ID
	})
	return out
}

func msTime(v any) time.Time {
	if n := num(v); n > 0 {
		return time.UnixMilli(int64(n))
	}
	return time.Time{}
}

func resultText(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	text := ""
	for _, raw := range asList(v) {
		text += str(obj(raw)["text"])
	}
	return text
}
