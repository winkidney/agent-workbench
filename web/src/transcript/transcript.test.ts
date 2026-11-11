import { describe, expect, it } from "vitest";
import type { Block, WorkbenchEvent } from "./model";
import {
  applyEvents,
  emptyState,
  follow,
  fromHistory,
  prepend,
  reconcile,
  settled,
  withSending,
  type State,
} from "./reducer";
import { loadInitial } from "./useTranscript";
import { askStates } from "./questions";
import { shapeUserText } from "./claude";
import {
  salient,
  toolDiff,
  toolSummary,
  toolTitle,
  unwrapShell,
} from "./summarize";
import { mentions, workspacePath } from "./media";
import { latestPlan } from "../composer/TodoPanel";
import {
  activity,
  compact,
  statusVerb,
  turnClock,
} from "../composer/StatusLine";

let seq = 0;
const ev = (kind: string, data: Record<string, unknown>): WorkbenchEvent => ({
  seq: ++seq,
  session: "s",
  kind,
  data,
  time: new Date(0).toISOString(),
});
const blocks = (s: State) => s.keys.map((k) => s.blocks.get(k)!);
const shape = (s: State) =>
  blocks(s).map((b: Block) => ({
    key: b.key,
    kind: b.kind,
    text: b.text,
    tool: b.tool && {
      name: b.tool.name,
      status: b.tool.status,
      output: b.tool.output,
    },
  }));

// Native records as Claude Code writes them, both on stdout and in JSONL.
const claudeRecords = {
  thinking: {
    type: "assistant",
    uuid: "a1",
    message: { id: "m1", content: [{ type: "thinking", thinking: "plan it" }] },
  },
  toolUse: {
    type: "assistant",
    uuid: "a2",
    message: {
      id: "m1",
      content: [
        {
          type: "tool_use",
          id: "toolu_1",
          name: "Bash",
          input: { command: "npm test" },
        },
      ],
    },
  },
  toolResult: {
    type: "user",
    uuid: "r1",
    message: {
      role: "user",
      content: [
        {
          type: "tool_result",
          tool_use_id: "toolu_1",
          content: "2 failed\n31 passed",
        },
      ],
    },
  },
  answer: {
    type: "assistant",
    uuid: "a3",
    message: { id: "m2", content: [{ type: "text", text: "Fixed **it**" }] },
  },
};

