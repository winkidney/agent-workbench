// Claude Code stream-json / transcript JSONL adapter. Works on Anthropic
// content block types (text, thinking, tool_use, tool_result, image); unknown
// block types stay visible as generic rows.
import {
  formatDuration,
  list,
  record,
  text,
  type Block,
  type Media,
  type Step,
  type StepStatus,
  type ToolStatus,
} from "./model";
import type { Context, Draft } from "./reducer";
import { stripAnsi } from "./ansi";

export function imageMedia(c: Record<string, unknown>): Media {
  if (c.omitted === true)
    return {
      kind: "omitted",
      bytes: Number(c.bytes),
      upload: text(c.upload) || undefined,
    };
  const source = record(c.source);
  if (source.type === "url") return { kind: "url", url: text(source.url) };
  const mime = text(source.media_type || c.mimeType) || "image/png";
  const data = text(source.data || c.data);
  return { kind: "data", src: `data:${mime};base64,${data}`, mime };
}

export function contentText(v: unknown): string {
  if (typeof v === "string") return v;
  return list(v)
    .map((x) => {
      const c = record(x);
      if (typeof x === "string") return x;
      if (c.type === "image") return "";
      return text(c.text ?? c.content ?? "");
    })
    .filter(Boolean)
    .join("\n");
}

export function contentMedia(v: unknown): Media[] {
  return list(v)
    .map(record)
    .filter((c) => c.type === "image")
    .map(imageMedia);
}

export function todoSteps(input: unknown): Step[] | undefined {
  const todos = record(input).todos;
  if (!Array.isArray(todos)) return undefined;
  const steps = todos.map(record).map((t) => ({
    text: text(t.content ?? t.step ?? t.text),
    status: (["pending", "in_progress", "completed"].includes(text(t.status))
      ? t.status
      : "pending") as StepStatus,
  }));
  return steps.every((s) => s.text) ? steps : undefined;
}

const tag = (s: string, name: string) =>
  new RegExp(`<${name}>([\\s\\S]*?)</${name}>`).exec(s)?.[1];
// eslint-disable-next-line no-control-regex

// Claude Code wraps slash commands and local output in tags; show them the way the TUI does.
export function shapeUserText(raw: string): {
  kind: "user" | "notice" | "skip";
  text: string;
} {
  const s = raw
    .replace(/<system-reminder>[\s\S]*?<\/system-reminder>/g, "")
    .trim();
  if (!s) return { kind: "skip", text: "" };
  const command = tag(s, "command-name");
  if (command !== undefined) {
    const args = tag(s, "command-args")?.trim();
    return {
      kind: "user",
      text: [command.trim(), args].filter(Boolean).join(" "),
    };
  }
  const stdout = tag(s, "local-command-stdout");
  if (stdout !== undefined) {
    const out = stripAnsi(stdout).trim();
    return out ? { kind: "notice", text: out } : { kind: "skip", text: "" };
  }
  if (tag(s, "local-command-caveat") !== undefined)
    return { kind: "skip", text: "" };
  const bash = tag(s, "bash-input");
  if (bash !== undefined) return { kind: "user", text: "! " + bash.trim() };
  const out = tag(s, "bash-stdout") ?? tag(s, "bash-stderr");
  if (out !== undefined)
    return out.trim()
      ? { kind: "notice", text: stripAnsi(out).trim() }
      : { kind: "skip", text: "" };
  if (/^\[Request interrupted by user/.test(s))
    return { kind: "notice", text: "Interrupted by user" };
  return { kind: "user", text: s };
}

// User records the harness produced (task notifications, peer messages) are
// events, not prompts. Claude Code marks them with origin / promptSource.
export function injectedKind(
  data: Record<string, unknown>,
  raw: string,
): string | undefined {
  const kind = text(record(data.origin).kind);
  if (kind && kind !== "human") return kind;
  if (data.promptSource === "system") return kind || "system";
  if (/^\s*<task-notification>/.test(raw)) return "task-notification";
  return undefined;
}

const stripTags = (s: string) =>
  s
    .replace(/<[^>]+>/g, " ")
    .replace(/\s+/g, " ")
    .trim();
const compact = (n: number) =>
  new Intl.NumberFormat("en", {
    notation: "compact",
    maximumFractionDigits: 1,
  }).format(n);

export function systemNotice(
  kind: string,
  raw: string,
): { label: string; text: string; tone?: "warn" } {
  if (kind === "task-notification") {
    const summary = tag(raw, "summary")?.trim();
    const status = tag(raw, "status")?.trim() ?? "";
    const tools = Number(tag(raw, "tool_uses"));
    const tokens = Number(
      tag(raw, "subagent_tokens") ?? tag(raw, "total_tokens"),
    );
    const ms = Number(tag(raw, "duration_ms"));
    const parts = [summary || `Background task ${status || "updated"}`];
    if (tools) parts.push(`${tools} tool use${tools === 1 ? "" : "s"}`);
    if (tokens) parts.push(`${compact(tokens)} tokens`);
    if (ms) parts.push(formatDuration(ms));
    return {
      label: "task",
      text: parts.join(" · "),
      tone: /fail|error|kill/i.test(status) ? "warn" : undefined,
    };
  }
  if (kind === "peer") {
    const from = /<agent-message from="([^"]+)"/.exec(raw)?.[1];
    const inner = /<agent-message[^>]*>([\s\S]*?)<\/agent-message>/.exec(
      raw,
    )?.[1];
    const body = stripTags(inner ?? raw);
    return {
      label: "peer",
      text: `Message from another session${from ? ` ${from}` : ""}: ${body.slice(0, 160)}`,
    };
  }
  return { label: kind, text: stripTags(raw).slice(0, 200) || kind };
}

