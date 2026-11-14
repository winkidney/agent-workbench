package workbench

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestUsageCommandIsLocalForBothAgents(t *testing.T) {
	for _, agent := range []string{"claude", "codex"} {
		cat := buildCatalog(agent, metadata{Commands: []any{map[string]any{"name": "usage"}}})
		count := 0
		for _, c := range cat.Commands {
			if c.Name == "usage" {
				count++
				if c.Action != "status" || c.Source != "workbench" {
					t.Fatalf("%s: %+v", agent, c)
				}
			}
		}
		if count != 1 {
			t.Fatalf("%s usage commands: %d", agent, count)
		}
	}
}

func TestResetCLIHelper(t *testing.T) {
	dir := os.Getenv("WORKBENCH_RESET_HELPER")
	if dir == "" {
		return
	}
	scan := bufio.NewScanner(os.Stdin)
	out := json.NewEncoder(os.Stdout)
	consumed := false
	for scan.Scan() {
		var req map[string]any
		_ = json.Unmarshal(scan.Bytes(), &req)
		result := map[string]any{}
		failure := ""
		switch req["method"] {
		case "account/rateLimits/read":
			raw, _ := os.ReadFile(filepath.Join(dir, "usage"))
			_ = json.Unmarshal(raw, &result)
			if consumed {
				if _, err := os.Stat(filepath.Join(dir, "refresh-fail")); err == nil {
					failure = "refresh failed"
				}
			}
		case "account/rateLimitResetCredit/consume":
			f, _ := os.OpenFile(filepath.Join(dir, "calls"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
			_, _ = f.Write(append(scan.Bytes(), '\n'))
			_ = f.Close()
			raw, _ := os.ReadFile(filepath.Join(dir, "outcome"))
			_ = json.Unmarshal(raw, &result)
			if _, err := os.Stat(filepath.Join(dir, "fail")); err == nil {
				failure = "connection lost"
			}
			consumed = true
		}
		reply := map[string]any{"id": req["id"], "result": result}
		if failure != "" {
			reply = map[string]any{"id": req["id"], "error": failure}
		}
		_ = out.Encode(reply)
	}
	os.Exit(0)
}

func TestBankedResetRedemption(t *testing.T) {
	posixOnly(t)
	s, m := authServer(t)
	dir := t.TempDir()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("WORKBENCH_RESET_HELPER", dir)
	binary := script(t, filepath.Join(dir, "codex"), "exec "+strconv.Quote(exe)+" -test.run=^TestResetCLIHelper$ -- \"$@\"\n")
	m.agents.Codex = AgentSetup{Path: binary, Home: dir}
	write := func(file string, v any) {
		b, _ := json.Marshal(v)
		if err := os.WriteFile(filepath.Join(dir, file), b, 0600); err != nil {
			t.Fatal(err)
		}
	}
	usage := func(expiry int64, status string) {
		write("usage", map[string]any{"accountId": "account-a", "rateLimitResetCredits": map[string]any{"availableCount": 1, "credits": []any{map[string]any{"id": "credit-a", "status": status, "resetType": "codexRateLimits", "expiresAt": expiry}}}})
	}
	usage(time.Now().Add(time.Hour).Unix(), "available")
	write("outcome", map[string]any{"outcome": "reset"})
	call := func(key, account, credit string) map[string]any {
		t.Helper()
		b, _ := json.Marshal(resetRequest{AccountID: account, CreditID: credit, IdempotencyKey: key})
		w := request(t, s, s.Host, "POST", "/api/agents/codex/usage/reset", string(b), nil)
		var v map[string]any
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &v) != nil {
			t.Fatalf("%d %s", w.Code, w.Body.String())
		}
		return v
	}
	count := func() int {
		b, _ := os.ReadFile(filepath.Join(dir, "calls"))
		return strings.Count(string(b), "account/rateLimitResetCredit/consume")
	}
	if v := call("first", "account-a", "credit-a"); v["outcome"] != "reset" {
		t.Fatal(v)
	}
	if v := call("first", "account-a", "credit-a"); v["outcome"] != "reset" || count() != 1 {
		t.Fatalf("receipt replay: %v calls=%d", v, count())
	}
	for _, args := range [][3]string{{"first", "account-a", "other"}, {"account", "account-b", "credit-a"}, {"missing", "account-a", "unknown"}} {
		if v := call(args[0], args[1], args[2]); v["outcome"] != "notAttempted" {
			t.Fatal(v)
		}
	}
	usage(1, "available")
	if v := call("expired", "account-a", "credit-a"); v["outcome"] != "notAttempted" {
		t.Fatal(v)
	}
	usage(time.Now().Add(time.Hour).Unix(), "redeemed")
	if v := call("redeemed", "account-a", "credit-a"); v["outcome"] != "notAttempted" {
		t.Fatal(v)
	}
	usage(time.Now().Add(time.Hour).Unix(), "available")
	write("fail", true)
	if v := call("ambiguous", "account-a", "credit-a"); v["outcome"] != "uncertain" {
		t.Fatal(v)
	}
	_ = os.Remove(filepath.Join(dir, "fail"))
	write("outcome", map[string]any{"outcome": "alreadyRedeemed"})
	usage(1, "redeemed")
	if v := call("ambiguous", "account-a", "credit-a"); v["outcome"] != "alreadyRedeemed" {
		t.Fatal(v)
	}
	usage(time.Now().Add(time.Hour).Unix(), "available")
	for _, outcome := range []string{"nothingToReset", "noCredit"} {
		write("outcome", map[string]any{"outcome": outcome})
		if v := call(outcome, "account-a", ""); v["outcome"] != outcome {
			t.Fatal(v)
		}
	}
	write("outcome", map[string]any{"outcome": "future-outcome"})
	if v := call("unknown-result", "account-a", "credit-a"); v["outcome"] != "uncertain" {
		t.Fatal(v)
	}
	m.resetMu.Lock()
	v := call("unknown-result", "account-a", "credit-a")
	m.resetMu.Unlock()
	if v["outcome"] != "uncertain" {
		t.Fatal("busy retry lost its pending attempt", v)
	}
	write("outcome", map[string]any{"outcome": "reset"})
	if v := call("unknown-result", "account-a", "credit-a"); v["outcome"] != "reset" {
		t.Fatal(v)
	}
	write("refresh-fail", true)
	if v := call("refresh-fail", "account-a", "credit-a"); v["outcome"] != "reset" || v["usageError"] == nil {
		t.Fatal(v)
	}
	if count() != 8 {
		t.Fatalf("consume calls %d", count())
	}
	if w := request(t, s, s.Host, "GET", "/api/agents/codex/usage/reset", "", nil); w.Code != 405 {
		t.Fatal(w.Code)
	}
	setAccess(t, s, `{"remoteAccess":"pairing"}`)
	if w := lan(t, s, "POST", "/api/agents/codex/usage/reset", `{}`, nil); w.Code != 401 {
		t.Fatal("unauthenticated reset accepted", w.Code)
	}
}