describe("claude adapter", () => {
  const live = [
    ev("user", { text: "run tests" }),
    ev("status", { status: "running" }),
    ev("delta", { channel: "thinking", item: "m1:0", text: "plan " }),
    ev("delta", { channel: "thinking", item: "m1:0", text: "it" }),
    ev("native", claudeRecords.thinking),
    ev("native", claudeRecords.toolUse),
    ev("native", claudeRecords.toolResult),
    ev("delta", { channel: "text", item: "m2:0", text: "Fixed " }),
    ev("native", claudeRecords.answer),
    ev("status", { status: "idle" }),
  ];
  const history: WorkbenchEvent[] = [
    {
      type: "user",
      uuid: "u1",
      message: { role: "user", content: "run tests" },
    },
    claudeRecords.thinking,
    claudeRecords.toolUse,
    claudeRecords.toolResult,
    claudeRecords.answer,
  ].map((d) => ({ seq: 0, session: "s", kind: "native", data: d, time: "" }));

  it("renders tool results as tool output, not user messages", () => {
    const s = applyEvents(emptyState(), live, {
      agent: "claude",
      history: false,
    });
    expect(shape(s)).toEqual([
      { key: "local:1", kind: "user", text: "run tests", tool: undefined },
      { key: "a1:0", kind: "thinking", text: "plan it", tool: undefined },
      {
        key: "toolu_1",
        kind: "tool",
        text: undefined,
        tool: { name: "Bash", status: "ok", output: "2 failed\n31 passed" },
      },
      { key: "a3:0", kind: "assistant", text: "Fixed **it**", tool: undefined },
    ]);
    expect(blocks(s).some((b) => b.streaming)).toBe(false);
  });

  it("live and history produce the same blocks after reconciliation", () => {
    const liveState = applyEvents(emptyState(), live, {
      agent: "claude",
      history: false,
    });
    const tail = fromHistory(history, "claude", false);
    const merged = reconcile(liveState, tail);
    expect(merged).not.toBeNull();
    expect(shape(merged!)).toEqual(shape(tail));
    expect(shape(tail).map((b) => b.kind)).toEqual([
      "user",
      "thinking",
      "tool",
      "assistant",
    ]);
  });

  it("waits for the CLI to write the latest record before reconciling", () => {
    const liveState = applyEvents(emptyState(), live, {
      agent: "claude",
      history: false,
    });
    const stale = fromHistory(history.slice(0, 3), "claude", false);
    expect(reconcile(liveState, stale)).toBeNull();
    // Initial load still keeps the unwritten live records.
    const initial = loadInitial(liveState, stale);
    expect(initial.keys).toContain("a3:0");
    expect(initial.keys.filter((k) => k === "toolu_1")).toHaveLength(1);
  });

  it("follows records another client writes without duplicating", () => {
    // Opened mid-turn: the tool call has no result yet and still runs.
    let s = fromHistory(history.slice(0, 3), "claude", true);
    expect(s.blocks.get("toolu_1")!.tool!.status).toBe("running");
    // The server sends only new records: the result, then the answer.
    s = follow(s, history.slice(3, 4), "claude");
    expect(s.blocks.get("toolu_1")!.tool).toMatchObject({
      name: "Bash",
      status: "ok",
      output: "2 failed\n31 passed",
    });
    s = follow(s, history.slice(4), "claude");
    // Re-sent records merge in place.
    s = follow(s, history.slice(2), "claude");
    expect(shape(s)).toEqual(shape(fromHistory(history, "claude", false)));
  });

  it("settles a turn another client left unfinished", () => {
    const s = settled(fromHistory(history.slice(0, 3), "claude", true));
    expect(s.blocks.get("toolu_1")!.tool!.status).toBe("interrupted");
  });

  it("keeps the directory each record was written in", () => {
    const at = (cwd: string, d: Record<string, unknown>) => ({ ...d, cwd });
    const s = fromHistory(
      [
        at("/w", {
          type: "user",
          uuid: "u1",
          message: { role: "user", content: "go" },
        }),
        at("/w/sub", claudeRecords.toolUse),
        at("/w/sub", claudeRecords.toolResult),
        at("/w/sub", claudeRecords.answer),
      ].map((d) => ({
        seq: 0,
        session: "s",
        kind: "native",
        data: d,
        time: "",
      })),
      "claude",
      false,
    );
    expect(blocks(s).map((b) => [b.kind, b.cwd])).toEqual([
      ["user", "/w"],
      ["tool", "/w/sub"],
      ["assistant", "/w/sub"],
    ]);
    const codex = fromHistory(
      [
        ev("native", {
          method: "item/completed",
          history: true,
          params: {
            item: {
              id: "c1",
              type: "commandExecution",
              command: "ls",
              cwd: "/w/app",
              status: "completed",
            },
          },
        }),
      ],
      "codex",
      false,
    );
    expect(codex.blocks.get("c1")!.cwd).toBe("/w/app");
  });

  it("shows a local command's output, live and from history", () => {
    const live = applyEvents(
      emptyState(),
      [
        ev("native", {
          type: "assistant",
          uuid: "l1",
          local_command_run: true,
          message: { content: [{ type: "text", text: "## Context Usage" }] },
        }),
      ],
      { agent: "claude", history: false },
    );
    expect(live.blocks.get("l1")).toMatchObject({
      kind: "notice",
      text: "## Context Usage",
      markdown: true,
    });
    const history = fromHistory(
      [
        ev("native", {
          type: "system",
          subtype: "local_command",
          uuid: "c1",
          content:
            "<local-command-stdout>## Context Usage</local-command-stdout>",
        }),
      ],
      "claude",
      false,
    );
    expect(history.blocks.get("c1")).toMatchObject({
      kind: "notice",
      text: "## Context Usage",
      markdown: true,
    });
  });

  it("marks tools without results in history as interrupted", () => {
    const s = fromHistory(history.slice(0, 3), "claude", false);
    expect(s.blocks.get("toolu_1")!.tool!.status).toBe("interrupted");
  });

  it("nests subagent activity under the parent tool", () => {
    const s = applyEvents(
      emptyState(),
      [
        ev("native", {
          type: "assistant",
          uuid: "t",
          message: {
            id: "m",
            content: [
              {
                type: "tool_use",
                id: "task",
                name: "Task",
                input: { description: "Explore" },
              },
            ],
          },
        }),
        ev("native", {
          type: "assistant",
          uuid: "c",
          parent_tool_use_id: "task",
          message: {
            id: "n",
            content: [
              {
                type: "tool_use",
                id: "child",
                name: "Grep",
                input: { pattern: "x" },
              },
            ],
          },
        }),
      ],
      { agent: "claude", history: false },
    );
    expect(s.blocks.get("child")!.parent).toBe("task");
  });

  it("keeps unknown content blocks visible", () => {
    const s = applyEvents(
      emptyState(),
      [
        ev("native", {
          type: "assistant",
          uuid: "x",
          message: { id: "m", content: [{ type: "future_block", value: 1 }] },
        }),
      ],
      { agent: "claude", history: false },
    );
    expect(blocks(s)[0].tool?.name).toBe("future_block");
  });

  it("shows slash commands and local output like the TUI", () => {
    expect(
      shapeUserText(
        "<command-name>/plan</command-name><command-args>fix it</command-args>",
      ),
    ).toEqual({
      kind: "user",
      text: "/plan fix it",
    });
    expect(
      shapeUserText("<local-command-stdout>Enabled</local-command-stdout>")
        .kind,
    ).toBe("notice");
    expect(
      shapeUserText("<local-command-caveat>x</local-command-caveat>").kind,
    ).toBe("skip");
    expect(shapeUserText("[Request interrupted by user]").text).toBe(
      "Interrupted by user",
    );
  });
});

