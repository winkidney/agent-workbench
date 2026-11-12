import { useEffect, useMemo, useRef, useState } from "react";
import {
  ShieldCheck,
  MessageCircleQuestion,
  ClipboardList,
} from "lucide-react";
import { Button } from "../components/ui/button";
import { Textarea } from "../components/ui/textarea";
import {
  list,
  record,
  text,
  type Agent,
  type Block,
  type Tool,
  type WorkbenchEvent,
} from "../transcript/model";
import { toolDiff, toolTitle, unwrapShell } from "../transcript/summarize";
import { DiffView } from "../transcript/Timeline";
import { Markdown } from "../transcript/Markdown";
import { useEnv } from "../transcript/media";
import { Choices } from "./Choices";
import { ElicitationForm } from "./ElicitationForm";
import { parseElicitation, type Elicitation } from "./elicitation";

export type Decision = "allow" | "allowAlways" | "deny" | "cancel";
// The permission mode an approved Claude plan continues in.
export type PlanMode = "auto" | "acceptEdits" | "default";
export type Reply = (
  id: string,
  decision: Decision,
  message?: string,
  answers?: Record<string, unknown>,
  mode?: PlanMode,
) => Promise<void>;

type Question = {
  id: string;
  header?: string;
  question: string;
  options: { label: string; description?: string }[];
  multi: boolean;
  other: boolean;
  secret?: boolean;
};

type Parsed =
  | { kind: "tool"; tool: Tool; reason?: string; always: boolean }
  | { kind: "questions"; questions: Question[]; format: "claude" | "codex" }
  | { kind: "plan"; plan: string }
  // Codex asks for access beyond its sandbox (network, paths).
  | { kind: "permissions"; items: string[]; reason?: string }
  // An MCP server asks for input.
  | { kind: "elicitation"; elicitation: Elicitation }
  | { kind: "unknown" };

// What a Codex permission request asks for, one line each.
export function permissionItems(permissions: unknown): string[] {
  const p = record(permissions);
  const out: string[] = [];
  if (record(p.network).enabled === true) out.push("Network access");
  const fs = record(p.fileSystem);
  const where = (path: unknown) => {
    const x = record(path);
    if (x.type === "path") return text(x.path);
    if (x.type === "glob_pattern") return text(x.pattern);
    if (x.type === "special") {
      const v = record(x.value);
      const kind = text(v.kind);
      const names: Record<string, string> = {
        root: "the whole file system",
        minimal: "system files",
        project_roots: "the project",
        tmpdir: "the temporary directory",
      };
      return (names[kind] ?? kind) + (v.subpath ? ` (${text(v.subpath)})` : "");
    }
    return text(path);
  };
  for (const e of list(fs.entries).map(record)) {
    const access = text(e.access);
    out.push(
      `${access === "write" ? "Write" : access === "read" ? "Read" : "No access to"} ${where(e.path)}`,
    );
  }
  for (const r of list(fs.read)) out.push(`Read ${text(r)}`);
  for (const w of list(fs.write)) out.push(`Write ${text(w)}`);
  return out;
}

