import {
  memo,
  useCallback,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import { useVirtualizer } from "@tanstack/react-virtual";
import { ArrowDown, ChevronRight, Loader2 } from "lucide-react";
import {
  formatDuration,
  record,
  type Block,
  type Step,
  type Tool,
} from "./model";
import type { State } from "./reducer";
import { CopyButton, Markdown } from "./Markdown";
import { BaseContext, MediaList, Mentions, useEnv } from "./media";
import { toolDiff, toolSummary, toolTitle, unwrapShell } from "./summarize";
import { withContext, type DiffLine } from "./diff";
import { hasAnsi, parseAnsi } from "./ansi";
import { AskCard } from "./AskCard";
import { askStates, parseReply, type AskState } from "./questions";

const ESTIMATE: Record<string, number> = {
  user: 64,
  assistant: 90,
  tool: 46,
  thinking: 30,
  plan: 90,
  notice: 28,
  error: 40,
  turn: 34,
};

export { formatDuration };

function layout(state: State) {
  const rows: Block[] = [];
  const kids = new Map<string, Block[]>();
  for (const k of state.keys) {
    const b = state.blocks.get(k)!;
    if (b.parent && state.blocks.has(b.parent)) {
      const list = kids.get(b.parent) ?? [];
      list.push(b);
      kids.set(b.parent, list);
      continue;
    }
    if (b.kind === "turn" && (b.durationMs ?? 0) < 1000) continue;
    rows.push(b);
  }
  return { rows, kids };
}

type RowProps = {
  block: Block;
  kids?: Block[];
  open: boolean;
  onToggle: (key: string) => void;
  // Codex questions: answered, skipped or still open (questions.ts).
  ask?: AskState;
};

function Fold({
  open,
  label,
  onClick,
}: {
  open: boolean;
  label: React.ReactNode;
  onClick: () => void;
}) {
  return (
    <button
      type="button"
      className="fold"
      aria-expanded={open}
      onClick={onClick}
    >
      {label}
      <ChevronRight
        size={12}
        className={open ? "fold-chevron open" : "fold-chevron"}
      />
    </button>
  );
}

// Tool output with its ANSI colors; plain text skips parsing.
function AnsiText({ text }: { text: string }) {
  const segments = useMemo(() => parseAnsi(text), [text]);
  if (!hasAnsi(text)) return <>{text}</>;
  return (
    <>
      {segments.map((s, i) => (
        <span
          key={i}
          style={{
            color: s.style.fg,
            background: s.style.bg,
            fontWeight: s.style.bold ? 650 : undefined,
            fontStyle: s.style.italic ? "italic" : undefined,
            textDecoration: s.style.underline ? "underline" : undefined,
            opacity: s.style.dim ? 0.6 : undefined,
          }}
        >
          {s.text}
        </span>
      ))}
    </>
  );
}

function Lines({ text, limit = 200 }: { text: string; limit?: number }) {
  const [all, setAll] = useState(false);
  const lines = text.replace(/\n$/, "").split("\n");
  const shown = all ? lines : lines.slice(0, limit);
  return (
    <>
      <pre className="tool-output">
        <AnsiText text={shown.join("\n")} />
      </pre>
      {lines.length > limit && (
        <button type="button" className="more" onClick={() => setAll(!all)}>
          {all ? "Show less" : `Show all ${lines.length} lines`}
        </button>
      )}
    </>
  );
}

export function DiffView({
  lines,
  limit = 400,
}: {
  lines: DiffLine[];
  limit?: number;
}) {
  const [all, setAll] = useState(false);
  const trimmed = useMemo(() => withContext(lines), [lines]);
  const shown = all ? trimmed : trimmed.slice(0, limit);
  return (
    <>
      <div className="diff" role="table" aria-label="Changes">
        {shown.map((l, i) => (
          <div
            key={i}
            className={
              "diff-line op-" +
              (l.op === " "
                ? "ctx"
                : l.op === "+"
                  ? "add"
                  : l.op === "-"
                    ? "del"
                    : "hunk")
            }
          >
            <span className="diff-op">{l.op === "@" ? "" : l.op}</span>
            <span className="diff-text">{l.text}</span>
          </div>
        ))}
      </div>
      {trimmed.length > limit && (
        <button type="button" className="more" onClick={() => setAll(!all)}>
          {all ? "Show less" : `Show all ${trimmed.length} lines`}
        </button>
      )}
    </>
  );
}

function InputView({ input }: { input: unknown }) {
  if (input === undefined || input === null) return null;
  const entries: [string, unknown][] =
    typeof input === "object" && !Array.isArray(input)
      ? Object.entries(record(input))
      : [["input", input]];
  const visible = entries.filter(
    ([, v]) => v !== undefined && v !== null && v !== "",
  );
  if (!visible.length) return null;
  return (
    <dl className="tool-input">
      {visible.map(([k, v]) => {
        const value = typeof v === "string" ? v : JSON.stringify(v, null, 2);
        const block = value.includes("\n") || value.length > 120;
        return (
          <div key={k} className={block ? "kv block" : "kv"}>
            <dt>{k}</dt>
            <dd>
              {block ? (
                <pre>
                  {value.length > 20000 ? value.slice(0, 20000) + "\n…" : value}
                </pre>
              ) : (
                value
              )}
            </dd>
          </div>
        );
      })}
    </dl>
  );
}

function Steps({ steps }: { steps: Step[] }) {
  return (
    <ul className="steps">
      {steps.map((s, i) => (
        <li key={i} className={"step " + s.status}>
          <span className="step-box" aria-label={s.status}>
            {s.status === "completed"
              ? "☒"
              : s.status === "in_progress"
                ? "◐"
                : "☐"}
          </span>
          <span>{s.text}</span>
        </li>
      ))}
    </ul>
  );
}

function ToolDetails({ tool, diff }: { tool: Tool; diff?: DiffLine[] }) {
  const input = record(tool.input);
  const command =
    typeof input.command === "string" ? unwrapShell(input.command) : undefined;
  return (
    <div className="tool-details">
      {command !== undefined && <pre className="tool-cmd">$ {command}</pre>}
      {diff ? (
        <DiffView lines={diff} />
      ) : (
        command === undefined && <InputView input={tool.input} />
      )}
      {tool.output && <Lines text={tool.output} />}
      <Mentions text={tool.output} />
      <details className="raw">
        <summary>Native record</summary>
        <pre>{JSON.stringify(tool.raw ?? tool, null, 2).slice(0, 50000)}</pre>
      </details>
    </div>
  );
}

function ChildLine({ block }: { block: Block }) {
  const env = useEnv();
  if (!block.tool) return null;
  const { name, arg } = toolTitle(block.tool, env.cwd);
  return (
    <div className={"child-line status-" + block.tool.status}>
      <span className="tool-dot">●</span>
      <span className="tool-name">{name}</span>
      {arg && <span className="tool-arg">({arg})</span>}
    </div>
  );
}

function ToolRow({ block, kids, open, onToggle }: RowProps) {
  const env = useEnv();
  const t = block.tool!;
  const { name, arg } = toolTitle(t, env.cwd);
  const diff = useMemo(() => toolDiff(t), [t]);
  const summary = toolSummary(t, diff);
  const tools = (kids ?? []).filter((k) => k.kind === "tool");
  const shownKids = open ? tools : tools.slice(-3);
  return (
    <div className={"row tool status-" + t.status}>
      <span className="mark tool-dot" aria-label={t.status}>
        {t.status === "running" ? <Loader2 size={11} className="spin" /> : "●"}
      </span>
      <div className="body">
        <button
          type="button"
          className="tool-head"
          aria-expanded={open}
          onClick={() => onToggle(block.key)}
        >
          <span className="tool-name">{name}</span>
          {arg && <span className="tool-arg">({arg})</span>}
          {t.durationMs !== undefined && t.durationMs >= 1000 && (
            <span className="tool-time">{formatDuration(t.durationMs)}</span>
          )}
        </button>
        {block.steps ? (
          <Steps steps={block.steps} />
        ) : (
          <div className={"tool-summary " + (summary.tone ?? "")}>
            <span className="elbow">⎿</span>
            <div>
              {summary.lines.map((l, i) => (
                <div key={i} className="summary-line">
                  {l}
                </div>
              ))}
              {!open && summary.more ? (
                <button
                  type="button"
                  className="more"
                  onClick={() => onToggle(block.key)}
                >
                  … +{summary.more} lines
                </button>
              ) : null}
            </div>
          </div>
        )}
        {tools.length > 0 && (
          <div className="tool-children">
            {!open && tools.length > 3 && (
              <div className="child-more">
                +{tools.length - 3} more tool uses
              </div>
            )}
            {shownKids.map((k) => (
              <ChildLine key={k.key} block={k} />
            ))}
          </div>
        )}
        <MediaList media={t.media} />
        {open && <ToolDetails tool={t} diff={diff} />}
      </div>
    </div>
  );
}

function UserRow({ block, open, onToggle }: RowProps) {
  const lines = (block.text ?? "").split("\n");
  const long = lines.length > 12;
  // Answers to Codex's questions read as question and answer.
  const replies = useMemo(() => parseReply(block.text), [block.text]);
  return (
    <div className={"row user" + (block.sending ? " sending" : "")}>
      <span className="mark">›</span>
      <div className="body">
        {replies ? (
          <div className="user-replies">
            {replies.map((r, i) => (
              <div key={i} className="ask-answered">
                <span className="ask-question">{r.question}</span>
                <span className="ask-answer">{r.answer}</span>
              </div>
            ))}
          </div>
        ) : (
          <div className="user-text">
            {long && !open ? lines.slice(0, 12).join("\n") : block.text}
          </div>
        )}
        {block.sending && <div className="user-sending">Sending…</div>}
        {block.steered && (
          <div className="user-steered-line">
            <span className="user-steered" title="Sent into the running turn">
              Steered
            </span>
            {block.waiting && (
              <span className="user-waiting">
                Claude reads it at its next step
                {block.canSendNow && (
                  <button
                    type="button"
                    title="Take it in now, as Ctrl+X Ctrl+S in Claude Code: a running tool moves to the background, or the reply in progress stops"
                    onClick={() =>
                      window.dispatchEvent(
                        new CustomEvent("workbench:send-now", {
                          detail: block.steerId,
                        }),
                      )
                    }
                  >
                    Send now
                  </button>
                )}
              </span>
            )}
          </div>
        )}
        {long && !replies && (
          <button
            type="button"
            className="more"
            onClick={() => onToggle(block.key)}
          >
            {open ? "Show less" : `Show ${lines.length - 12} more lines`}
          </button>
        )}
        <MediaList media={block.media} />
      </div>
    </div>
  );
}

function AssistantRow({ block, ask }: RowProps) {
  if (block.questions?.length && ask)
    return (
      <div className="row assistant">
        <span className="mark">●</span>
        <div className="body">
          <AskCard id={block.key} questions={block.questions} state={ask} />
        </div>
      </div>
    );
  return (
    <div className={"row assistant" + (block.streaming ? " streaming" : "")}>
      <span className="mark">●</span>
      <div className="body">
        <Markdown text={block.text ?? ""} />
        {block.streaming && <span className="caret" aria-hidden />}
        {!block.streaming && <Mentions text={block.text} />}
        {!block.streaming && block.text && (
          <div className="row-actions">
            <CopyButton text={block.text} label="Copy message" />
          </div>
        )}
      </div>
    </div>
  );
}

function ThinkingRow({ block, open, onToggle }: RowProps) {
  return (
    <div className={"row thinking" + (block.streaming ? " streaming" : "")}>
      <span className="mark">✻</span>
      <div className="body">
        <Fold
          open={open}
          label={block.streaming ? "Thinking…" : "Thought"}
          onClick={() => onToggle(block.key)}
        />
        {open && <div className="thinking-text">{block.text}</div>}
      </div>
    </div>
  );
}

function PlanRow({ block }: RowProps) {
  return (
    <div className="row plan">
      <span className="mark">●</span>
      <div className="body">
        <div className="tool-head static">
          <span className="tool-name">
            {block.steps ? "Update Plan" : "Plan"}
          </span>
        </div>
        {block.steps && block.text && (
          <div className="plan-note">{block.text}</div>
        )}
        {block.steps ? (
          <Steps steps={block.steps} />
        ) : (
          <Markdown text={block.text ?? ""} />
        )}
      </div>
    </div>
  );
}

function NoticeRow({ block, open, onToggle }: RowProps) {
  const lines = (block.text ?? "").split("\n");
  const long = lines.length > 6;
  if (block.detail !== undefined)
    return (
      <div
        className={"row notice event" + (block.tone === "warn" ? " warn" : "")}
      >
        <span className="mark">◇</span>
        <div className="body">
          <button
            type="button"
            className="event-head"
            aria-expanded={open}
            onClick={() => onToggle(block.key)}
          >
            {block.label && <span className="event-label">{block.label}</span>}
            <span className="event-text">{block.text}</span>
            <ChevronRight
              size={12}
              className={open ? "fold-chevron open" : "fold-chevron"}
            />
          </button>
          {open && (
            <pre className="tool-output">
              <AnsiText text={block.detail ?? ""} />
            </pre>
          )}
        </div>
      </div>
    );
  if (block.markdown)
    // Output of a local command (/context): tables and headings.
    return (
      <div className="row notice local-output">
        <span className="mark">⎿</span>
        <div className="body">
          <Markdown text={block.text ?? ""} />
        </div>
      </div>
    );
  return (
    <div className={"row notice" + (block.tone === "warn" ? " warn" : "")}>
      <span className="mark">⎿</span>
      <div className="body">
        <div className="notice-text">
          {long && !open ? lines.slice(0, 6).join("\n") : block.text}
        </div>
        {long && (
          <button
            type="button"
            className="more"
            onClick={() => onToggle(block.key)}
          >
            {open ? "Show less" : `… +${lines.length - 6} lines`}
          </button>
        )}
      </div>
    </div>
  );
}

// Paths in a block are relative to the directory its record was written in.
const Row = memo(function Row(props: RowProps) {
  return (
    <BaseContext.Provider value={props.block.cwd}>
      <RowBody {...props} />
    </BaseContext.Provider>
  );
});

function RowBody(props: RowProps) {
  switch (props.block.kind) {
    case "user":
      return <UserRow {...props} />;
    case "assistant":
      return <AssistantRow {...props} />;
    case "thinking":
      return <ThinkingRow {...props} />;
    case "tool":
      return <ToolRow {...props} />;
    case "plan":
      return <PlanRow {...props} />;
    case "error":
      return (
        <div className="row error" role="alert">
          <span className="mark">✗</span>
          <div className="body error-text-block">{props.block.text}</div>
        </div>
      );
    case "turn":
      return (
        <div className="turn-divider">
          <span>Worked for {formatDuration(props.block.durationMs ?? 0)}</span>
        </div>
      );
    default:
      return <NoticeRow {...props} />;
  }
}

export function Timeline({
  state,
  verbose,
  hasMore,
  loadingEarlier,
  onLoadEarlier,
}: {
  state: State;
  verbose: boolean;
  hasMore: boolean;
  loadingEarlier: boolean;
  onLoadEarlier: () => void;
}) {
  const ref = useRef<HTMLDivElement>(null);
  const { rows, kids } = useMemo(() => layout(state), [state]);
  const asks = useMemo(
    () => askStates(state.keys.map((k) => state.blocks.get(k)!)),
    [state],
  );
  const [open, setOpen] = useState<Set<string>>(() => new Set());
  const toggle = useCallback(
    (key: string) =>
      setOpen((s) => {
        const next = new Set(s);
        if (next.has(key)) next.delete(key);
        else next.add(key);
        return next;
      }),
    [],
  );
  const virtual = useVirtualizer({
    count: rows.length,
    getScrollElement: () => ref.current,
    estimateSize: (i) => ESTIMATE[rows[i]?.kind] ?? 60,
    overscan: 8,
    getItemKey: (i) => rows[i].key,
  });
  const stick = useRef(true);
  const [behind, setBehind] = useState(false);
  const [nearTop, setNearTop] = useState(true);
  const earlier = useRef({ hasMore, loadingEarlier, onLoadEarlier });
  earlier.current = { hasMore, loadingEarlier, onLoadEarlier };
  const total = virtual.getTotalSize();
  const last = rows[rows.length - 1];
  const first = rows[0]?.key;

  // Keep the reading position when an older page is prepended.
  const previousFirst = useRef(first);
  useLayoutEffect(() => {
    const before = previousFirst.current;
    previousFirst.current = first;
    const el = ref.current;
    if (!el || !before || before === first) return;
    const index = rows.findIndex((r) => r.key === before);
    if (index > 0)
      el.scrollTop += virtual.getOffsetForIndex(index, "start")?.[0] ?? 0;
  }, [first]);

  // Follow new output only while the reader is at the bottom.
  useLayoutEffect(() => {
    const el = ref.current;
    if (el && stick.current) el.scrollTop = el.scrollHeight;
  }, [total, rows.length, last]);
  useLayoutEffect(() => {
    if (!stick.current) setBehind(true);
  }, [last]);

  const onScroll = () => {
    const el = ref.current;
    if (!el) return;
    stick.current = el.scrollHeight - el.scrollTop - el.clientHeight < 60;
    setNearTop(el.scrollTop < 400);
    if (stick.current) setBehind(false);
    const e = earlier.current;
    if (el.scrollTop < 150 && e.hasMore && !e.loadingEarlier) e.onLoadEarlier();
  };

  return (
    <div className="timeline-wrap">
      <div ref={ref} className="timeline" onScroll={onScroll}>
        {((hasMore && nearTop) || loadingEarlier) && (
          <div className="load-earlier">
            <button
              type="button"
              disabled={loadingEarlier}
              onClick={onLoadEarlier}
            >
              {loadingEarlier
                ? "Loading earlier messages…"
                : "Load earlier messages"}
            </button>
          </div>
        )}
        <div className="timeline-inner" style={{ height: total }}>
          {virtual.getVirtualItems().map((v) => {
            const b = rows[v.index];
            return (
              <div
                key={v.key}
                data-index={v.index}
                ref={virtual.measureElement}
                className={"vrow after-" + (rows[v.index - 1]?.kind ?? "none")}
                style={{ transform: `translateY(${v.start}px)` }}
              >
                <Row
                  block={b}
                  kids={kids.get(b.key)}
                  open={verbose || open.has(b.key)}
                  onToggle={toggle}
                  ask={b.questions ? asks.get(b.key) : undefined}
                />
              </div>
            );
          })}
        </div>
      </div>
      {behind && (
        <button
          type="button"
          className="new-output"
          onClick={() => {
            stick.current = true;
            setBehind(false);
            const el = ref.current;
            if (el) el.scrollTop = el.scrollHeight;
          }}
        >
          <ArrowDown size={13} /> New output
        </button>
      )}
    </div>
  );
}