describe("codex adapter", () => {
  const command = {
    id: "call_1",
    type: "commandExecution",
    command: "/bin/bash -lc 'npm test'",
    status: "inProgress",
    commandActions: [],
  };
  it("merges item/started and item/completed into one row", () => {
    const s = applyEvents(
      emptyState(),
      [
        ev("user", { text: "hi" }),
        ev("native", {
          method: "item/started",
          params: {
            item: {
              id: "u",
              type: "userMessage",
              content: [{ type: "text", text: "hi" }],
            },
          },
        }),
        ev("native", { method: "item/started", params: { item: command } }),
        ev("native", {
          method: "item/completed",
          params: {
            item: {
              ...command,
              status: "completed",
              exitCode: 1,
              aggregatedOutput: "boom\n",
            },
          },
        }),
        ev("delta", { channel: "text", item: "msg", text: "Done" }),
        ev("native", {
          method: "item/completed",
          params: { item: { id: "msg", type: "agentMessage", text: "Done." } },
        }),
        ev("native", {
          method: "turn/completed",
          params: { turn: { id: "t", status: "completed", durationMs: 4200 } },
        }),
      ],
      { agent: "codex", history: false },
    );
    expect(shape(s).map((b) => [b.key, b.kind])).toEqual([
      ["u", "user"],
      ["call_1", "tool"],
      ["msg", "assistant"],
      ["turn:t", "turn"],
    ]);
    const tool = s.blocks.get("call_1")!.tool!;
    expect(tool.status).toBe("failed");
    expect(toolTitle(tool)).toEqual({ name: "Shell", arg: "npm test" });
    expect(toolSummary(tool).lines).toEqual(["Exit 1", "boom"]);
    expect(s.blocks.get("msg")!.text).toBe("Done.");
  });

  it("drops an interrupted notice once the turn's real end arrives", () => {
    const summary = (status: string) =>
      ev("native", {
        method: "turn/completed",
        history: true,
        params: { turn: { id: "t2", status } },
      });
    // Read while another client was still running it, then followed.
    let s = fromHistory([summary("interrupted")], "codex", true);
    expect(s.blocks.get("turn-interrupted:t2")?.text).toBe(
      "Interrupted by user",
    );
    s = follow(s, [summary("completed")], "codex");
    expect(s.blocks.has("turn-interrupted:t2")).toBe(false);
    expect(s.blocks.get("turn:t2")!.text).toBe("completed");
  });

  it("renders unknown item types generically", () => {
    const s = applyEvents(
      emptyState(),
      [
        ev("native", {
          method: "item/completed",
          params: {
            item: {
              id: "z",
              type: "quantumLeap",
              target: "1985",
              status: "completed",
            },
          },
        }),
      ],
      { agent: "codex", history: true },
    );
    const tool = s.blocks.get("z")!.tool!;
    expect(tool.name).toBe("quantumLeap");
    expect(toolTitle(tool).arg).toBe("1985");
  });

  it("parses file change diffs and plan updates", () => {
    const s = applyEvents(
      emptyState(),
      [
        ev("native", {
          method: "item/completed",
          params: {
            item: {
              id: "f",
              type: "fileChange",
              status: "completed",
              changes: [
                {
                  path: "a.ts",
                  kind: { type: "update" },
                  diff: "@@ -1 +1 @@\n-old\n+new\n+more",
                },
              ],
            },
          },
        }),
        ev("native", {
          method: "turn/plan/updated",
          params: {
            turnId: "t",
            plan: [
              { step: "Read", status: "completed" },
              { step: "Fix", status: "inProgress" },
            ],
          },
        }),
        ev("native", {
          method: "turn/plan/updated",
          params: {
            turnId: "t",
            plan: [
              { step: "Read", status: "completed" },
              { step: "Fix", status: "completed" },
            ],
          },
        }),
      ],
      { agent: "codex", history: false },
    );
    const tool = s.blocks.get("f")!.tool!;
    expect(toolSummary(tool, toolDiff(tool)).lines).toEqual(["+2 −1"]);
    const plans = blocks(s).filter((b) => b.kind === "plan");
    expect(plans).toHaveLength(1);
    expect(plans[0].steps!.map((x) => x.status)).toEqual([
      "completed",
      "completed",
    ]);
  });
});