// Native request → card model. Unknown request types fall back to raw JSON.
export function parseRequest(
  agent: Agent,
  request: unknown,
  lookup: (id: string) => Block | undefined,
): Parsed {
  const v = record(request);
  const elicitation = parseElicitation(agent, request);
  if (elicitation) return { kind: "elicitation", elicitation };
  if (agent === "claude") {
    const req = record(v.request);
    if (req.subtype !== "can_use_tool") return { kind: "unknown" };
    const name = text(req.tool_name);
    const input = record(req.input);
    if (name === "AskUserQuestion" && Array.isArray(input.questions))
      return {
        kind: "questions",
        format: "claude",
        questions: input.questions.map(record).map((q) => ({
          id: text(q.question),
          header: text(q.header) || undefined,
          question: text(q.question),
          options: list(q.options)
            .map(record)
            .map((o) => ({
              label: text(o.label),
              description: text(o.description) || undefined,
            })),
          multi: q.multiSelect === true,
          other: true,
        })),
      };
    if (name === "ExitPlanMode" && typeof input.plan === "string")
      return { kind: "plan", plan: input.plan };
    return {
      kind: "tool",
      tool: { name, input: req.input, status: "running", raw: req },
      reason: text(req.decision_reason || req.blocked_path) || undefined,
      always: list(req.permission_suggestions).length > 0,
    };
  }
  const method = text(v.method);
  const p = record(v.params);
  if (method === "item/permissions/requestApproval")
    return {
      kind: "permissions",
      items: permissionItems(p.permissions),
      reason: text(p.reason) || undefined,
    };
  if (method === "item/commandExecution/requestApproval")
    return {
      kind: "tool",
      tool: {
        name: "Shell",
        nativeType: "commandExecution",
        input: { command: p.command, cwd: p.cwd, actions: p.commandActions },
        status: "running",
      },
      reason: text(p.reason) || undefined,
      always: true,
    };
  if (method === "item/fileChange/requestApproval") {
    const item = lookup(text(p.itemId))?.tool;
    return {
      kind: "tool",
      tool: item ?? {
        name: "Edit",
        input: { grantRoot: p.grantRoot },
        status: "running",
      },
      reason: text(p.reason) || undefined,
      always: true,
    };
  }
  if (method.includes("requestUserInput") && Array.isArray(p.questions))
    return {
      kind: "questions",
      format: "codex",
      questions: p.questions.map(record).map((q) => ({
        id: text(q.id),
        header: text(q.header) || undefined,
        question: text(q.question),
        options: list(q.options)
          .map(record)
          .map((o) => ({
            label: text(o.label),
            description: text(o.description) || undefined,
          })),
        multi: false,
        other: q.isOther === true || !list(q.options).length,
        secret: q.isSecret === true,
      })),
    };
  return { kind: "unknown" };
}

function ToolPreview({ tool }: { tool: Tool }) {
  const env = useEnv();
  const { name, arg } = toolTitle(tool, env.cwd);
  const diff = useMemo(() => toolDiff(tool), [tool]);
  const input = record(tool.input);
  const command =
    typeof input.command === "string" ? unwrapShell(input.command) : undefined;
  return (
    <div className="approval-tool">
      <div className="tool-head static">
        <span className="tool-name">{name}</span>
        {arg && !command && <span className="tool-arg">({arg})</span>}
      </div>
      {command !== undefined && <pre className="tool-cmd">$ {command}</pre>}
      {diff && <DiffView lines={diff} limit={120} />}
    </div>
  );
}

function QuestionForm({
  questions,
  busy,
  onSubmit,
  onDecline,
  declineLabel,
}: {
  questions: Question[];
  busy: boolean;
  onSubmit: (answers: Record<string, string[]>) => void;
  onDecline: () => void;
  declineLabel: string;
}) {
  const [picked, setPicked] = useState<Record<string, string[]>>({});
  const [other, setOther] = useState<Record<string, string>>({});
  const answer = (q: Question) => [
    ...(picked[q.id] ?? []),
    ...(other[q.id]?.trim() ? [other[q.id].trim()] : []),
  ];
  const complete = questions.every((q) => answer(q).length > 0);
  const toggle = (q: Question, label: string) =>
    setPicked((p) => {
      const cur = p[q.id] ?? [];
      if (!q.multi) return { ...p, [q.id]: cur[0] === label ? [] : [label] };
      return {
        ...p,
        [q.id]: cur.includes(label)
          ? cur.filter((x) => x !== label)
          : [...cur, label],
      };
    });
  return (
    <form
      onSubmit={(e) => {
        e.preventDefault();
        if (complete)
          onSubmit(Object.fromEntries(questions.map((q) => [q.id, answer(q)])));
      }}
    >
      {questions.map((q) => (
        <fieldset key={q.id} className="question">
          {q.header && <span className="question-header">{q.header}</span>}
          <legend>{q.question}</legend>
          {q.options.map((o, i) => {
            const on = (picked[q.id] ?? []).includes(o.label);
            return (
              <button
                key={o.label}
                type="button"
                className={"approval-option" + (on ? " selected" : "")}
                aria-pressed={on}
                onClick={() => toggle(q, o.label)}
              >
                <span className="pointer">
                  {q.multi ? (on ? "☒" : "☐") : on ? "◉" : "○"}
                </span>
                <span className="num">{i + 1}.</span>
                <span>{o.label}</span>
                {o.description && <span className="hint">{o.description}</span>}
              </button>
            );
          })}
          {q.other && (
            <input
              aria-label={`Other answer for ${q.question}`}
              type={q.secret ? "password" : "text"}
              placeholder={q.options.length ? "Other…" : "Your answer"}
              value={other[q.id] ?? ""}
              onChange={(e) =>
                setOther((o) => ({ ...o, [q.id]: e.target.value }))
              }
            />
          )}
        </fieldset>
      ))}
      <div className="approval-actions">
        <Button type="submit" size="sm" disabled={busy || !complete}>
          Submit answers
        </Button>
        <Button
          type="button"
          size="sm"
          variant="outline"
          disabled={busy}
          onClick={onDecline}
        >
          {declineLabel}
        </Button>
      </div>
    </form>
  );
}

