// The records the server reads from Claude, live and from its transcript
// (internal/workbench/claude_records.go): the same cases its Go test checks,
// shown here as the page shows them (ADR 0025).
import { describe, expect, it } from "vitest";
import { applyEvents, emptyState, reconcile } from "./reducer";
import type { WorkbenchEvent } from "./model";

// Read with the page's own bundler, so the page's build needs no Node types.
const files = import.meta.glob<Record<string, unknown>[]>(
  "../../../internal/workbench/testdata/claude/*.view.json",
  { eager: true, import: "default" },
);
const byName = Object.fromEntries(
  Object.entries(files).map(([path, v]) => [
    path.replace(/^.*\/|\.view\.json$/g, ""),
    v,
  ]),
);
const views = (name: string) => byName[name];
const events = (records: Record<string, unknown>[]): WorkbenchEvent[] =>
  records.map(
    (data, i) => ({ seq: i + 1, kind: "native", data }) as WorkbenchEvent,
  );
const shown = (records: Record<string, unknown>[], history = true) => {
  const s = applyEvents(emptyState(), events(records), {
    agent: "claude",
    history,
  });
  return s.keys.map((k) => s.blocks.get(k)!);
};

const REPORT_FROM = "From subagent general-purpose";

describe("Claude records read on the server", () => {
  const cases = Object.keys(byName);
  it("covers every case", () => {
    expect(cases).toEqual(
      expect.arrayContaining([
        "report-mid-turn",
        "report-between-turns",
        "report-without-origin",
        "task-mid-turn",
        "task-between-turns",
        "peer-session",
        "steered",
      ]),
    );
  });
  for (const name of cases.filter((n) => n.startsWith("report-")))
    it(`shows a subagent's report as Markdown: ${name}`, () => {
      const b = shown(views(name));
      expect(b).toHaveLength(2);
      expect(b[1]).toMatchObject({
        key: "peer:a1b2c3d4e5f60718:3a367469",
        kind: "notice",
        markdown: true,
        label: "subagent",
      });
      expect(b[1].text).toMatch(
        /^## Review of example\/app#12\n\n- \*\*Verdict\*\*/,
      );
      expect(b[1].text).not.toContain("agent-message");
      expect(b[1].text).not.toContain("Subagent hand-back");
      expect(b[1].from).toBe(
        name === "report-without-origin"
          ? "From subagent a1b2c3d4e5f60718"
          : REPORT_FROM,
      );
    });
  for (const name of ["task-mid-turn", "task-between-turns"])
    it(`shows a task notification as a line: ${name}`, () => {
      const b = shown(views(name));
      expect(b[1]).toMatchObject({
        key: name === "task-mid-turn" ? "task:q1" : "task:n1",
        kind: "notice",
        label: "task",
      });
      expect(b[1].text).toBe(
        name === "task-between-turns"
          ? 'Background command "Run the full build" completed (exit code 0) · 88 tool uses · 183.2K tokens · 20m 21s'
          : 'Background command "Run the full build" completed (exit code 0)',
      );
      expect(b[1].detail).toContain("<task-id>bcgut4u57</task-id>");
    });
  it("shows a plugin's plain notice as its own line", () => {
    expect(shown(views("task-plain-notice"))[1]).toMatchObject({
      label: "task",
      text: "example-plugin: its SessionStart hook was skipped.",
    });
  });
  it("shows another session's message and a steered message", () => {
    expect(shown(views("peer-session"))[1]).toMatchObject({
      label: "session",
      from: "From another session other-session",
      text: "Build is **green**.",
      markdown: true,
    });
    expect(shown(views("steered"))[1]).toMatchObject({
      kind: "user",
      text: "end with BANANA",
      steered: true,
    });
  });
  it("replaces a live report with the turn's record", () => {
    // Live: the prompt, then the report as the server reads a task_notification.
    const [prompt, report] = views("report-mid-turn");
    const live = applyEvents(
      { ...emptyState(), liveStart: 0 },
      events([
        { ...prompt, history: undefined },
        {
          type: "workbench_peer",
          key: report.key,
          from: report.from,
          handback: true,
          body: report.body,
          uuid: "live-uuid",
        },
      ]),
      { agent: "claude", history: false },
    );
    const tail = applyEvents(emptyState(), events(views("report-mid-turn")), {
      agent: "claude",
      history: true,
    });
    const next = reconcile(live, tail);
    expect(next).not.toBeNull();
    const b = next!.keys.map((k) => next!.blocks.get(k)!);
    expect(b.filter((x) => x.label === "subagent")).toHaveLength(1);
    expect(b.find((x) => x.label === "subagent")!.from).toBe(REPORT_FROM);
  });
});