describe("history paging", () => {
  it("prepends older pages without duplicating overlapping tools", () => {
    const newer = fromHistory(
      [
        {
          seq: 0,
          session: "s",
          kind: "native",
          time: "",
          data: claudeRecords.toolResult,
        },
      ],
      "claude",
      false,
    );
    const older = fromHistory(
      [
        {
          seq: 0,
          session: "s",
          kind: "native",
          time: "",
          data: claudeRecords.toolUse,
        },
      ],
      "claude",
      false,
    );
    const s = prepend(newer, older);
    expect(s.keys).toEqual(["toolu_1"]);
    const tool = s.blocks.get("toolu_1")!.tool!;
    expect(tool.name).toBe("Bash");
    expect(tool.output).toContain("31 passed");
    expect(tool.status).toBe("ok");
  });
});

describe("summaries", () => {
  it("picks a salient argument for any tool", () => {
    expect(salient({ file_path: "/w/a.ts", limit: 3 })).toBe("/w/a.ts");
    expect(salient({ zzz: "fallback" })).toBe("fallback");
    expect(
      toolTitle(
        { name: "Read", input: { file_path: "/w/src/a.ts" }, status: "ok" },
        "/w",
      ).arg,
    ).toBe("src/a.ts");
    expect(
      toolTitle({
        name: "mcp__github__create_issue",
        input: { title: "x" },
        status: "ok",
      }).name,
    ).toBe("github · create_issue");
    expect(unwrapShell("bash -lc 'ls -la'")).toBe("ls -la");
  });
  it("summarises edits as line stats", () => {
    const t = {
      name: "Edit",
      input: { old_string: "a\nb", new_string: "a\nc\nd" },
      status: "ok" as const,
    };
    expect(toolSummary(t, toolDiff(t)).lines).toEqual(["+2 −1"]);
  });
  it("previews long output with a remainder count", () => {
    const s = toolSummary({
      name: "Bash",
      status: "ok",
      output: "1\n2\n3\n4\n5",
    });
    expect(s.lines).toEqual(["1", "2", "3"]);
    expect(s.more).toBe(2);
  });
});