function assistant(d: Draft, data: Record<string, unknown>, ctx: Context) {
  const msg = record(data.message);
  const msgId = text(msg.id);
  const base = text(data.uuid) || msgId || "a:" + ctx.seq;
  const parent = text(data.parent_tool_use_id) || undefined;
  const content =
    typeof msg.content === "string"
      ? [{ type: "text", text: msg.content }]
      : list(msg.content);
  content.forEach((raw, i) => {
    const c = record(raw);
    const type = text(c.type);
    const key = `${base}:${i}`;
    if (
      type === "text" ||
      type === "thinking" ||
      type === "redacted_thinking"
    ) {
      const kind = type === "text" ? "assistant" : "thinking";
      const body = text(type === "text" ? c.text : c.thinking);
      const stream = msgId
        ? d.find(
            (b) => b.key.startsWith(`s:${msgId}:`) && b.kind === kind,
            false,
          )
        : undefined;
      if (stream)
        d.replace(stream.key, { key, kind, text: body || stream.text, parent });
      else if (body) d.upsert({ key, kind, text: body, parent });
      return;
    }
    if (type.endsWith("tool_use")) {
      const id = text(c.id) || key;
      d.upsert({
        key: id,
        kind: "tool",
        parent,
        steps: todoSteps(c.input),
        tool: {
          name: text(c.name) || type,
          input: c.input,
          status: "running",
          nativeType: type,
          raw: c,
        },
      });
      return;
    }
    if (type.endsWith("tool_result")) {
      const id = text(c.tool_use_id);
      if (id && d.get(id))
        d.patchTool(id, {
          status: "ok",
          output: contentText(c.content),
          result: c.content,
        });
      return;
    }
    d.upsert({
      key,
      kind: "tool",
      parent,
      tool: {
        name: type || "block",
        input: c,
        status: "ok",
        nativeType: type,
        raw: c,
      },
    });
  });
}

