import { describe, expect, it } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";
import { content, fields, parseElicitation } from "./elicitation";
import { parseRequest, permissionItems, planChoices } from "./ApprovalCard";
import { ElicitationForm } from "./ElicitationForm";
import { applyEvents, emptyState } from "../transcript/reducer";
import { changedFiles } from "../transcript/codex";
import { toolSummary } from "../transcript/summarize";
import type { Block } from "../transcript/model";

const schema = {
  type: "object",
  properties: {
    env: { type: "string", title: "Environment", enum: ["staging", "prod"] },
    region: {
      type: "string",
      oneOf: [
        { const: "eu", title: "Europe" },
        { const: "us", title: "United States" },
      ],
    },
    tags: { type: "array", items: { type: "string", enum: ["a", "b"] } },
    replicas: { type: "integer", minimum: 1, default: 2 },
    confirm: { type: "boolean", title: "I understand" },
    note: { type: "string", format: "email" },
  },
  required: ["env", "confirm"],
};

describe("MCP elicitation", () => {
  it("reads a Codex form", () => {
    const e = parseElicitation("codex", {
      method: "mcpServer/elicitation/request",
      params: {
        serverName: "deploy",
        mode: "form",
        message: "Where to?",
        requestedSchema: schema,
      },
    })!;
    expect(e.mode).toBe("form");
    expect(e.server).toBe("deploy");
    expect(e.fields.map((f) => [f.name, f.type, f.required])).toEqual([
      ["env", "select", true],
      ["region", "select", false],
      ["tags", "multi", false],
      ["replicas", "integer", false],
      ["confirm", "boolean", true],
      ["note", "text", false],
    ]);
    expect(e.fields[1].options).toEqual([
      { value: "eu", label: "Europe" },
      { value: "us", label: "United States" },
    ]);
  });

  it("reads Claude's, in the server's order, and URLs", () => {
    const e = parseElicitation("claude", {
      type: "control_request",
      request: {
        subtype: "elicitation",
        mcp_server_name: "deploy",
        message: "Where to?",
        mode: "form",
        requested_schema: schema,
        property_order: ["confirm", "env"],
      },
    })!;
    expect(e.fields.map((f) => f.name)).toEqual(["confirm", "env"]);
    const url = parseElicitation("codex", {
      method: "mcpServer/elicitation/request",
      params: {
        serverName: "auth",
        mode: "url",
        message: "Sign in",
        url: "https://example.com/login",
        elicitationId: "e1",
      },
    })!;
    expect([url.mode, url.url]).toEqual(["url", "https://example.com/login"]);
    const verify = parseElicitation("codex", {
      method: "mcpServer/elicitation/request",
      params: {
        serverName: "auth",
        mode: "openai/userVerification",
        title: "Verify",
        description: "Confirm on your device",
        challenge: "x",
      },
    })!;
    expect(verify.mode).toBe("unsupported");
    expect(
      parseElicitation("claude", { request: { subtype: "can_use_tool" } }),
    ).toBeUndefined();
  });

  it("sends each field in its type, and needs the required ones", () => {
    const form = fields(schema);
    expect(content(form, { env: "prod" })).toEqual({
      content: { env: "prod", replicas: 2, confirm: false },
      complete: true,
    });
    expect(
      content(form, {
        env: "prod",
        replicas: "3.7",
        tags: ["b"],
        note: "a@b.c",
        confirm: true,
      }),
    ).toEqual({
      content: {
        env: "prod",
        replicas: 3,
        tags: ["b"],
        note: "a@b.c",
        confirm: true,
      },
      complete: true,
    });
    expect(content(form, {}).complete).toBe(false);
  });

  it("renders the form and the link", () => {
    const e = parseElicitation("codex", {
      method: "mcpServer/elicitation/request",
      params: {
        serverName: "deploy",
        mode: "form",
        message: "Where to?",
        requestedSchema: schema,
      },
    })!;
    const html = renderToStaticMarkup(
      <ElicitationForm
        elicitation={e}
        busy={false}
        onAccept={() => {}}
        onDecline={() => {}}
        onCancel={() => {}}
      />,
    );
    expect(html).toContain("Environment");
    expect(html).toContain("United States");
    expect(html).toContain('type="email"');
    expect(html).toContain("Decline");
  });
});

