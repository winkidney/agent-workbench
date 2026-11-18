import { describe, expect, it, vi } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";
import {
  EnvContext,
  PathChip,
  PathCode,
  UrlLink,
  openWorkspacePath,
  splitLineRef,
  opensInPanel,
  type Env,
} from "./media";

const env = (over: Partial<Env> = {}): Env => ({
  cwd: "/w",
  openUrl: () => {},
  openPath: () => {},
  thumb: () => null,
  reveal: () => {},
  locate: () => {},
  view: () => {},
  ...over,
});

const render = (node: React.ReactNode, e: Env = env()) =>
  renderToStaticMarkup(
    <EnvContext.Provider value={e}>{node}</EnvContext.Provider>,
  );

describe("what opens in the preview panel", () => {
  it("pages, documents, Markdown and text", () => {
    for (const p of [
      "docs/plan.md",
      "README.markdown",
      "out/report.html",
      "a.pdf",
      "data.json",
      "run.log",
      "src/main.go",
      "web/App.tsx",
      "Makefile",
      "deploy/Dockerfile",
      ".env.local",
    ])
      expect(opensInPanel(p), p).toBe(true);
  });
  it("not binaries, archives or folders", () => {
    for (const p of [
      "dist/app.zip",
      "sound.wav",
      "scene.blend",
      "bin/agent-workbench",
      "build/",
    ])
      expect(opensInPanel(p), p).toBe(false);
  });
});

describe("a path a session wrote", () => {
  it("previews what the panel shows and locates the rest", () => {
    const openPath = vi.fn();
    const locate = vi.fn();
    const reveal = vi.fn();
    const e = env({ openPath, locate, reveal });
    openWorkspacePath(e, "notes/plan.md", "/w/sub");
    openWorkspacePath(e, "dist/app.zip", "/w/sub");
    expect(openPath).toHaveBeenCalledWith("notes/plan.md", "/w/sub");
    expect(locate).toHaveBeenCalledWith("dist/app.zip", "/w/sub");
    expect(reveal).not.toHaveBeenCalled();
  });
  it("says so on links and inline code", () => {
    expect(render(<PathCode path="build/out.log" />)).toContain(
      'title="Preview build/out.log"',
    );
    expect(render(<PathCode path="dist/app.zip" />)).toContain(
      'title="Show dist/app.zip in Files"',
    );
    const chip = render(<PathChip path="/w/report.pdf" />);
    expect(chip).toContain('title="Preview report.pdf"');
    // Its side button shows it in the panel's file list, never the file
    // manager directly.
    expect(chip).toContain('aria-label="Show report.pdf in Files"');
    expect(chip).not.toContain("this computer");
  });
});

describe("the lines a link points to", () => {
  it("are read from the end of the path, as editors and GitHub write them", () => {
    expect(splitLineRef("/w/a.gd:127")).toEqual({
      path: "/w/a.gd",
      at: { line: 127 },
    });
    expect(splitLineRef("a.go:12:5")).toEqual({
      path: "a.go",
      at: { line: 12 },
    });
    expect(splitLineRef("a.go:12-20")).toEqual({
      path: "a.go",
      at: { line: 12, end: 20 },
    });
    expect(splitLineRef("a.go:12:5-20:3")).toEqual({
      path: "a.go",
      at: { line: 12, end: 20 },
    });
    expect(splitLineRef("a.go#L12")).toEqual({
      path: "a.go",
      at: { line: 12 },
    });
    expect(splitLineRef("a.go#L12C5")).toEqual({
      path: "a.go",
      at: { line: 12 },
    });
    expect(splitLineRef("a.go#L12-L20")).toEqual({
      path: "a.go",
      at: { line: 12, end: 20 },
    });
  });
  it("leave other paths as they are", () => {
    for (const p of ["a:b.txt", "notes.md", "a.go:0", "a.go:", ":12", "v1:2x"])
      expect(splitLineRef(p), p).toEqual({ path: p });
    // A range that runs backwards marks its first line only.
    expect(splitLineRef("a.go:20-12")).toEqual({
      path: "a.go",
      at: { line: 20 },
    });
  });
  it("open the file at them, or locate it without them", () => {
    const openPath = vi.fn();
    const locate = vi.fn();
    const e = env({ openPath, locate });
    const html = render(<UrlLink href="/w/sub/fx.gd:127">fx</UrlLink>, e);
    expect(html).toContain('href="#sub/fx.gd"');
    expect(html).toContain('title="Preview sub/fx.gd at line 127"');
    openWorkspacePath(e, "sub/fx.gd", "/w", { line: 127 });
    openWorkspacePath(e, "dist/app.zip", "/w", { line: 3 });
    expect(openPath).toHaveBeenCalledWith("sub/fx.gd", "/w", { line: 127 });
    expect(locate).toHaveBeenCalledWith("dist/app.zip", "/w");
  });
});