function user(d: Draft, data: Record<string, unknown>, ctx: Context) {
  const msg = record(data.message);
  const key = text(data.uuid) || "u:" + ctx.seq;
  const parent = text(data.parent_tool_use_id) || undefined;
  if (data.isCompactSummary === true) {
    d.upsert({ key, kind: "notice", text: "Conversation compacted" });
    return;
  }
  const content =
    typeof msg.content === "string"
      ? [{ type: "text", text: msg.content }]
      : list(msg.content);
  const texts: string[] = [];
  const media: Media[] = [];
  for (const raw of content) {
    const c = record(raw);
    if (c.type === "tool_result") {
      const id = text(c.tool_use_id);
      const output = contentText(c.content);
      const images = contentMedia(c.content);
      const declined =
        c.is_error === true &&
        /doesn't want to proceed|user (rejected|denied)|was rejected/i.test(
          output,
        );
      const status: ToolStatus = declined
        ? "declined"
        : c.is_error
          ? "failed"
          : "ok";
      const result = data.tool_use_result ?? data.toolUseResult;
      const change = {
        status,
        output,
        media: images.length ? images : undefined,
        result,
        // An Agent call's result names the subagent it started.
        agentId: text(record(result).agentId) || undefined,
      };
      if (d.get(id)) d.patchTool(id, change);
      else
        d.upsert({
          key: id || key,
          kind: "tool",
          parent,
          tool: { name: "Tool", placeholder: true, ...change },
        });
    } else if (c.type === "text") texts.push(text(c.text));
    else if (c.type === "image") media.push(imageMedia(c));
  }
  if (parent || (!texts.length && !media.length)) return;
  const joined = texts.join("\n");
  const injected = injectedKind(data, joined);
  if (injected) {
    d.upsert({
      key,
      kind: "notice",
      detail: joined,
      ...systemNotice(injected, joined),
    });
    return;
  }
  const shaped = shapeUserText(joined);
  if (shaped.kind === "skip" && !media.length) return;
  if (shaped.kind === "notice") {
    d.upsert({ key, kind: "notice", text: shaped.text });
    return;
  }
  const block: Block = {
    key,
    kind: "user",
    text: shaped.text,
    media: media.length ? media : undefined,
    local: false,
    // Taken in mid-turn (a queued_command record).
    steered: data.steered === true || undefined,
  };
  const echo = ctx.history
    ? undefined
    : d.find((b) => b.local === true && b.kind === "user");
  if (echo) d.replace(echo.key, block);
  else d.upsert(block);
}

export function applyClaude(
  d: Draft,
  data: Record<string, unknown>,
  ctx: Context,
) {
  switch (text(data.type)) {
    case "assistant":
      // A local command's output (/context, /usage) streams as an assistant
      // message; it is shown as the command's output, as in history.
      if (data.local_command_run) {
        const content = list(record(data.message).content)
          .map((c) => text(record(c).text))
          .join("\n");
        return localOutput(d, text(data.uuid) || "local:" + ctx.seq, content);
      }
      return assistant(d, data, ctx);
    case "user":
      return user(d, data, ctx);
    // History: the subagent an Agent call started, and how many of its
    // tool calls were left out (subagents.go).
    case "workbench_subagent": {
      const tool = text(data.tool_use_id);
      d.patchTool(tool, { agentId: text(data.agentId) || undefined });
      const earlier = Number(data.earlier) || 0;
      if (earlier && d.get(tool)) d.patch(tool, { earlier });
      return;
    }
    case "system":
      // Claude registers a subagent as a task of the Agent call.
      if (
        data.subtype === "task_started" &&
        text(data.task_type).includes("agent") &&
        data.tool_use_id
      )
        d.patchTool(text(data.tool_use_id), {
          agentId: text(data.task_id) || undefined,
        });
      if (data.subtype === "local_command")
        return localOutput(
          d,
          text(data.uuid) || "local:" + ctx.seq,
          text(data.content),
        );
      if (data.subtype === "compact_boundary")
        d.upsert({
          key: "compact:" + (text(data.uuid) || ctx.seq),
          kind: "notice",
          text: "Conversation compacted",
        });
      return systemRecord(d, data, ctx);
  }
}

const AUTO_REASONS: Record<string, string> = {
  outside_reads_blocked: "reads outside the project are blocked",
  memory_paused: "memory is paused",
  classifier_transcript_too_long: "the conversation is too long for its check",
  auto_mode_no_check_in_session: "its safety check does not run here",
};

const QUIET_HOOKS = new Set([
  "PreToolUse",
  "PostToolUse",
  "PostToolUseFailure",
  "Stop",
  "SubagentStop",
]);