describe("Codex permission requests", () => {
  it("lists what is asked for", () => {
    expect(
      permissionItems({
        network: { enabled: true },
        fileSystem: {
          entries: [
            { access: "write", path: { type: "path", path: "/srv/app" } },
            {
              access: "read",
              path: { type: "special", value: { kind: "tmpdir" } },
            },
          ],
        },
      }),
    ).toEqual([
      "Network access",
      "Write /srv/app",
      "Read the temporary directory",
    ]);
    const parsed = parseRequest(
      "codex",
      {
        method: "item/permissions/requestApproval",
        params: {
          reason: "fetch docs",
          permissions: { network: { enabled: true } },
        },
      },
      () => undefined,
    );
    expect(parsed).toEqual({
      kind: "permissions",
      items: ["Network access"],
      reason: "fetch docs",
    });
  });
});

const run = (agent: "claude" | "codex", ...data: Record<string, unknown>[]) =>
  applyEvents(
    emptyState(),
    data.map((d, i) => ({
      seq: i + 1,
      session: "s",
      kind: "native",
      data: d,
      time: "",
    })),
    { agent, history: false },
  );
const blocks = (s: ReturnType<typeof run>): Block[] =>
  s.keys.map((k) => s.blocks.get(k)!);
const notices = (s: ReturnType<typeof run>) =>
  blocks(s)
    .filter((b) => b.kind === "notice")
    .map((b) => [b.text, b.tone]);

describe("Claude system records", () => {
  it("marks a step auto mode blocked on its tool", () => {
    const s = run(
      "claude",
      {
        type: "assistant",
        message: {
          id: "m1",
          role: "assistant",
          content: [
            {
              type: "tool_use",
              id: "toolu_1",
              name: "Bash",
              input: { command: "rm -rf /" },
            },
          ],
        },
      },
      {
        type: "system",
        subtype: "permission_denied",
        tool_name: "Bash",
        tool_use_id: "toolu_1",
        decision_reason_type: "classifier",
      },
    );
    const tool = s.blocks.get("toolu_1")!.tool!;
    expect([tool.status, tool.note]).toEqual([
      "declined",
      "Blocked by auto mode: its safety check said no",
    ]);
    expect(toolSummary(tool).lines).toEqual([
      "Blocked by auto mode: its safety check said no",
    ]);
  });

  it("shows warnings, model switches, failed hooks and long instructions once", () => {
    const s = run(
      "claude",
      {
        type: "system",
        subtype: "informational",
        level: "info",
        content: "transcript only",
      },
      {
        type: "system",
        subtype: "informational",
        level: "warning",
        content: "A hook blocked the turn 9 times",
      },
      {
        type: "system",
        subtype: "notification",
        key: "cap",
        priority: "high",
        text: "A hook blocked the turn 9 times",
      },
      {
        type: "system",
        subtype: "notification",
        key: "low",
        priority: "low",
        text: "quiet",
      },
      {
        type: "system",
        subtype: "model_fallback",
        trigger: "overloaded",
        original_model: "claude-opus-5-5",
        fallback_model: "claude-sonnet-5-5",
      },
      {
        type: "system",
        subtype: "hook_response",
        hook_event: "UserPromptSubmit",
        hook_name: "lint",
        outcome: "error",
        exit_code: 2,
        stderr: "lint failed\nmore",
      },
      // Shown in the tool's result already.
      {
        type: "system",
        subtype: "hook_response",
        hook_event: "PreToolUse",
        hook_name: "PreToolUse:Bash",
        outcome: "error",
        exit_code: 2,
        stderr: "refused",
      },
      {
        type: "system",
        subtype: "hook_response",
        hook_event: "Stop",
        hook_name: "Stop",
        outcome: "success",
        exit_code: 0,
      },
      {
        type: "system",
        subtype: "instruction_size_warning",
        total_chars: 52000,
        total_limit_chars: 40000,
        file_count: 3,
      },
    );
    expect(notices(s)).toEqual([
      ["A hook blocked the turn 9 times", "warn"],
      [
        "Switched from claude-opus-5-5 to claude-sonnet-5-5 (the model is overloaded)",
        "warn",
      ],
      [
        "The UserPromptSubmit hook (lint) failed with exit code 2: lint failed",
        "warn",
      ],
      [
        "Instruction files (CLAUDE.md and rules, 3 files) are 52,000 characters, over the 40,000 recommended for this model",
        "warn",
      ],
    ]);
  });
});

