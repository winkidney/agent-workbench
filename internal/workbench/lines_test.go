package workbench

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestLineReaderHasNoLengthLimit(t *testing.T) {
	long := strings.Repeat("x", 40<<20)
	scan := newLineReader(strings.NewReader("a\r\n" + long + "\n\nlast"))
	var got []string
	for scan.Scan() {
		got = append(got, scan.Text())
	}
	if scan.Err() != nil {
		t.Fatal(scan.Err())
	}
	if len(got) != 4 || got[0] != "a" || got[1] != long || got[2] != "" || got[3] != "last" {
		t.Fatalf("lines %d", len(got))
	}
}

// A page of turns with generated images is one line of tens of megabytes.
func TestCodexClientReadsAHugeResponse(t *testing.T) {
	posixOnly(t)
	server := script(t, filepath.Join(t.TempDir(), "codex"), `while IFS= read -r line; do
  id=$(printf '%s' "$line" | sed -n 's/.*"id":"\([^"]*\)".*/\1/p')
  [ -z "$id" ] && continue
  case "$line" in
    *thread/turns/list*)
      printf '{"id":"%s","result":{"data":[{"id":"t1","items":[{"type":"imageGeneration","result":"' "$id"
      head -c 25000000 /dev/zero | tr '\0' 'A'
      printf '"}]}]}}\n' ;;
    *) printf '{"id":"%s","result":{}}\n' "$id" ;;
  esac
done
`)
	c, err := startCodexClientWith(agentRun{Binary: server}, "test")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	page, err := c.request("turns", "thread/turns/list", map[string]any{"threadId": "z"})
	if err != nil {
		t.Fatal(err)
	}
	item := obj(obj(page["data"].([]any)[0])["items"].([]any)[0])
	if n := len(str(item["result"])); n != 25000000 {
		t.Fatalf("result %d bytes", n)
	}
}
