import { useEffect, useRef, useState } from "react";
import { ArrowUpRight, Check, Copy, FolderOpen } from "lucide-react";
import { api } from "../../lib/api";
import {
  AGENT_IDS,
  LOGIN_METHODS,
  automaticHome,
  loginActive,
  loginText,
  sameSetup,
  setupOf,
  signInText,
  type AgentId,
  type AgentSetup,
  type AgentView,
  type AgentsSetup,
  type AgentsView,
  type LoginState,
} from "../../lib/agents";
import { AGENT_NAMES } from "../../lib/about";
import { copyField } from "../../lib/copy";
import { DirBrowser } from "../../newsession/DirBrowser";
import { Button } from "../ui/button";
import { Input } from "../ui/input";
import { CopyValue } from "./AboutTab";

type Custom = Record<AgentId, { path: boolean; home: boolean }>;

const customOf = (s: AgentsSetup): Custom => ({
  claude: { path: !!s.claude.path, home: !!s.claude.home },
  codex: { path: !!s.codex.path, home: !!s.codex.home },
});

// What a draft saves: a field left on Automatic saves nothing.
function effective(draft: AgentsSetup, custom: Custom): AgentsSetup {
  const one = (id: AgentId): AgentSetup => ({
    path: custom[id].path ? (draft[id].path ?? "").trim() : "",
    home: custom[id].home ? (draft[id].home ?? "").trim() : "",
  });
  return { claude: one("claude"), codex: one("codex") };
}

