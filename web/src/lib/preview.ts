// How this page can show a preview. Preview origins (p-<id>.agent.localhost)
// only work in a browser on the workbench computer, opened at its canonical
// address (agent.localhost:PORT). Elsewhere (another device, a tunnel) a file
// preview opens at its fileUrl on the workbench's own address, sandboxed;
// dev servers cannot be shown there.
export type PreviewMode = "origin" | "file" | "unavailable";

export function previewMode(
  p: { url: string; fileUrl?: string },
  pageHost: string,
): PreviewMode {
  const canonical = new URL(p.url).host.replace(/^p-[^.]+\./, "");
  if (pageHost.toLowerCase() === canonical.toLowerCase()) return "origin";
  return p.fileUrl ? "file" : "unavailable";
}

// The workbench computer's own address, for messages.
export function canonicalAddress(p: { url: string }): string {
  const u = new URL(p.url);
  return u.protocol + "//" + u.host.replace(/^p-[^.]+\./, "");
}

// Browsers show PDFs only outside a sandbox (the server leaves real PDFs
// unsandboxed); everything else gets an opaque origin of its own.
export function fileFrameSandbox(entry: string): string | undefined {
  return /\.pdf$/i.test(entry)
    ? undefined
    : "allow-scripts allow-forms allow-popups allow-modals allow-downloads";
}
