package workbench

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type sideProcess struct {
	fakeProcess
	reply func(map[string]any)
}

func (p *sideProcess) Input(v any) error {
	if err := p.fakeProcess.Input(v); err != nil {
		return err
	}
	b, _ := json.Marshal(v)
	var x map[string]any
	json.Unmarshal(b, &x)
	if p.reply != nil {
		go p.reply(x)
	}
	return nil
}
func sideFixture(t *testing.T, agent string) (*Manager, *sideProcess, string) {
	t.Helper()
	m, _ := fixture(t)
	s, _ := m.Create(agent, t.TempDir(), "side-test")
	p := &sideProcess{}
	m.mu.Lock()
	r := m.find(s.ID)
	r.proc = p
	r.ready = true
	r.session.NativeID = "parent"
	r.generation = 1
	m.mu.Unlock()
	return m, p, s.ID
}
func awaitSide(t *testing.T, m *Manager, id, status string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		s, err := m.Side(id)
		if err == nil && s.Status == status {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	s, _ := m.Side(id)
	t.Fatalf("want %s, got %+v", status, s)
}
func TestSideConfirmationHistoryAndIdlePin(t *testing.T) {
	m, _, parent := sideFixture(t, "codex")
	a, err := m.OpenSide(parent)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := m.OpenSide(parent)
	if a.ID != b.ID {
		t.Fatal("duplicate active side")
	}
	m.sideEmit(a.ID, "notice", map[string]any{"text": "durable answer"})
	if err = m.CloseSide(a.ID, false); err == nil {
		t.Fatal("closed without confirmation")
	}
	if err = m.Stop(parent, true); err == nil {
		t.Fatal("slept with side")
	}
	if err = m.CloseSide(a.ID, true); err != nil {
		t.Fatal(err)
	}
	awaitSide(t, m, a.ID, "closed")
	m.mu.Lock()
	if len(m.sides) != 0 {
		t.Fatal("runtime retained")
	}
	if m.find(parent).proc == nil {
		t.Fatal("parent stopped")
	}
	m.mu.Unlock()
	if err = m.CloseSide(a.ID, true); err != nil {
		t.Fatal("close is not idempotent", err)
	}
	m.store.Prune(time.Now().Add(time.Hour), 0)
	history, err := m.sideHistory(a.ID, 0)
	if err != nil || len(history) != 1 {
		t.Fatal("side history lost with cache", history, err)
	}
	if err = m.SendSide(a.ID, "again", "new"); err == nil {
		t.Fatal("closed history resumed")
	}
}
func TestCodexSideRoutingAndCleanup(t *testing.T) {
	m, p, parent := sideFixture(t, "codex")
	m.mu.Lock()
	m.find(parent).turn = true
	m.find(parent).turnID = "parent-turn"
	m.mu.Unlock()
	sc, _ := m.OpenSide(parent)
	p.reply = func(v map[string]any) {
		method := str(v["method"])
		if method == "" {
			return
		}
		result := map[string]any{}
		switch method {
		case "thread/fork":
			result = map[string]any{"thread": map[string]any{"id": "side-native"}}
		case "turn/start":
			m.receive(parent, 1, map[string]any{"method": "turn/started", "params": map[string]any{"threadId": "side-native", "turn": map[string]any{"id": "side-turn"}}})
		case "thread/loaded/list":
			result = map[string]any{"data": []any{"parent"}}
		}
		m.receive(parent, 1, map[string]any{"id": v["id"], "result": result})
	}
	if err := m.SendSide(sc.ID, "question", "once"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for {
		m.mu.Lock()
		turn := m.sides[sc.ID].turns["side-native"]
		m.mu.Unlock()
		if turn != "" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("no side turn")
		}
		time.Sleep(time.Millisecond)
	}
	m.receive(parent, 1, map[string]any{"method": "item/agentMessage/delta", "params": map[string]any{"threadId": "side-native", "itemId": "answer", "delta": "side answer"}})
	m.mu.Lock()
	r := m.find(parent)
	if !r.turn || r.turnID != "parent-turn" || len(r.children) != 0 {
		t.Fatal("side modified parent")
	}
	m.mu.Unlock()
	if err := m.SendSide(sc.ID, "question", "once"); err != nil {
		t.Fatal("duplicate not idempotent", err)
	}
	m.CloseSide(sc.ID, true)
	awaitSide(t, m, sc.ID, "closed")
	history, _ := m.sideHistory(sc.ID, 0)
	if len(history) < 2 {
		t.Fatal("missing transcript")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	methods := map[string]bool{}
	for _, v := range p.in {
		methods[str(v["method"])] = true
	}
	for _, method := range []string{"thread/goal/clear", "turn/interrupt", "thread/backgroundTerminals/clean", "thread/unsubscribe", "thread/loaded/list"} {
		if !methods[method] {
			t.Fatal("cleanup skipped", method)
		}
	}
}
func TestClaudeSideCancelWaitsForNativeCompletion(t *testing.T) {
	m, p, parent := sideFixture(t, "claude")
	sc, _ := m.OpenSide(parent)
	asked := make(chan string, 1)
	cancelled := make(chan string, 1)
	p.reply = func(v map[string]any) {
		if str(obj(v["request"])["subtype"]) == "side_question" {
			asked <- str(v["request_id"])
		}
		if v["type"] == "control_cancel_request" {
			cancelled <- str(v["request_id"])
		}
	}
	m.SendSide(sc.ID, "question", "one")
	key := <-asked
	m.CloseSide(sc.ID, true)
	if got := <-cancelled; got != key {
		t.Fatal("cancelled wrong request")
	}
	s, _ := m.Side(sc.ID)
	if s.Status != "closing" {
		t.Fatal("reported close before cancellation acknowledgement")
	}
	m.receive(parent, 1, map[string]any{"type": "control_response", "response": map[string]any{"request_id": key, "subtype": "error", "error": "Side question cancelled"}})
	awaitSide(t, m, sc.ID, "closed")
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.find(parent).rpc) != 0 || m.find(parent).proc != p {
		t.Fatal("request leaked or parent changed")
	}
}
func TestSideCleanupFailureRetainsOwnership(t *testing.T) {
	m, p, parent := sideFixture(t, "codex")
	sc, _ := m.OpenSide(parent)
	m.mu.Lock()
	m.sides[sc.ID].native = "side-native"
	m.sides[sc.ID].threads["side-native"] = true
	m.sides[sc.ID].generation = 1
	m.mu.Unlock()
	p.reply = func(v map[string]any) {
		m.receive(parent, 1, map[string]any{"id": v["id"], "error": map[string]any{"message": "simulated refusal"}})
	}
	m.CloseSide(sc.ID, true)
	awaitSide(t, m, sc.ID, "cleanup_failed")
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.sides[sc.ID] == nil || m.find(parent).proc != p {
		t.Fatal("lost cleanup ownership")
	}
}
func TestSideStoreRestartIsReadOnly(t *testing.T) {
	m, _, parent := sideFixture(t, "claude")
	sc, _ := m.OpenSide(parent)
	m.sideEmit(sc.ID, "notice", map[string]any{"text": "answer"})
	other, err := OpenStore(m.store.dir)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	var data string
	if err = other.db.QueryRow(`SELECT data FROM side_chats WHERE id=?`, sc.ID).Scan(&data); err != nil {
		t.Fatal(err)
	}
	var saved SideChat
	json.Unmarshal([]byte(data), &saved)
	if saved.Status != "interrupted" {
		t.Fatal(fmt.Sprintf("restart status: %+v", saved))
	}
}

func TestSideNativeEphemeralGoalAndRepeatedCleanup(t *testing.T) {
	m, p, parent := sideFixture(t, "codex")
	p.reply = func(v map[string]any) {
		result := map[string]any{}
		message := map[string]any{"id": v["id"], "result": result}
		if v["method"] == "thread/goal/clear" {
			delete(message, "result")
			message["error"] = map[string]any{"message": "ephemeral thread does not support goals"}
		}
		if v["method"] == "thread/loaded/list" {
			result["data"] = []any{"parent"}
		}
		m.receive(parent, 1, message)
	}
	for i := 0; i < 20; i++ {
		sc, err := m.OpenSide(parent)
		if err != nil {
			t.Fatal(err)
		}
		m.mu.Lock()
		runtime := m.sides[sc.ID]
		runtime.native = "native"
		runtime.threads["native"] = true
		runtime.generation = 1
		m.mu.Unlock()
		if err = m.CloseSide(sc.ID, true); err != nil {
			t.Fatal(err)
		}
		awaitSide(t, m, sc.ID, "closed")
		m.mu.Lock()
		if len(m.sides) != 0 || len(m.find(parent).rpc) != 0 {
			t.Fatal("side resources retained")
		}
		m.mu.Unlock()
	}
}

func TestSideCancelledRequestIsNeverWritten(t *testing.T) {
	m, p, parent := sideFixture(t, "claude")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := m.rpcContext(ctx, parent, "side_question", map[string]any{"question": "late"}, nil); err == nil {
		t.Fatal("cancelled request accepted")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.in) != 0 {
		t.Fatal("cancelled request sent to engine")
	}
}
func TestSideLateForkNotificationIsOwned(t *testing.T) {
	m, _, parent := sideFixture(t, "codex")
	sc, _ := m.OpenSide(parent)
	m.mu.Lock()
	active := m.sides[sc.ID]
	active.generation = 1
	active.Status = "closing"
	m.mu.Unlock()
	m.receive(parent, 1, map[string]any{"method": "thread/started", "params": map[string]any{"thread": map[string]any{"id": "late-fork", "ephemeral": true, "forkedFromId": "parent"}}})
	m.mu.Lock()
	defer m.mu.Unlock()
	if !active.threads["late-fork"] || active.native != "late-fork" {
		t.Fatal("lost late fork ownership")
	}
}

func TestSideCleanupIncludesChildDiscoveredDuringInterrupt(t *testing.T) {
	m, p, parent := sideFixture(t, "codex")
	sc, _ := m.OpenSide(parent)
	m.mu.Lock()
	active := m.sides[sc.ID]
	active.native = "root-side"
	active.threads["root-side"] = true
	active.generation = 1
	active.turns["root-side"] = "turn"
	m.mu.Unlock()
	var childClosed atomic.Bool
	p.reply = func(v map[string]any) {
		method := str(v["method"])
		params := obj(v["params"])
		result := map[string]any{}
		if method == "turn/interrupt" {
			m.receive(parent, 1, map[string]any{"method": "item/completed", "params": map[string]any{"threadId": "root-side", "item": map[string]any{"type": "subAgentActivity", "agentThreadId": "late-child"}}})
		}
		if method == "thread/unsubscribe" && params["threadId"] == "late-child" {
			childClosed.Store(true)
		}
		if method == "thread/loaded/list" {
			result["data"] = []any{"parent"}
			if !childClosed.Load() {
				result["data"] = []any{"parent", "late-child"}
			}
		}
		m.receive(parent, 1, map[string]any{"id": v["id"], "result": result})
	}
	m.CloseSide(sc.ID, true)
	awaitSide(t, m, sc.ID, "closed")
	if !childClosed.Load() {
		t.Fatal("closed without reclaiming late child")
	}
}

// A side thread that starts a multi-agent v2 sub-agent: Codex refuses any
// request sent to the sub-agent itself, so closing must leave it to Codex.
func TestSideCleanupLeavesV2SubAgentsToCodex(t *testing.T) {
	m, p, parent := sideFixture(t, "codex")
	sc, _ := m.OpenSide(parent)
	m.mu.Lock()
	active := m.sides[sc.ID]
	active.native = "root-side"
	active.threads["root-side"] = true
	active.generation = 1
	active.turns["root-side"] = "turn"
	m.mu.Unlock()
	m.receive(parent, 1, map[string]any{"method": "item/completed", "params": map[string]any{"threadId": "root-side", "item": map[string]any{"type": "subAgentActivity", "agentThreadId": "v2-child"}}})
	var rootSteps sync.Map
	p.reply = func(v map[string]any) {
		method := str(v["method"])
		params := obj(v["params"])
		if params["threadId"] == "v2-child" {
			// Interrupting the root reports the child again.
			m.receive(parent, 1, map[string]any{"method": "item/completed", "params": map[string]any{"threadId": "root-side", "item": map[string]any{"type": "subAgentActivity", "agentThreadId": "v2-child"}}})
			m.receive(parent, 1, map[string]any{"id": v["id"], "error": map[string]any{"code": -32600, "message": "direct app-server input is not allowed for multi-agent v2 sub-agents"}})
			return
		}
		if params["threadId"] == "root-side" {
			rootSteps.Store(method, true)
		}
		result := map[string]any{}
		if method == "thread/loaded/list" {
			// Codex may keep the sub-agent loaded for a while.
			result["data"] = []any{"parent", "v2-child"}
		}
		m.receive(parent, 1, map[string]any{"id": v["id"], "result": result})
	}
	if err := m.CloseSide(sc.ID, true); err != nil {
		t.Fatal(err)
	}
	awaitSide(t, m, sc.ID, "closed")
	for _, method := range []string{"turn/interrupt", "thread/unsubscribe"} {
		if _, ok := rootSteps.Load(method); !ok {
			t.Fatal("root thread missed", method)
		}
	}
}