// Agents: which program runs each agent and where it keeps its data, and
// signing it in from here (useful away from this computer's terminal).
export function AgentsTab({
  open,
  onDirty,
  onBusy,
}: {
  open: boolean;
  onDirty: (dirty: boolean) => void;
  onBusy: (busy: boolean) => void;
}) {
  const [view, setView] = useState<AgentsView | null>(null);
  const [draft, setDraft] = useState<AgentsSetup | null>(null);
  const [custom, setCustom] = useState<Custom | null>(null);
  const [checks, setChecks] = useState<Partial<Record<AgentId, AgentView>>>({});
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [status, setStatus] = useState("");

  const load = async () => {
    const v = await api<AgentsView>("agents/setup");
    setView(v);
    const s = setupOf(v);
    setDraft(s);
    setCustom(customOf(s));
    setChecks({});
    return v;
  };
  useEffect(() => {
    if (!open) return;
    setError("");
    setStatus("");
    load().catch((e) => setError(e instanceof Error ? e.message : String(e)));
  }, [open]);

  const next = draft && custom ? effective(draft, custom) : null;
  const dirty = !!(view && next && !sameSetup(next, setupOf(view)));
  useEffect(() => onDirty(dirty), [dirty]);
  useEffect(() => () => onDirty(false), []);
  useEffect(() => onBusy(busy), [busy]);

  // Check what is typed, without saving, once typing pauses.
  const nextKey = next ? JSON.stringify(next) : "";
  useEffect(() => {
    if (!next || !view) return;
    const timers = AGENT_IDS.map((id) => {
      const saved = setupOf(view)[id];
      if (
        (next[id].path ?? "") === (saved.path ?? "") &&
        (next[id].home ?? "") === (saved.home ?? "")
      ) {
        setChecks((c) => ({ ...c, [id]: undefined }));
        return 0;
      }
      return window.setTimeout(() => {
        api<AgentView>(`agents/${id}/check`, "POST", next[id])
          .then((v) => setChecks((c) => ({ ...c, [id]: v })))
          .catch(() => {});
      }, 500);
    });
    return () => timers.forEach((t) => t && clearTimeout(t));
  }, [nextKey]);

  const save = async () => {
    if (!next) return;
    setBusy(true);
    setError("");
    setStatus("");
    try {
      const v = await api<AgentsView>("agents/setup", "PUT", next);
      setView(v);
      const s = setupOf(v);
      setDraft(s);
      setCustom(customOf(s));
      setChecks({});
      setStatus("Saved. New sessions start with these.");
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  };

  if (!view || !draft || !custom)
    return error ? (
      <p role="alert" className="error-text">
        {error}
      </p>
    ) : (
      <p className="subtle">Loading…</p>
    );

  return (
    <form
      onSubmit={(e) => {
        e.preventDefault();
        void save();
      }}
    >
      <p className="subtle agents-intro">
        Which program runs each agent, and the folder it keeps its settings,
        login and conversations in. Automatic uses this computer's PATH and the
        CLI's own default. Changes apply to sessions as they start.
      </p>
      {AGENT_IDS.map((id) => (
        <AgentBlock
          key={id}
          id={id}
          view={view[id]}
          check={checks[id]}
          draft={draft[id]}
          custom={custom[id]}
          dirty={
            !sameSetup({ ...setupOf(view), [id]: next![id] }, setupOf(view))
          }
          disabled={busy}
          onDraft={(d) => setDraft({ ...draft, [id]: { ...draft[id], ...d } })}
          onCustom={(c) =>
            setCustom({ ...custom, [id]: { ...custom[id], ...c } })
          }
          onSignedIn={() => void load()}
        />
      ))}
      {error && (
        <p role="alert" className="error-text">
          {error}
        </p>
      )}
      {status && (
        <p role="status" className="subtle">
          {status}
        </p>
      )}
      <Button type="submit" className="agents-save" disabled={busy || !dirty}>
        {busy ? "Saving…" : "Save agent settings"}
      </Button>
    </form>
  );
}

function AgentBlock({
  id,
  view,
  check,
  draft,
  custom,
  dirty,
  disabled,
  onDraft,
  onCustom,
  onSignedIn,
}: {
  id: AgentId;
  view: AgentView;
  check?: AgentView;
  draft: AgentSetup;
  custom: { path: boolean; home: boolean };
  dirty: boolean;
  disabled: boolean;
  onDraft: (d: AgentSetup) => void;
  onCustom: (c: Partial<{ path: boolean; home: boolean }>) => void;
  onSignedIn: () => void;
}) {
  const name = AGENT_NAMES[id];
  const [browsing, setBrowsing] = useState(false);
  // What is shown is the draft's check while editing, else the saved view.
  const shown = check ?? view;
  const auto = view.program.source === "path" ? view : undefined;
  return (
    <section className="settings-section agent-setup" aria-label={name}>
      <h3>{name}</h3>
      <fieldset className="access-setting" disabled={disabled}>
        <legend>Program</legend>
        <label className="access-option">
          <input
            type="radio"
            name={`${id}-program`}
            checked={!custom.path}
            onChange={() => onCustom({ path: false })}
          />
          <span>
            <strong>Automatic</strong>
            <small>
              {auto?.program.path
                ? `${auto.program.path} · ${auto.program.version || "version unknown"}`
                : view.program.source === "path"
                  ? view.program.error || "Not found on PATH"
                  : `The ${id} on this computer's PATH`}
            </small>
          </span>
        </label>
        <label className="access-option">
          <input
            type="radio"
            name={`${id}-program`}
            checked={custom.path}
            onChange={() => onCustom({ path: true })}
          />
          <span>
            <strong>Choose a program</strong>
            <small>For one CLI version, or one installed elsewhere.</small>
          </span>
        </label>
        {custom.path && (
          <div className="access-secret">
            <Input
              aria-label={`${name} program`}
              list={`${id}-candidates`}
              spellCheck={false}
              autoCapitalize="off"
              placeholder={view.program.candidates[0] ?? `/usr/local/bin/${id}`}
              value={draft.path ?? ""}
              onChange={(e) => onDraft({ path: e.target.value })}
            />
            <datalist id={`${id}-candidates`}>
              {view.program.candidates.map((c) => (
                <option key={c} value={c} />
              ))}
            </datalist>
            {draft.path && (
              <p
                className={shown.program.error ? "warn" : "subtle"}
                role="status"
              >
                {shown.program.error ||
                  (shown.program.version ? `✓ ${shown.program.version}` : "")}
              </p>
            )}
          </div>
        )}
      </fieldset>
      <fieldset className="access-setting" disabled={disabled}>
        <legend>Home folder</legend>
        <label className="access-option">
          <input
            type="radio"
            name={`${id}-home`}
            checked={!custom.home}
            onChange={() => onCustom({ home: false })}
          />
          <span>
            <strong>Automatic</strong>
            <small>{automaticHome(view.home)}</small>
          </span>
        </label>
        <label className="access-option">
          <input
            type="radio"
            name={`${id}-home`}
            checked={custom.home}
            onChange={() => onCustom({ home: true })}
          />
          <span>
            <strong>Choose a folder</strong>
            <small>
              Its own settings, login and conversations, apart from the others.
              The sessions listed change with it.
            </small>
          </span>
        </label>
        {custom.home && (
          <div className="access-secret">
            <div className="secret-row">
              <Input
                aria-label={`${name} home folder`}
                spellCheck={false}
                autoCapitalize="off"
                placeholder={
                  id === "codex" ? "~/.codex-work" : "~/.claude-work"
                }
                value={draft.home ?? ""}
                onChange={(e) => onDraft({ home: e.target.value })}
              />
              <Button
                type="button"
                variant="outline"
                size="sm"
                onClick={() => setBrowsing(!browsing)}
              >
                <FolderOpen size={14} /> Browse
              </Button>
            </div>
            {browsing && (
              <div className="agent-browser">
                <DirBrowser
                  start={draft.home || "~"}
                  hidden
                  onPick={(p) => {
                    onDraft({ home: p });
                    setBrowsing(false);
                  }}
                  onCancel={() => setBrowsing(false)}
                />
              </div>
            )}
            {draft.home && shown.home.error && (
              <p className="warn" role="status">
                {shown.home.error}
                {shown.home.error.includes("create it") &&
                  ` (mkdir -p ${draft.home.trim()})`}
              </p>
            )}
          </div>
        )}
        {view.busy > 0 && (
          <p className="subtle">
            {view.busy === 1
              ? "1 session is at work: the home folder can change when it finishes."
              : `${view.busy} sessions are at work: the home folder can change when they finish.`}
          </p>
        )}
      </fieldset>
      <SignInRow
        id={id}
        view={view}
        shown={shown}
        dirty={dirty}
        onSignedIn={onSignedIn}
      />
    </section>
  );
}

function SignInRow({
  id,
  view,
  shown,
  dirty,
  onSignedIn,
}: {
  id: AgentId;
  view: AgentView;
  shown: AgentView;
  dirty: boolean;
  onSignedIn: () => void;
}) {
  const [login, setLogin] = useState<LoginState | undefined>(view.login);
  const [method, setMethod] = useState("claudeai");
  const [code, setCode] = useState("");
  const [error, setError] = useState("");
  const [copied, setCopied] = useState(false);
  const signedInAt = useRef("");
  useEffect(() => setLogin(view.login), [view.login]);
  // While a sign-in waits for the user, follow it.
  useEffect(() => {
    if (!loginActive(login)) return;
    const t = window.setInterval(() => {
      api<LoginState>(`agents/${id}/login`)
        .then((l) => {
          setLogin(l.state === "none" ? undefined : l);
          if (l.state === "done" && signedInAt.current !== l.started) {
            signedInAt.current = l.started ?? "";
            onSignedIn();
          }
        })
        .catch(() => {});
    }, 1500);
    return () => clearInterval(t);
  }, [login?.state, id]);
  const run = async (fn: () => Promise<LoginState | void>) => {
    setError("");
    try {
      const l = await fn();
      if (l) setLogin(l);
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    }
  };
  const start = () =>
    run(() =>
      api<LoginState>(`agents/${id}/login`, "POST", {
        method: id === "claude" ? method : "",
      }),
    );
  const cancel = () =>
    run(async () => {
      await api(`agents/${id}/login`, "DELETE");
      setLogin(undefined);
    });
  const status = signInText(shown.home.signIn);
  const active = loginActive(login);
  return (
    <div className="agent-signin">
      <div className="agent-signin-head">
        <span className={shown.home.signIn?.signedIn ? "ok" : "subtle"}>
          {status || (shown.home.exists ? "" : "The folder does not exist yet")}
        </span>
        {!active && (
          <span className="agent-signin-start">
            {id === "claude" && (
              <select
                aria-label="Sign in with"
                value={method}
                disabled={dirty}
                onChange={(e) => setMethod(e.target.value)}
              >
                {LOGIN_METHODS.map((m) => (
                  <option key={m.id} value={m.id}>
                    {m.label}
                  </option>
                ))}
              </select>
            )}
            <Button
              type="button"
              variant="outline"
              size="sm"
              disabled={dirty || !view.home.exists}
              title={dirty ? "Save first: sign-in uses the saved folder" : ""}
              onClick={() => void start()}
            >
              {shown.home.signIn?.signedIn ? "Sign in again" : "Sign in"}
            </Button>
          </span>
        )}
      </div>
      {login && login.state !== "none" && (
        <div className="agent-login" role="group" aria-label="Sign-in">
          <p
            className={
              login.state === "failed" || login.state === "expired"
                ? "warn"
                : "subtle"
            }
          >
            {loginText(login)}
          </p>
          {active && login.url && (
            <a href={login.url} target="_blank" rel="noreferrer">
              {login.needsCode ? "Open the sign-in page" : login.url}{" "}
              <ArrowUpRight size={12} aria-hidden />
            </a>
          )}
          {active && login.code && (
            <span className="agent-login-code">
              <code>{login.code}</code>
              <button
                type="button"
                className="about-copy-button"
                aria-label="Copy the code"
                onClick={() =>
                  copyField(`${id}-login-code`, login.code!, () => {
                    setCopied(true);
                    setTimeout(() => setCopied(false), 1200);
                  })
                }
              >
                {copied ? <Check size={13} /> : <Copy size={13} />}
              </button>
            </span>
          )}
          {active && login.needsCode && (
            <div className="secret-row">
              <Input
                aria-label="Code from the sign-in page"
                autoComplete="off"
                spellCheck={false}
                value={code}
                onChange={(e) => setCode(e.target.value)}
              />
              <Button
                type="button"
                size="sm"
                disabled={!code.trim()}
                onClick={() =>
                  void run(async () => {
                    const l = await api<LoginState>(
                      `agents/${id}/login/code`,
                      "POST",
                      { code },
                    );
                    setCode("");
                    return l;
                  })
                }
              >
                Submit code
              </Button>
            </div>
          )}
          {active && (
            <Button
              type="button"
              variant="ghost"
              size="sm"
              onClick={() => void cancel()}
            >
              Cancel sign-in
            </Button>
          )}
        </div>
      )}
      {error && <p className="warn">{error}</p>}
      <div className="agent-terminal">
        <small className="subtle">Or in a terminal on this computer:</small>
        <CopyValue id={`${id}-login-command`} value={view.home.loginCommand} />
      </div>
    </div>
  );
}
