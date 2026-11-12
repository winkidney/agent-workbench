import { afterEach, describe, expect, it, vi } from "vitest";
import { download, downloadUrl } from "./api";

// A stand-in for the page's document: the links made and clicked.
function page() {
  const clicked: { href: string; download: string }[] = [];
  vi.stubGlobal("document", {
    createElement: () => {
      const a = {
        href: "",
        download: "unset",
        hidden: false,
        click: () => clicked.push({ href: a.href, download: a.download }),
        remove: () => {},
      };
      return a;
    },
    body: { append: () => {} },
  });
  return clicked;
}

afterEach(() => vi.unstubAllGlobals());

describe("downloading a path a session wrote", () => {
  it("names the session, the path and where the agent was", () => {
    expect(downloadUrl("codex:a b", "out/報告 1.txt", "/w/sub")).toBe(
      "/api/download?session=codex%3Aa%20b&path=out%2F%E5%A0%B1%E5%91%8A%201.txt&base=%2Fw%2Fsub",
    );
    expect(downloadUrl("s", "a.txt")).toBe(
      "/api/download?session=s&path=a.txt",
    );
  });
  it("checks it, then lets the browser stream it to disk", async () => {
    const clicked = page();
    const fetch = vi.fn(async () => new Response(null, { status: 200 }));
    vi.stubGlobal("fetch", fetch);
    await download("s", "build", "/w");
    expect(fetch).toHaveBeenCalledWith(
      "/api/download?session=s&path=build&base=%2Fw",
      { method: "HEAD" },
    );
    expect(clicked).toEqual([
      { href: "/api/download?session=s&path=build&base=%2Fw", download: "" },
    ]);
  });
  it("reports a missing path here, without a failed download", async () => {
    const clicked = page();
    vi.stubGlobal("fetch", async () => new Response(null, { status: 404 }));
    await expect(download("s", "gone.zip")).rejects.toThrow(
      "gone.zip no longer exists.",
    );
    vi.stubGlobal("fetch", async () => new Response(null, { status: 400 }));
    await expect(download("s", "../x")).rejects.toThrow(
      "Couldn't download ../x.",
    );
    expect(clicked).toEqual([]);
  });
});
