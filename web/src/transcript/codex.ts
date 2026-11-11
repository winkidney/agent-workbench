// Codex app-server adapter. Works on ThreadItem.type + id; unknown item
// types become generic tool rows, so new Codex tools render without changes.
import {
  list,
  record,
  text,
  type Media,
  type StepStatus,
  type Tool,
  type ToolStatus,
} from "./model";
import type { Context, Draft } from "./reducer";
import { asyncQuestions } from "./questions";

const num = (v: unknown) => (typeof v === "number" ? v : undefined);

function status(v: unknown, completed: boolean): ToolStatus {
  switch (text(v)) {
    case "inProgress":
    case "in_progress":
      return completed ? "ok" : "running";
    case "completed":
      return "ok";
    case "failed":
    case "error":
      return "failed";
    case "declined":
    case "rejected":
      return "declined";
    case "interrupted":
    case "cancelled":
      return "interrupted";
  }
  return completed ? "ok" : "running";
}

// Text from Codex / MCP content arrays without depending on each item schema.
export function outputText(v: unknown): string {
  if (typeof v === "string") return v;
  if (Array.isArray(v))
    return v
      .map((x) => {
        const c = record(x);
        return typeof x === "string" ? x : text(c.text ?? c.content ?? "");
      })
      .filter(Boolean)
      .join("\n");
  const r = record(v);
  if (r.content !== undefined) return outputText(r.content);
  if (typeof r.text === "string") return r.text;
  return v == null ? "" : JSON.stringify(v, null, 2);
}

function outputMedia(v: unknown): Media[] {
  const items = Array.isArray(v) ? v : list(record(v).content);
  return items
    .map(record)
    .filter((c) => c.type === "image" || c.type === "inputImage")
    .map((c): Media =>
      c.data
        ? {
            kind: "data",
            src: `data:${text(c.mimeType) || "image/png"};base64,${text(c.data)}`,
          }
        : { kind: "url", url: text(c.url ?? c.imageUrl) },
    );
}

export function codexTool(
  it: Record<string, unknown>,
  completed: boolean,
): Tool {
  const type = text(it.type);
  const base = { nativeType: type, raw: it, durationMs: num(it.durationMs) };
  const s = status(it.status, completed);
  switch (type) {
    case "commandExecution": {
      const exitCode = num(it.exitCode);
      return {
        ...base,
        name: "Shell",
        input: { command: it.command, cwd: it.cwd, actions: it.commandActions },
        status:
          s === "ok" && exitCode !== undefined && exitCode !== 0 ? "failed" : s,
        output: text(it.aggregatedOutput) || undefined,
        exitCode,
      };
    }
    case "fileChange": {
      const changes = list(it.changes).map(record);
      const added = changes.every((c) => record(c.kind).type === "add");
      return {
        ...base,
        name: added ? "Write" : "Edit",
        input: { paths: changes.map((c) => text(c.path)) },
        status: s,
        diff: changes
          .map(
            (c) =>
              `--- a/${text(c.path)}\n+++ b/${text(c.path)}\n${text(c.diff)}`,
          )
          .join("\n"),
      };
    }
    case "mcpToolCall": {
      const error = record(it.error);
      return {
        ...base,
        name: `${text(it.server)} · ${text(it.tool)}`,
        input: it.arguments,
        status: error.message ? "failed" : s,
        output: text(error.message) || outputText(it.result) || undefined,
        media: outputMedia(it.result),
      };
    }
    case "dynamicToolCall":
      return {
        ...base,
        name: text(it.tool) || type,
        input: it.arguments,
        status: it.success === false ? "failed" : s,
        output: outputText(it.contentItems) || undefined,
        media: outputMedia(it.contentItems),
      };
    case "collabAgentToolCall":
      return {
        ...base,
        name: "Agent",
        input: { description: it.tool, prompt: it.prompt, model: it.model },
        status: s,
      };
    case "webSearch": {
      const action = record(it.action);
      return {
        ...base,
        name: "WebSearch",
        input: { query: it.query || action.query, url: action.url },
        status: s,
        output:
          list(it.results)
            .map(record)
            .map((r) =>
              [text(r.title), text(r.url)].filter(Boolean).join(" — "),
            )
            .join("\n") || undefined,
      };
    }
    case "imageView":
      return {
        ...base,
        name: "ViewImage",
        input: { path: it.path },
        status: s,
        media: it.path ? [{ kind: "path", path: text(it.path) }] : undefined,
      };
    case "imageGeneration":
      return {
        ...base,
        name: "GenerateImage",
        input: { prompt: it.revisedPrompt },
        status: it.failure ? "failed" : s,
        output: it.failure ? outputText(it.failure) : undefined,
        media: it.savedPath
          ? [{ kind: "path", path: text(it.savedPath) }]
          : undefined,
      };
    case "functionCallOutput":
      return {
        ...base,
        name: text(it.name) || type,
        status: s,
        output: outputText(it.output) || undefined,
      };
  }
  // Generic: any future tool-like item.
  const { id: _id, type: _type, status: st, ...input } = it;
  return {
    ...base,
    name: type || "item",
    input,
    status: status(st, completed),
    output: outputText(it.output ?? it.result) || undefined,
  };
}

const stepStatus = (v: unknown): StepStatus =>
  v === "completed"
    ? "completed"
    : v === "inProgress" || v === "in_progress"
      ? "in_progress"
      : "pending";

