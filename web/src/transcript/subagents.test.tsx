import { describe, expect, it } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";
import { applyEvents, CHILD_TOOLS, emptyState } from "./reducer";
import { subagentKey } from "./codex";
import { ToolRow } from "./Timeline";
import { EnvContext, type Env } from "./media";
import { ApprovalCard } from "../approval/ApprovalCard";
import type { Agent, Block } from "./model";

const run = (
  agent: Agent,
  data: Record<string, unknown>[],
  history = false,
  state = emptyState(),
) =>
  applyEvents(
    state,
    data.map((d, i) => ({
      seq: i + 1,
      session: "s",
      kind: "native",
      data: d,
      time: "",
    })),
    { agent, history },
  );

const agentCall = {
  type: "assistant",
  uuid: "m1",
  message: {
    id: "msg1",
    role: "assistant",
    content: [
      {
        type: "tool_use",
        id: "toolu_A",
        name: "Agent",
        input: { description: "Look around", prompt: "ls" },
      },
    ],
  },
};
const big = "x".repeat(20000);
const subCall = (id: string) => ({
  type: "assistant",
  uuid: "sa" + id,
  parent_tool_use_id: "toolu_A",
  message: {
    role: "assistant",
    content: [
      {
        type: "tool_use",
        id,
        name: "Bash",
        input: { command: "echo " + id, description: big },
      },
    ],
  },
});
const subResult = (id: string) => ({
  type: "user",
  uuid: "su" + id,
  parent_tool_use_id: "toolu_A",
  tool_use_result: { stdout: big },
  message: {
    role: "user",
    content: [{ type: "tool_result", tool_use_id: id, content: "out " + big }],
  },
});

describe("Claude subagents", () => {
  it("nests the subagent's calls from history under its Agent row", () => {
    const s = run(
      "claude",
      [
        agentCall,
        {
          type: "workbench_subagent",
          history: true,
          tool_use_id: "toolu_A",
          agentId: "a1",
          earlier: 7,
        },
        subCall("t0"),
        subResult("t0"),
      ],
      true,
    );
    const row = s.blocks.get("toolu_A")!;
    expect([row.tool!.agentId, row.earlier]).toEqual(["a1", 7]);
    const line = s.blocks.get("t0")!;
    expect(line.parent).toBe("toolu_A");
    expect(line.tool!.status).toBe("ok");
    // Only what a line shows.
    expect(line.tool!.raw).toBeUndefined();
    expect(line.tool!.result).toBeUndefined();
    expect(line.tool!.output!.length).toBeLessThanOrEqual(300);
    const input = line.tool!.input as Record<string, string>;
    expect(input.description.length).toBeLessThanOrEqual(1025);
  });

  it("learns the subagent live from its task and the Agent result", () => {
    let s = run("claude", [
      agentCall,
      {
        type: "system",
        subtype: "task_started",
        task_id: "a9",
        tool_use_id: "toolu_A",
        task_type: "local_agent",
      },
    ]);
    expect(s.blocks.get("toolu_A")!.tool!.agentId).toBe("a9");
    s = run("claude", [
      agentCall,
      {
        type: "user",
        uuid: "r1",
        tool_use_result: { agentId: "a2", status: "async_launched" },
        message: {
          role: "user",
          content: [
            { type: "tool_result", tool_use_id: "toolu_A", content: "ok" },
          ],
        },
      },
    ]);
    expect(s.blocks.get("toolu_A")!.tool!.agentId).toBe("a2");
  });

  it(`keeps a row's latest ${CHILD_TOOLS} lines and counts the rest`, () => {
    const calls = Array.from({ length: CHILD_TOOLS + 50 }, (_, i) =>
      subCall("t" + i),
    );
    // In several batches, as live records arrive.
    let s = run("claude", [agentCall]);
    for (let i = 0; i < calls.length; i += 40)
      s = run("claude", calls.slice(i, i + 40), false, s);
    const lines = s.keys.filter((k) => s.blocks.get(k)!.parent === "toolu_A");
    expect(lines.length).toBe(CHILD_TOOLS);
    expect(lines[0]).toBe("t50");
    expect(s.blocks.get("toolu_A")!.earlier).toBe(50);
    expect(s.blocks.size).toBe(s.keys.length);
  });
});

