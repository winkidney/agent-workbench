import { describe, expect, it } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";
import { EnvContext, PathChip, PathCode, type Env } from "./media";

const env = (here?: boolean): Env => ({
  cwd: "/w",
  here,
  openUrl: () => {},
  openPath: () => {},
  thumb: () => null,
  reveal: () => {},
  view: () => {},
});

const render = (here: boolean | undefined, node: React.ReactNode) =>
  renderToStaticMarkup(
    <EnvContext.Provider value={env(here)}>{node}</EnvContext.Provider>,
  );

describe("a path a session wrote", () => {
  it("shows in Files on the workbench computer", () => {
    for (const here of [true, undefined]) {
      expect(render(here, <PathCode path="build/out.log" />)).toContain(
        'title="Show build/out.log in Files (opens on this computer)"',
      );
      expect(render(here, <PathChip path="/w/report.pdf" />)).toContain(
        'aria-label="Show report.pdf in Files"',
      );
    }
  });
  it("downloads anywhere else", () => {
    const code = render(false, <PathCode path="build/out.log" />);
    expect(code).toContain('title="Download build/out.log"');
    expect(code).not.toContain("Files");
    const chip = render(false, <PathChip path="/w/report.pdf" />);
    expect(chip).toContain('aria-label="Download report.pdf"');
    expect(chip).not.toContain("Files");
  });
});