function item(
  d: Draft,
  it: Record<string, unknown>,
  completed: boolean,
  ctx: Context,
) {
  const id = text(it.id);
  if (!id) return;
  const type = text(it.type);
  switch (type) {
    case "userMessage": {
      const parts = list(it.content).map(record);
      const body = parts
        .filter((c) => c.type === "text")
        .map((c) => text(c.text))
        .join("\n");
      const media = parts.flatMap((c): Media[] =>
        c.type === "image"
          ? [{ kind: "url", url: text(c.url) }]
          : c.type === "localImage"
            ? [{ kind: "path", path: text(c.path) }]
            : [],
      );
      const block = {
        key: id,
        kind: "user" as const,
        text: body,
        media: media.length ? media : undefined,
        local: false,
        // History marks a later user message in a turn as steered into it.
        steered: it.workbenchSteered === true || undefined,
      };
      const echo = ctx.history
        ? undefined
        : d.find((b) => b.local === true && b.kind === "user");
      if (echo)
        d.replace(echo.key, {
          ...block,
          steered: block.steered ?? echo.steered,
        });
      else d.upsert(block);
      return;
    }
    case "agentMessage": {
      const body = text(it.text);
      const questions = asyncQuestions(it);
      if (body || questions || d.get(id))
        d.upsert({
          key: id,
          kind: "assistant",
          text: body || undefined,
          streaming: !completed,
          questions,
        });
      return;
    }
    case "reasoning": {
      const summary = list(it.summary).map(text).filter(Boolean);
      const body = (summary.length ? summary : list(it.content).map(text)).join(
        "\n\n",
      );
      if (completed && !body && !d.get(id)?.text) return d.remove(id);
      if (body || d.get(id))
        d.upsert({
          key: id,
          kind: "thinking",
          text: body || undefined,
          streaming: !completed,
        });
      return;
    }
    case "plan":
      d.upsert({
        key: id,
        kind: "plan",
        text: text(it.text),
        streaming: !completed,
      });
      return;
    case "contextCompaction":
      d.upsert({ key: id, kind: "notice", text: "Context compacted" });
      return;
    case "enteredReviewMode":
    case "exitedReviewMode":
      d.upsert({
        key: id,
        kind: "notice",
        text:
          (type === "enteredReviewMode"
            ? "Entered review mode"
            : "Exited review mode") +
          (it.review ? ` · ${text(it.review)}` : ""),
      });
      return;
    case "subAgentActivity":
      d.upsert({
        key: id,
        kind: "notice",
        text: [text(it.agentPath), text(it.kind)].filter(Boolean).join(" · "),
      });
      return;
    case "hookPrompt":
      d.upsert({ key: id, kind: "notice", text: "Hook prompt" });
      return;
  }
  d.upsert({ key: id, kind: "tool", tool: codexTool(it, completed) });
}

export function applyCodex(
  d: Draft,
  data: Record<string, unknown>,
  ctx: Context,
) {
  const p = record(data.params);
  const method = text(data.method);
  switch (method) {
    case "item/started":
    case "item/completed":
      return item(d, record(p.item), method === "item/completed", ctx);
    case "turn/plan/updated": {
      const key = "plan:" + text(p.turnId);
      d.remove(key);
      d.upsert({
        key,
        kind: "plan",
        local: true,
        text: text(p.explanation) || undefined,
        steps: list(p.plan)
          .map(record)
          .map((s) => ({ text: text(s.step), status: stepStatus(s.status) })),
      });
      return;
    }
    case "turn/completed": {
      const turn = record(p.turn);
      const id = text(turn.id);
      if (!id || turn.status === "inProgress") return;
      if (!ctx.history)
        d.settle(turn.status === "completed" ? "ok" : "interrupted");
      d.upsert({
        key: "turn:" + id,
        kind: "turn",
        durationMs: num(turn.durationMs),
        text: text(turn.status),
      });
      const error = record(turn.error);
      if (turn.status === "failed" && error.message)
        d.upsert({
          key: "turn-error:" + id,
          kind: "error",
          text: text(error.message),
        });
      if (turn.status === "interrupted")
        d.upsert({
          key: "turn-interrupted:" + id,
          kind: "notice",
          text: "Interrupted by user",
        });
      // A turn another client was still running can read back as
      // interrupted; its real end replaces that.
      else d.remove("turn-interrupted:" + id);
      return;
    }
    case "error": {
      const message = text(record(p.error).message) || text(p.message);
      if (message)
        d.upsert({
          key: "error:" + ctx.seq,
          kind: "error",
          text: message,
          local: true,
        });
      return;
    }
    case "warning":
    case "configWarning":
    case "deprecationNotice":
    case "guardianWarning": {
      const message = text(p.message) || text(p.summary);
      // Startup warnings repeat on every resume; keep one per text.
      if (message)
        d.upsert({
          key: "warn:" + message.slice(0, 200),
          kind: "notice",
          tone: "warn",
          text: message,
          local: true,
        });
      return;
    }
    case "thread/compacted":
      d.upsert({
        key: "compacted:" + ctx.seq,
        kind: "notice",
        text: "Context compacted",
        local: true,
      });
      return;
    case "model/rerouted":
      d.upsert({
        key: "rerouted:" + ctx.seq,
        kind: "notice",
        tone: "warn",
        text: `Model rerouted to ${text(p.toModel)}`,
        local: true,
      });
      return;
  }
}
