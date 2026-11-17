package workbench

import (
	"io/fs"
	"net/http"
	"strconv"
)

// fileETag names one version of a file: its size and modification time to
// the nanosecond. Last-Modified alone has one-second precision, so an agent
// writing the same file twice within a second would be served the old copy.
func fileETag(info fs.FileInfo) string {
	return `"` + strconv.FormatInt(info.Size(), 36) + "-" +
		strconv.FormatInt(info.ModTime().UnixNano(), 36) + `"`
}

// revalidate makes the browser ask before each reuse of a file an agent may
// rewrite under the same name; an unchanged file costs a 304 (ServeContent
// answers If-None-Match). A response already kept from caches stays so.
func revalidate(w http.ResponseWriter, info fs.FileInfo) {
	if w.Header().Get("Cache-Control") != "private, no-store" {
		w.Header().Set("Cache-Control", "private, no-cache")
	}
	w.Header().Set("ETag", fileETag(info))
}
