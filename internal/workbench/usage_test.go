package workbench

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestUsageRefreshAndErrorsFromBothCLIs(t *testing.T) {
	posixOnly(t)
	for _, agent := range []string{"codex", "claude"} {
		t.Run(agent, func(t *testing.T) {
			dir := t.TempDir()
			payload := filepath.Join(dir, "payload")
			failure := filepath.Join(dir, "failure")
			log := filepath.Join(dir, "calls")
			binary := script(t, filepath.Join(dir, agent), `case "$1" in --help) exit 0;; esac
while IFS= read -r line; do
 printf '%s\n' "$line" >> `+strconv.Quote(log)+`
 id=$(printf '%s' "$line" | sed -n 's/.*"id":"\([^"]*\)".*/\1/p')
 cid=$(printf '%s' "$line" | sed -n 's/.*"request_id":"\([^"]*\)".*/\1/p')
 result='{}'; err=''
 case "$line" in
  *account/rateLimits/read*|*get_usage*)
   if [ -f `+strconv.Quote(failure)+` ]; then err='usage denied'; else result=$(cat `+strconv.Quote(payload)+`); fi ;;
 esac
 if [ -n "$cid" ]; then
  if [ -n "$err" ]; then printf '{"response":{"request_id":"%s","subtype":"error","error":"%s"}}\n' "$cid" "$err"
  else printf '{"response":{"request_id":"%s","subtype":"success","response":%s}}\n' "$cid" "$result"; fi
 else
  if [ -n "$err" ]; then printf '{"id":"%s","error":"%s"}\n' "$id" "$err"
  else printf '{"id":"%s","result":%s}\n' "$id" "$result"; fi
 fi
done
`)
			s, m := authServer(t)
			home := filepath.Join(dir, "home")
			os.Mkdir(home, 0700)
			if agent == "codex" {
				m.agents.Codex = AgentSetup{Path: binary, Home: home}
			} else {
				m.agents.Claude = AgentSetup{Path: binary, Home: home}
			}
			get := func(refresh bool) metadata {
				t.Helper()
				path := "/api/agents/" + agent + "/usage"
				if refresh {
					path += "?refresh=1"
				}
				w := request(t, s, s.Host, "GET", path, "", nil)
				var v metadata
				if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &v) != nil {
					t.Fatalf("usage: %d %s", w.Code, w.Body.String())
				}
				return v
			}
			os.WriteFile(payload, []byte(`{"reading":1}`), 0600)
			if v := get(false); v.Error != "" || v.UsageError != "" || v.Usage["reading"] != float64(1) {
				t.Fatalf("initial: %+v", v)
			}
			os.WriteFile(payload, []byte(`{"reading":2}`), 0600)
			if v := get(false); v.Usage["reading"] != float64(1) {
				t.Fatal("normal request bypassed cache")
			}
			if v := get(true); v.Usage["reading"] != float64(2) {
				t.Fatalf("refresh did not read CLI: %+v", v)
			}
			os.WriteFile(failure, nil, 0600)
			if v := get(true); !strings.Contains(v.UsageError, "usage denied") {
				t.Fatalf("usage error swallowed: %+v", v)
			}
			os.Remove(failure)
			if v := get(true); v.UsageError != "" || v.Usage["reading"] != float64(2) {
				t.Fatalf("recovery: %+v", v)
			}
			data, _ := os.ReadFile(log)
			if strings.Contains(string(data), "thread/start") || strings.Contains(string(data), `"type":"user"`) || len(m.sessions) != 0 {
				t.Fatal("usage created a session or sent a prompt")
			}
			// A new CLI home must not reuse the old cached snapshot.
			os.WriteFile(payload, []byte(`{"reading":3}`), 0600)
			if agent == "codex" {
				m.agents.Codex.Home = dir
			} else {
				m.agents.Claude.Home = dir
			}
			if v := get(false); v.Usage["reading"] != float64(3) {
				t.Fatal("CLI home ignored in cache key")
			}
		})
	}
}