const FALLBACK_TRIGGERS: Record<string, string> = {
  model_not_found: "the model is not available",
  permission_denied: "no access to the model",
  overloaded: "the model is overloaded",
  server_error: "server errors",
  last_resort: "an error",
  model_blocked: "the model is blocked",
  refusal: "the model declined",
};

// The system records worth a line in the conversation: warnings, a model
// switch, a failed hook, a step auto mode blocked, long instructions.
function systemRecord(d: Draft, data: Record<string, unknown>, ctx: Context) {
  const key = (name: string) => `${name}:${text(data.uuid) || ctx.seq}`;
  const notice = (name: string, line: string, warn = false) =>
    d.upsert({
      key: key(name),
      kind: "notice",
      text: line,
      tone: warn ? "warn" : undefined,
      local: true,
    });
  switch (text(data.subtype)) {
    case "permission_denied": {
      const tool = text(data.tool_use_id);
      const code = text(data.decision_reason_code);
      const why =
        AUTO_REASONS[code] ??
        (text(data.decision_reason_type) === "classifier"
          ? "its safety check said no"
          : text(data.decision_reason_type) || "not allowed");
      const note = `Blocked by auto mode: ${why}`;
      if (d.get(tool)?.tool) {
        d.patchTool(tool, { status: "declined", note });
        return;
      }
      return notice(
        "denied",
        `${text(data.tool_name)} ${note.toLowerCase()}`,
        true,
      );
    }
    case "informational": {
      const level = text(data.level);
      if (level === "info") return;
      return notice(
        "info",
        text(data.content),
        level === "warning" || level === "suggestion",
      );
    }
    case "notification": {
      const priority = text(data.priority);
      if (priority !== "high" && priority !== "immediate") return;
      const line = text(data.text);
      // Often the same words as an informational line just before.
      if (d.find((b) => b.kind === "notice" && b.text === line)) return;
      return d.upsert({
        key: "notification:" + text(data.key),
        kind: "notice",
        text: line,
        tone: "warn",
        local: true,
      });
    }
    case "model_fallback":
    case "model_refusal_fallback":
    case "model_consent_fallback": {
      const why = FALLBACK_TRIGGERS[text(data.trigger)];
      return notice(
        "fallback",
        `Switched from ${text(data.original_model)} to ${text(data.fallback_model)}${why ? ` (${why})` : ""}`,
        true,
      );
    }
    case "hook_response": {
      const outcome = text(data.outcome);
      const code = typeof data.exit_code === "number" ? data.exit_code : 0;
      if (outcome !== "error" && code === 0) return;
      // A tool hook's refusal shows in the tool's result, a Stop hook's
      // feedback (a /goal check) in the conversation.
      if (QUIET_HOOKS.has(text(data.hook_event))) return;
      const said = (text(data.stderr) || text(data.output) || text(data.stdout))
        .trim()
        .split("\n")[0];
      return notice(
        "hook",
        `The ${text(data.hook_event)} hook${text(data.hook_name) && text(data.hook_name) !== text(data.hook_event) ? ` (${text(data.hook_name)})` : ""} failed${code ? ` with exit code ${code}` : ""}${said ? `: ${said}` : ""}`,
        true,
      );
    }
    case "instruction_size_warning": {
      const total = Number(data.total_chars) || 0;
      const limit = Number(data.total_limit_chars) || 0;
      const files = Number(data.file_count) || 0;
      return notice(
        "instructions",
        `Instruction files (CLAUDE.md and rules${files > 1 ? `, ${files} files` : ""}) are ${total.toLocaleString("en")} characters, over the ${limit.toLocaleString("en")} recommended for this model`,
        true,
      );
    }
  }
}

// Output of a command Claude ran locally (no model call), as Markdown.
function localOutput(d: Draft, key: string, content: string) {
  const body = (tag(content, "local-command-stdout") ?? content)
    .replace(/<\/?local-command-std(?:out|err)>/g, "")
    .trim();
  if (!body) return;
  d.upsert({ key, kind: "notice", text: stripAnsi(body), markdown: true });
}
