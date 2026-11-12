import { describe, expect, it } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";
import {
  backgroundable,
  elapsed,
  memory,
  summary,
  taskState,
  taskTitle,
  type BackgroundTask,
} from "./background";
import { StatusIcon } from "../components/AgentIcon";
import { applyEvents, emptyState } from "../transcript/reducer";

const now = Date.parse("2026-11-12T10:10:00Z");
const task = (extra: Partial<BackgroundTask>): BackgroundTask => ({
  id: "t1",
  source: "claude",
  kind: "shell",
  description: "npm run dev",
  status: "running",
  started: "2026-11-12T10:00:00Z",
  ...extra,
});

describe("background work", () => {
  it("sums it up in one line", () => {
    expect(summary([task({})], now)).toBe(
      "1 background task · npm run dev 10m 0s",
    );
    expect(
      summary(
        [
          task({}),
          task({ id: "t2", kind: "monitor", description: "watch CI" }),
          task({ id: "t3", status: "completed" }),
        ],
        now,
      ),
    ).toBe("2 background tasks · npm run dev 10m 0s");
    expect(summary([task({ status: "completed" })], now)).toBe(
      "Background shell done",
    );
    expect(summary([], now)).toBe("");
  });

  it("shows progress while running and the end after", () => {
    expect(taskState(task({ progress: "12 tool uses · Grep" }))).toBe(
      "12 tool uses · Grep",
    );
    expect(
      taskState(
        task({ source: "codex", kind: "terminal", cpu: 3.4, rssKb: 51200 }),
      ),
    ).toBe("3% CPU · 50 MB");
    expect(taskState(task({}))).toBe("running");
    expect(taskState(task({ status: "failed" }))).toBe("failed");
    expect(memory(2 * 1024 * 1024)).toBe("2.0 GB");
    expect(
      elapsed(
        task({ status: "completed", ended: "2026-11-12T10:00:08Z" }),
        now,
      ),
    ).toBe("8s");
  });

  it("names Codex terminals by the command typed", () => {
    expect(
      taskTitle(
        task({
          kind: "terminal",
          description: `/usr/bin/zsh -lc "python3 -m http.server"`,
        }),
      ),
    ).toBe("python3 -m http.server");
  });

  it("finds what a Claude turn waits on", () => {
    const blocks = [
      { key: "u1", kind: "user" },
      {
        key: "toolu_1",
        kind: "tool",
        tool: { name: "Bash", status: "running" },
      },
      {
        key: "toolu_2",
        kind: "tool",
        tool: { name: "Read", status: "running" },
      },
    ];
    expect(backgroundable(blocks)).toBe("toolu_1");
    expect(
      backgroundable([
        { key: "toolu_1", kind: "tool", tool: { name: "Bash", status: "ok" } },
      ]),
    ).toBeUndefined();
    // Only within the current turn.
    expect(
      backgroundable([
        {
          key: "toolu_1",
          kind: "tool",
          tool: { name: "Bash", status: "running" },
        },
        { key: "u2", kind: "user" },
      ]),
    ).toBeUndefined();
  });

  it("marks sessions with background work between turns", () => {
    const html = renderToStaticMarkup(
      <StatusIcon status="idle" background={2} />,
    );
    expect(html).toContain('aria-label="2 background tasks"');
    expect(html).toContain("bg-count");
    // A running turn still shows as running; a request for the user wins.
    expect(
      renderToStaticMarkup(<StatusIcon status="running" background={2} />),
    ).toContain("px-chase");
    expect(
      renderToStaticMarkup(
        <StatusIcon status="idle" background={2} attention="approval" />,
      ),
    ).toContain("needs-you");
  });

  it("shows a failed task's notice in the warning color", () => {
    const s = applyEvents(
      emptyState(),
      [
        {
          seq: 1,
          session: "s",
          kind: "notice",
          time: "",
          data: { text: "Background shell failed: npm test", tone: "warn" },
        },
      ],
      { agent: "claude", history: false },
    );
    expect([...s.blocks.values()][0].tone).toBe("warn");
  });
});