describe("media", () => {
  it("keeps paths inside the working directory", () => {
    expect(workspacePath("/w/out/a.png", "/w")).toBe("out/a.png");
    expect(workspacePath("/etc/passwd", "/w")).toBeNull();
    // Relative to where the agent was; the server confines it to the root.
    expect(workspacePath("../x.png", "/w")).toBe("../x.png");
    expect(workspacePath("https://x.test/a.png", "/w")).toBeNull();
  });
  it("finds loopback URLs and media paths in output", () => {
    const m = mentions(
      "Server at http://localhost:5173/ and saved /w/out/shot.png, see https://example.com",
      "/w",
    );
    expect(m.urls).toEqual(["http://localhost:5173/"]);
    expect(m.paths).toEqual(["out/shot.png"]);
  });
});

describe("live turn edge cases", () => {
  it("ignores in-progress turn summaries and keeps streaming text on reconcile", () => {
    const live = applyEvents(
      emptyState(),
      [
        ev("native", {
          method: "turn/completed",
          params: { turn: { id: "t1", status: "inProgress" } },
        }),
        ev("user", { text: "go" }),
        ev("delta", { channel: "text", item: "m1", text: "Partial answer" }),
      ],
      { agent: "codex", history: false },
    );
    expect(live.keys.some((k) => k.startsWith("turn:"))).toBe(false);
    const tail = fromHistory(
      [
        {
          seq: 0,
          session: "s",
          kind: "native",
          time: "",
          data: {
            method: "item/completed",
            params: {
              item: {
                id: "u1",
                type: "userMessage",
                content: [{ type: "text", text: "go" }],
              },
            },
          },
        },
      ],
      "codex",
      true,
    );
    const merged = reconcile(live, tail)!;
    expect(merged.keys).toEqual(["u1", "m1"]);
    expect(merged.blocks.get("m1")!.text).toBe("Partial answer");
  });
});

describe("todo panel and status line", () => {
  it("tracks the newest TodoWrite list and the running tool", () => {
    const todo = (statuses: string[]) => ({
      type: "assistant",
      uuid: "t" + statuses.join(""),
      message: {
        id: "m",
        content: [
          {
            type: "tool_use",
            id: "todo-" + statuses.join(""),
            name: "TodoWrite",
            input: {
              todos: statuses.map((status, i) => ({
                content: "step " + i,
                status,
                activeForm: "doing " + i,
              })),
            },
          },
        ],
      },
    });
    const s = applyEvents(
      emptyState(),
      [
        ev("native", todo(["in_progress", "pending"])),
        ev("native", todo(["completed", "in_progress"])),
        ev("native", claudeRecords.toolUse),
      ],
      { agent: "claude", history: false },
    );
    expect(latestPlan(s)!.map((x) => x.status)).toEqual([
      "completed",
      "in_progress",
    ]);
    expect(
      toolTitle(s.blocks.get("todo-completedin_progress")!.tool!).name,
    ).toBe("Update Todos");
    expect(activity(s, "running")).toBe("Running Bash(npm test)");
    expect(activity(s, "waiting")).toBe("Awaiting approval");
  });
});

