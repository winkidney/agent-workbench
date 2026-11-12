// How the shell reacts to a refused API call. Other devices get the
// server's own page (marked X-Workbench-Access) when access from them is off,
// the password changed, or too many attempts failed: reloading shows that
// page, or the browser's sign-in prompt. Any other 401 means the pairing
// session ended.
export function refusal(
  status: number,
  headers: Headers,
): "reload" | "pair" | undefined {
  if (headers.has("X-Workbench-Access")) return "reload";
  if (status === 401) return "pair";
}

// Throws for a refused call after starting what the refusal needs.
export function checkAccess(r: Response, path: string) {
  switch (refusal(r.status, r.headers)) {
    case "reload":
      location.reload();
      throw new Error("Signing in again…");
    case "pair":
      if (path === "login")
        throw new Error("Incorrect pairing code. Try again.");
      window.dispatchEvent(new CustomEvent("workbench:unauthorized"));
      throw new Error("Session expired. Enter your pairing code again.");
  }
}

// JSON API helper.
export async function api<T>(
  path: string,
  method = "GET",
  body?: unknown,
): Promise<T> {
  const r = await fetch("/api/" + path, {
    method,
    headers: body ? { "Content-Type": "application/json" } : {},
    body: body ? JSON.stringify(body) : undefined,
  });
  checkAccess(r, path);
  if (!r.ok) throw new Error(await r.text());
  return r.json();
}

// Where a file or folder a session wrote downloads from: a file as it is,
// a folder as a zip made while it is sent.
export function downloadUrl(session: string, path: string, base?: string) {
  return (
    `/api/download?session=${encodeURIComponent(session)}&path=${encodeURIComponent(path)}` +
    (base ? `&base=${encodeURIComponent(base)}` : "")
  );
}

// Downloads it with the browser's own download, which streams it to disk
// (nothing is held in the page). It is checked first, so a missing or
// refused path is reported here rather than as a failed download.
export async function download(session: string, path: string, base?: string) {
  const url = downloadUrl(session, path, base);
  const r = await fetch(url, { method: "HEAD" });
  checkAccess(r, "download");
  if (!r.ok)
    throw new Error(
      r.status === 404
        ? `${path} no longer exists.`
        : `Couldn't download ${path}.`,
    );
  const a = document.createElement("a");
  a.href = url;
  a.download = "";
  a.hidden = true;
  document.body.append(a);
  a.click();
  a.remove();
}