export function ApprovalCard({
  event,
  agent,
  lookup,
  reply,
  onInterrupt,
}: {
  event: WorkbenchEvent;
  agent: Agent;
  lookup: (id: string) => Block | undefined;
  reply: Reply;
  onInterrupt: () => void;
}) {
  const id = text(event.data.id);
  const parsed = useMemo(
    () => parseRequest(agent, event.data.request, lookup),
    [agent, event, lookup],
  );
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [selected, setSelected] = useState(0);
  const [feedback, setFeedback] = useState("");
  const ref = useRef<HTMLDivElement>(null);
  const feedbackRef = useRef<HTMLTextAreaElement>(null);

  useEffect(() => {
    const active = document.activeElement as HTMLElement | null;
    const typing =
      active &&
      (active.tagName === "TEXTAREA" || active.tagName === "INPUT") &&
      (active as HTMLTextAreaElement).value;
    if (!typing) ref.current?.focus({ preventScroll: true });
  }, []);

  const send = async (
    decision: Decision,
    message?: string,
    answers?: Record<string, unknown>,
    mode?: PlanMode,
  ) => {
    setBusy(true);
    setError("");
    try {
      await reply(id, decision, message, answers, mode);
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  };

  // Claude Code's own plan choices: each "Yes" also sets how edits are
  // approved from then on. A session that was in auto mode before planning
  // is offered auto mode again first, as Claude Code does.
  const resumeAuto = event.data.planResume === "auto";
  const planModes: PlanMode[] = resumeAuto
    ? ["auto", "acceptEdits", "default"]
    : ["acceptEdits", "default"];
  const toolOptions =
    parsed.kind === "plan"
      ? [
          ...(resumeAuto ? [{ label: "Yes, and use auto mode" }] : []),
          { label: "Yes, auto-accept edits" },
          { label: "Yes, manually approve edits" },
          { label: "No, keep planning", hint: "tell Claude what to change" },
        ]
      : parsed.kind === "permissions"
        ? [
            { label: "Yes, for this turn" },
            { label: "Yes, for this session" },
            { label: "No", hint: "Codex continues without it" },
          ]
        : [
            { label: "Yes" },
            ...(parsed.kind === "tool" && parsed.always
              ? [{ label: "Yes, and don't ask again this session" }]
              : []),
            { label: "No", hint: "tell the agent what to do instead" },
          ];
  const denyIndex = toolOptions.length - 1;
  const choose = (i: number) => {
    const was = selected;
    setSelected(i);
    if (i === denyIndex) {
      // First choice reveals the optional feedback box; the second sends.
      // A permission request takes no feedback.
      if (was !== denyIndex && parsed.kind !== "permissions") return;
      return void send("deny", feedback.trim() || undefined);
    }
    if (parsed.kind === "plan")
      return void send("allow", undefined, undefined, planModes[i]);
    void send(i === 0 ? "allow" : "allowAlways");
  };

  const onKey = (e: React.KeyboardEvent) => {
    if (
      parsed.kind === "questions" ||
      parsed.kind === "elicitation" ||
      parsed.kind === "unknown" ||
      busy
    )
      return;
    const inFeedback = e.target === feedbackRef.current;
    if (e.key === "Escape") {
      e.preventDefault();
      void send("deny", feedback.trim() || undefined);
    } else if (inFeedback) {
      if (e.key === "Enter" && !e.shiftKey && !e.nativeEvent.isComposing) {
        e.preventDefault();
        void send("deny", feedback.trim() || undefined);
      }
    } else if (e.key === "ArrowDown" || e.key === "ArrowUp") {
      e.preventDefault();
      setSelected(
        (s) =>
          (s + (e.key === "ArrowDown" ? 1 : toolOptions.length - 1)) %
          toolOptions.length,
      );
    } else if (/^[1-9]$/.test(e.key) && Number(e.key) <= toolOptions.length) {
      e.preventDefault();
      choose(Number(e.key) - 1);
    } else if (e.key === "Enter") {
      e.preventDefault();
      choose(selected);
    }
  };

  useEffect(() => {
    if (
      selected === denyIndex &&
      parsed.kind !== "questions" &&
      parsed.kind !== "permissions"
    )
      feedbackRef.current?.focus();
  }, [selected, denyIndex, parsed.kind]);

  const title =
    parsed.kind === "questions"
      ? "The agent has questions"
      : parsed.kind === "plan"
        ? "Ready to code?"
        : parsed.kind === "tool"
          ? `Allow ${toolTitle(parsed.tool).name}?`
          : parsed.kind === "permissions"
            ? "Allow more access?"
            : parsed.kind === "elicitation"
              ? `${parsed.elicitation.server || "An MCP server"} needs input`
              : "The agent needs a response";
  const Icon =
    parsed.kind === "questions" || parsed.kind === "elicitation"
      ? MessageCircleQuestion
      : parsed.kind === "plan"
        ? ClipboardList
        : ShieldCheck;

  return (
    <div
      ref={ref}
      className="approval"
      tabIndex={-1}
      onKeyDown={onKey}
      role="group"
      aria-label={title}
    >
      <div className="approval-title">
        <Icon size={15} />
        <strong>{title}</strong>
      </div>
      {parsed.kind === "tool" && <ToolPreview tool={parsed.tool} />}
      {parsed.kind === "tool" && parsed.reason && (
        <p className="approval-reason">{parsed.reason}</p>
      )}
      {parsed.kind === "permissions" && (
        <>
          <ul className="approval-permissions">
            {(parsed.items.length ? parsed.items : ["More access"]).map(
              (item) => (
                <li key={item}>{item}</li>
              ),
            )}
          </ul>
          {parsed.reason && <p className="approval-reason">{parsed.reason}</p>}
        </>
      )}
      {parsed.kind === "plan" && (
        <div className="approval-plan">
          <Markdown text={parsed.plan} />
        </div>
      )}
      {parsed.kind === "elicitation" ? (
        <ElicitationForm
          elicitation={parsed.elicitation}
          busy={busy}
          onAccept={(content) =>
            void send("allow", undefined, content ?? undefined)
          }
          onDecline={() => void send("deny")}
          onCancel={() => void send("cancel")}
        />
      ) : parsed.kind === "questions" ? (
        <QuestionForm
          questions={parsed.questions}
          busy={busy}
          declineLabel={
            parsed.format === "codex" ? "Interrupt turn" : "Decline"
          }
          onDecline={() =>
            parsed.format === "codex"
              ? onInterrupt()
              : void send("deny", "User declined to answer")
          }
          onSubmit={(answers) =>
            void send(
              "allow",
              undefined,
              parsed.format === "codex"
                ? Object.fromEntries(
                    Object.entries(answers).map(([k, v]) => [
                      k,
                      { answers: v },
                    ]),
                  )
                : Object.fromEntries(
                    Object.entries(answers).map(([k, v]) => [k, v.join(", ")]),
                  ),
            )
          }
        />
      ) : parsed.kind === "unknown" ? (
        <>
          <pre className="approval-raw">
            {JSON.stringify(event.data.request, null, 2)}
          </pre>
          <div className="approval-actions">
            <Button
              size="sm"
              variant="outline"
              disabled={busy}
              onClick={() => void send("deny")}
            >
              Deny
            </Button>
            <Button
              size="sm"
              variant="ghost"
              disabled={busy}
              onClick={onInterrupt}
            >
              Interrupt turn
            </Button>
          </div>
        </>
      ) : (
        <>
          <Choices
            options={toolOptions}
            selected={selected}
            onChoose={choose}
          />
          {selected === denyIndex && parsed.kind !== "permissions" && (
            <Textarea
              ref={feedbackRef}
              className="approval-feedback"
              aria-label="Feedback for the agent"
              placeholder={
                parsed.kind === "plan"
                  ? "Optional: what should change in the plan? Enter to send"
                  : "Optional: what should the agent do instead? Enter to send"
              }
              value={feedback}
              onChange={(e) => setFeedback(e.target.value)}
            />
          )}
          <p className="approval-keys">
            ↑↓ select · 1–{toolOptions.length} choose · Enter confirm · Esc deny
          </p>
        </>
      )}
      {parsed.kind !== "unknown" && (
        <details className="raw">
          <summary>Native request</summary>
          <pre>{JSON.stringify(event.data.request, null, 2)}</pre>
        </details>
      )}
      {error && (
        <p role="alert" className="error-text">
          {error}
        </p>
      )}
    </div>
  );
}