describe("harness events", () => {
  const notification = `<task-notification>
<task-id>a47ea24edcf66f692</task-id>
<tool-use-id>toolu_01</tool-use-id>
<status>completed</status>
<summary>Agent "Devfee finance and revenue-share research" finished</summary>
<usage><subagent_tokens>183232</subagent_tokens><tool_uses>88</tool_uses><duration_ms>1221172</duration_ms></usage>
</task-notification>`;
  const record = (extra: Record<string, unknown>, content: string) =>
    ev("native", {
      type: "user",
      uuid: "n" + ++seq,
      message: { role: "user", content },
      ...extra,
    });
  it("shows task notifications as events, not user prompts", () => {
    const s = applyEvents(
      emptyState(),
      [
        record(
          { promptSource: "system", origin: { kind: "task-notification" } },
          notification,
        ),
        record(
          { origin: { kind: "peer" }, promptSource: "system" },
          'Another Claude session sent a message: <agent-message from="abc">Build is green</agent-message>',
        ),
        // Older transcripts without origin metadata.
        record({}, notification),
        record(
          { origin: { kind: "human" }, promptSource: "typed" },
          "fix the build",
        ),
      ],
      { agent: "claude", history: true },
    );
    const b = blocks(s);
    expect(b.map((x) => [x.kind, x.label])).toEqual([
      ["notice", "task"],
      ["notice", "peer"],
      ["notice", "task"],
      ["user", undefined],
    ]);
    expect(b[0].text).toBe(
      'Agent "Devfee finance and revenue-share research" finished · 88 tool uses · 183.2K tokens · 20m 21s',
    );
    expect(b[0].detail).toContain("<task-id>a47ea24edcf66f692</task-id>");
    expect(b[1].text).toBe("Message from another session abc: Build is green");
  });
});

describe("live progress", () => {
  it("names the phase when nothing more specific is known", () => {
    expect(statusVerb("Working", "thinking")).toBe("Thinking");
    expect(statusVerb("Working", "requesting")).toBe("Waiting for the model");
    expect(statusVerb("Running Bash(ls)", "thinking")).toBe("Running Bash(ls)");
    expect(statusVerb("Working", "")).toBe("Working");
    expect(statusVerb("Starting Codex", "mcp", { done: 1, total: 3 })).toBe(
      "Starting MCP servers (1/3)",
    );
    expect(compact(1800)).toBe("1.8k");
    expect(compact(190.8)).toBe("191");
  });
  it("times the turn from the send that started it", () => {
    expect(turnClock(10_000, 1_000)).toBe(1_000);
    expect(turnClock(10_000, null)).toBe(10_000);
    // An old send (or one after the turn began) does not move the clock.
    expect(turnClock(500_000, 1_000)).toBe(500_000);
    expect(turnClock(10_000, 20_000)).toBe(10_000);
    expect(turnClock(null, 1_000)).toBeNull();
  });
  it("shows a sent message until the server echoes it", () => {
    const s = withSending(emptyState(), "hello", "sending:1");
    expect(s.keys).toEqual(["sending:1"]);
    expect(s.blocks.get("sending:1")).toMatchObject({
      kind: "user",
      text: "hello",
      sending: true,
    });
  });
});

describe("steering", () => {
  it("shows a steered message in the turn, keeps the tag on Codex's echo, and takes back one that did not reach it", () => {
    const codex = { agent: "codex" as const, history: false };
    let s = applyEvents(
      emptyState(),
      [
        ev("user", { text: "fix the tests" }),
        ev("native", {
          method: "item/started",
          params: {
            item: {
              id: "u1",
              type: "userMessage",
              content: [{ type: "text", text: "fix the tests" }],
            },
          },
        }),
        ev("user", { text: "use vitest", steered: true }),
        ev("user", { text: "and too late", steered: true }),
        ev("retract", { text: "and too late" }),
      ],
      codex,
    );
    expect(blocks(s).map((b) => [b.text, !!b.steered])).toEqual([
      ["fix the tests", false],
      ["use vitest", true],
    ]);
    s = applyEvents(
      s,
      [
        ev("native", {
          method: "item/started",
          params: {
            item: {
              id: "u2",
              type: "userMessage",
              content: [{ type: "text", text: "use vitest" }],
            },
          },
        }),
      ],
      codex,
    );
    expect(blocks(s).map((b) => [b.key, b.local, !!b.steered])).toEqual([
      ["u1", false, false],
      ["u2", false, true],
    ]);
  });

  it("shows Claude's history record of a message taken in mid-turn as steered", () => {
    const s = applyEvents(
      emptyState(),
      [
        {
          seq: 0,
          session: "s",
          kind: "native",
          data: {
            type: "user",
            uuid: "q1",
            steered: true,
            history: true,
            message: { role: "user", content: "end with BANANA" },
          },
          time: "",
        },
      ],
      { agent: "claude", history: true },
    );
    expect(blocks(s).map((b) => [b.kind, b.text, b.steered])).toEqual([
      ["user", "end with BANANA", true],
    ]);
  });
});