describe("Codex records", () => {
  const diff = (files: string[]) =>
    files
      .map(
        (f) =>
          `diff --git a/${f} b/${f}\n--- a/${f}\n+++ b/${f}\n@@ -1 +1 @@\n-old\n+new\n`,
      )
      .join("");

  it("shows the turn's changes after its latest change", () => {
    expect(changedFiles(diff(["a.ts", "b.ts"]))).toEqual(["a.ts", "b.ts"]);
    const s = run(
      "codex",
      {
        method: "turn/diff/updated",
        params: { turnId: "t1", diff: diff(["a.ts"]) },
      },
      {
        method: "item/completed",
        params: {
          item: { type: "agentMessage", id: "msg1", text: "Edited a.ts" },
        },
      },
      {
        method: "turn/diff/updated",
        params: { turnId: "t1", diff: diff(["a.ts", "b.ts", "c.ts", "d.ts"]) },
      },
    );
    const keys = s.keys;
    expect(keys[keys.length - 1]).toBe("changes:t1");
    const tool = s.blocks.get("changes:t1")!.tool!;
    expect(tool.input).toEqual({ files: "a.ts, b.ts, c.ts and 1 more" });
    expect(tool.diff).toContain("d.ts");
  });

  it("shows MCP progress and hooks that failed", () => {
    const s = run(
      "codex",
      {
        method: "item/started",
        params: {
          item: {
            type: "mcpToolCall",
            id: "call1",
            server: "docs",
            tool: "search",
            status: "inProgress",
            arguments: {},
          },
        },
      },
      {
        method: "item/mcpToolCall/progress",
        params: { itemId: "call1", message: "Indexed 40 of 120 pages" },
      },
      {
        method: "hook/completed",
        params: {
          run: {
            id: "h1",
            eventName: "preToolUse",
            status: "blocked",
            entries: [{ kind: "stop", text: "no deploys on Friday" }],
          },
        },
      },
      {
        method: "hook/completed",
        params: {
          run: {
            id: "h2",
            eventName: "postToolUse",
            status: "completed",
            entries: [],
          },
        },
      },
    );
    expect(s.blocks.get("call1")!.tool!.progress).toBe(
      "Indexed 40 of 120 pages",
    );
    expect(notices(s)).toEqual([
      ["The preToolUse hook blocked this step: no deploys on Friday", "warn"],
    ]);
  });
});

describe("an approved Claude plan", () => {
  it("offers auto mode first to a session that ran in it, and says what each choice means", () => {
    const back = planChoices(true);
    expect(back.modes).toEqual(["auto", "acceptEdits", "default"]);
    expect(back.options.map((o) => o.label)).toEqual([
      "Yes, and use auto mode",
      "Yes, auto-accept edits",
      "Yes, manually approve edits",
      "No, keep planning",
    ]);
    expect(back.options.every((o) => o.hint)).toBe(true);
    expect(back.options[1].hint).toContain("commands still ask");
    const plain = planChoices(false);
    expect(plain.modes).toEqual(["acceptEdits", "default"]);
    expect(plain.options[0].label).toBe("Yes, auto-accept edits");
    // The last choice keeps planning; the modes line up with the Yes ones.
    expect(plain.options).toHaveLength(plain.modes.length + 1);
    expect(back.options).toHaveLength(back.modes.length + 1);
  });
});
