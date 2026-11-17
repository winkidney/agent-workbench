import { describe, expect, it, vi } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";
import {
  EnvContext,
  PathChip,
  PathCode,
  openWorkspacePath,
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

const render = (node: React.ReactNode) =>
  renderToStaticMarkup(
    <EnvContext.Provider value={env()}>{node}</EnvContext.Provider>,
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