describe("Codex subagents", () => {
  const activity = (kind: string) => ({
    method: "item/completed",
    params: {
      item: {
        type: "subAgentActivity",
        id: "act-" + kind,
        kind,
        agentThreadId: "kid",
        agentPath: "/root/inspect_files",
      },
    },
  });
  const child = (id: string) => ({
    method: "item/completed",
    workbenchChild: "kid",
    params: {
      threadId: "kid",
      item: {
        type: "commandExecution",
        id,
        command: "ls -la",
        status: "completed",
        aggregatedOutput: "y".repeat(300),
      },
    },
  });

  it("shows a subagent as a row with its tool calls under it", () => {
    const s = run("codex", [
      activity("started"),
      child("c1"),
      {
        method: "workbench/subagent",
        history: true,
        params: { threadId: "kid", earlier: 3, more: true },
      },
      activity("completed"),
    ]);
    const row = s.blocks.get(subagentKey("kid"))!;
    expect([row.tool!.name, row.tool!.threadId, row.tool!.status]).toEqual([
      "Agent",
      "kid",
      "ok",
    ]);
    expect([row.earlier, row.earlierMore]).toEqual([3, true]);
    expect(s.blocks.get("c1")!.parent).toBe(subagentKey("kid"));
    // One row per subagent, not a line per activity.
    expect(s.keys.filter((k) => k.startsWith("act-"))).toEqual([]);
  });

  it("makes the row when a subagent's line comes first", () => {
    const s = run("codex", [child("c1")]);
    expect(s.blocks.get(subagentKey("kid"))!.tool!.status).toBe("running");
    expect(s.blocks.get("c1")!.parent).toBe(subagentKey("kid"));
  });
});

const env = (listed: string[]): Env => ({
  cwd: "/p",
  openUrl: () => {},
  openPath: () => {},
  thumb: () => null,
  reveal: () => {},
  view: () => {},
  subagentSession: ({ agentId, threadId }) => {
    const id = agentId ? `claude:s1:agent-${agentId}` : `codex:${threadId}`;
    return listed.includes(id) ? id : undefined;
  },
  listed: (id) => listed.includes(id),
  openSession: () => {},
});

describe("links to subagents", () => {
  const row = (b: Block, kids: Block[], listed: string[]) =>
    renderToStaticMarkup(
      <EnvContext.Provider value={env(listed)}>
        <ToolRow block={b} kids={kids} open={false} onToggle={() => {}} />
      </EnvContext.Provider>,
    );
  const agentRow: Block = {
    key: "toolu_A",
    kind: "tool",
    earlier: 10,
    tool: {
      name: "Agent",
      status: "ok",
      input: { description: "Look around" },
      agentId: "a1",
    },
  };
  const kids: Block[] = Array.from({ length: 5 }, (_, i) => ({
    key: "t" + i,
    kind: "tool",
    parent: "toolu_A",
    tool: { name: "Bash", status: "ok", input: { command: "echo " + i } },
  }));

  it("links the row once the subagent's session is listed", () => {
    expect(row(agentRow, kids, [])).not.toContain("Transcript");
    const html = row(agentRow, kids, ["claude:s1:agent-a1"]);
    expect(html).toContain("Transcript");
    // 2 folded and 10 not kept.
    expect(html).toContain("+12");
    expect(html).toContain("more tool uses");
  });

  it("names the subagent on its request", () => {
    const card = (listed: string[]) =>
      renderToStaticMarkup(
        <EnvContext.Provider value={env(listed)}>
          <ApprovalCard
            event={{
              seq: 1,
              session: "s",
              kind: "approval",
              time: "",
              data: {
                id: "p1",
                request: {
                  request: {
                    subtype: "can_use_tool",
                    tool_name: "Bash",
                    input: { command: "make" },
                  },
                },
                subagent: {
                  session: "claude:s1:agent-a9",
                  description: "Check the build",
                },
              },
            }}
            agent="claude"
            lookup={() => undefined}
            reply={async () => {}}
            onInterrupt={() => {}}
          />
        </EnvContext.Provider>,
      );
    const html = card(["claude:s1:agent-a9"]);
    expect(html).toContain("From subagent");
    expect(html).toContain("Check the build");
    expect(html).toContain("Transcript");
    expect(card([])).not.toContain("Transcript");
  });
});
