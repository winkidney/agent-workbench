package workbench

import (
	"strings"
	"testing"
)

func TestReportOf(t *testing.T) {
	for _, c := range []struct {
		reply, text string
		forYou      bool
	}{
		{"Two loops are due today:\n- renew domain\n- reply to Ann", "Two loops are due today:\n- renew domain\n- reply to Ann", true},
		{"Nothing due today.\n\n[Nothing for you]", "Nothing due today.", false},
		{"今天沒有到期事項。\n**[nothing for you]**。", "今天沒有到期事項。", false},
		{"`[Nothing for you]`", "", false},
		{"   ", "", false},
		// The mark only counts as the last line.
		{"[Nothing for you]\nBut the build is broken.", "[Nothing for you]\nBut the build is broken.", true},
	} {
		text, forYou := reportOf(c.reply)
		if text != c.text || forYou != c.forYou {
			t.Errorf("reportOf(%q) = %q, %v; want %q, %v", c.reply, text, forYou, c.text, c.forYou)
		}
	}
	long, _ := reportOf(strings.Repeat("界", routineReportMax))
	if len(long) > routineReportMax+len("…") || !strings.HasSuffix(long, "…") || !strings.HasPrefix(long, "界") {
		t.Fatalf("long report: %d bytes", len(long))
	}
}

// A message routine's reply is its report: shown to the user until seen,
// the newest in place of older ones, and nothing when the agent says it has
// nothing for them.
func TestRoutineReportsWaitUntilSeen(t *testing.T) {
	m, root := assistantsWithGit(t)
	laneAssistant(t, m, root, "")
	fake := &sessions{}
	m.as.foreground = fake.send
	id := fileRoutineID("mail", "Daily summary")
	report := func() *RoutineRun {
		t.Helper()
		list, err := m.Routines()
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range list {
			if r.ID == id {
				return r.Report
			}
		}
		t.Fatal("routine not listed")
		return nil
	}

	first := runNow(t, m, id, "sent")
	fake.waitFor(t, 1)
	if !strings.HasSuffix(fake.sent[0].text, reportNote) {
		t.Fatalf("message: %q", fake.sent[0].text)
	}
	m.routineTurnEnded(first.ID, "s1", false, "", "Nothing new.\n[Nothing for you]")
	if v := waitRun(t, m, first.ID, "done"); v.ForYou || v.Notify || v.Report != "Nothing new." {
		t.Fatalf("quiet run: %+v", v)
	}
	if r := report(); r != nil {
		t.Fatalf("quiet report listed: %+v", r)
	}

	second := runNow(t, m, id, "sent")
	m.routineTurnEnded(second.ID, "s2", false, "", "Ann asked about the invoice.")
	if v := waitRun(t, m, second.ID, "done"); !v.ForYou || !v.Notify {
		t.Fatalf("report run: %+v", v)
	}
	third := runNow(t, m, id, "sent")
	m.routineTurnEnded(third.ID, "s3", false, "", "The invoice is overdue.")
	waitRun(t, m, third.ID, "done")
	if r := report(); r == nil || r.ID != third.ID || r.Report != "The invoice is overdue." || r.Session != "s3" {
		t.Fatalf("listed report: %+v", r)
	}
	// The older one gave way, and does not notify again.
	if v, _ := m.store.run(second.ID); v.Seen == "" || v.Notify {
		t.Fatalf("older report: %+v", v)
	}

	if err := m.SeeReports(id); err != nil {
		t.Fatal(err)
	}
	if r := report(); r != nil {
		t.Fatalf("seen report listed: %+v", r)
	}
	if v, _ := m.store.run(third.ID); v.Seen == "" || v.Notify || !v.ForYou {
		t.Fatalf("seen run: %+v", v)
	}
}

func TestTrackReplyKeepsTheTurnsLastMessage(t *testing.T) {
	r := &runtime{reply: "old"}
	trackReply(r, "turn/started", nil)
	trackReply(r, "item/completed", map[string]any{"item": map[string]any{"type": "agentMessage", "text": "first"}})
	trackReply(r, "item/completed", map[string]any{"item": map[string]any{"type": "commandExecution"}})
	trackReply(r, "item/completed", map[string]any{"item": map[string]any{"type": "agentMessage", "text": "last"}})
	if r.reply != "last" {
		t.Fatalf("reply %q", r.reply)
	}
}