describe("claude steer waiting", () => {
  it("waits for Claude's next step, then is taken in", () => {
    const claude = { agent: "claude" as const, history: false };
    let s = applyEvents(
      emptyState(),
      [
        ev("user", {
          text: "go left",
          steered: true,
          uuid: "u9",
          sendNow: true,
        }),
      ],
      claude,
    );
    expect(blocks(s).map((b) => [b.waiting, b.canSendNow])).toEqual([
      [true, true],
    ]);
    s = applyEvents(s, [ev("steer", { uuid: "u9", state: "queued" })], claude);
    expect(blocks(s)[0].waiting).toBe(true);
    s = applyEvents(s, [ev("steer", { uuid: "u9", state: "started" })], claude);
    expect(blocks(s).map((b) => [b.text, b.steered, b.waiting])).toEqual([
      ["go left", true, false],
    ]);
    // A stop cancels a waiting steer; it goes back to the queue.
    s = applyEvents(
      s,
      [
        ev("user", { text: "go right", steered: true, uuid: "u10" }),
        ev("steer", { uuid: "u10", state: "cancelled" }),
      ],
      claude,
    );
    expect(blocks(s).map((b) => b.text)).toEqual(["go left"]);
  });
});

describe("Codex async questions in the transcript", () => {
  it("reads the questions of an async agent message, as Codex 0.167 sends it", () => {
    // Recorded from a real request_user_input_async call.
    const s = applyEvents(
      emptyState(),
      [
        ev("native", {
          method: "item/completed",
          params: {
            item: {
              id: "call_3d85",
              type: "agentMessage",
              phase: "final_answer",
              delivery: "async",
              text: "Which color should the button be?\n- Red\n- Green\n- Blue\n\nWhat should the button say?",
              questions: [
                {
                  title: "Which color should the button be?",
                  options: ["Red", "Green", "Blue"],
                },
                { title: "What should the button say?", options: null },
              ],
            },
          },
        }),
        ev("native", {
          method: "item/completed",
          params: {
            item: { id: "plain", type: "agentMessage", text: "Done." },
          },
        }),
      ],
      { agent: "codex", history: false },
    );
    expect(s.blocks.get("call_3d85")!.questions).toEqual([
      {
        title: "Which color should the button be?",
        options: [{ label: "Red" }, { label: "Green" }, { label: "Blue" }],
      },
      { title: "What should the button say?", options: undefined },
    ]);
    expect(s.blocks.get("plain")!.questions).toBeUndefined();
  });
});

describe("questions cut off when the engine stops", () => {
  it("turns Claude's waiting AskUserQuestion into an answerable one", () => {
    const live = applyEvents(
      emptyState(),
      [
        ev("native", {
          type: "assistant",
          uuid: "a",
          message: {
            id: "m",
            content: [
              {
                type: "tool_use",
                id: "ask",
                name: "AskUserQuestion",
                input: {
                  questions: [
                    {
                      question: "Which size should the logo be?",
                      header: "Size",
                      multiSelect: false,
                      options: [{ label: "Small" }, { label: "Large" }],
                    },
                  ],
                },
              },
            ],
          },
        }),
      ],
      { agent: "claude", history: false },
    );
    const order = (s: State) => s.keys.map((k) => s.blocks.get(k)!);
    // Waiting on the approval card while the engine runs.
    expect(live.blocks.get("ask")!.tool!.status).toBe("running");
    expect(askStates(order(live)).get("ask")!.status).toBe("pending");
    // The engine stopped (status sleeping, stopped or error): settled.
    const after = settled(live);
    expect(after.blocks.get("ask")!.tool!.status).toBe("interrupted");
    expect(askStates(order(after)).get("ask")!.status).toBe("unanswered");
  });
});
