// JSON API helper; a 401 signals the shell to show the pairing screen.
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
  if (r.status === 401) {
    if (path === "login") throw new Error("Incorrect pairing code. Try again.");
    window.dispatchEvent(new CustomEvent("workbench:unauthorized"));
    throw new Error("Session expired. Enter your pairing code again.");
  }
  if (!r.ok) throw new Error(await r.text());
  return r.json();
}
