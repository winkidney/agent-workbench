import { useEffect, useState } from "react";
import { ArrowUpRight, Check, Copy, RefreshCw } from "lucide-react";
import { api } from "../../lib/api";
import {
  AGENT_NAMES,
  ENGINES,
  bytes,
  diagnostics,
  newer,
  uptime,
  type About,
  type AboutAgent,
} from "../../lib/about";
import { copyField } from "../../lib/copy";
import { Button } from "../ui/button";

const HOME_SOURCES: Record<string, string> = {
  setting: " (chosen in Settings)",
  environment: " (from the environment)",
  default: "",
};

const FEATURES: Record<string, string> = {
  "--prompt-suggestions": "Prompt suggestions",
  "--include-hook-events": "Hook events",
};

export function CopyValue({ id, value }: { id: string; value: string }) {
  const [done, setDone] = useState(false);
  return (
    <span className="about-copy">
      <input id={id} readOnly value={value} aria-label={id} />
      <button
        type="button"
        className="about-copy-button"
        aria-label="Copy"
        title={done ? "Copied" : "Copy"}
        onClick={() =>
          copyField(id, value, () => {
            setDone(true);
            setTimeout(() => setDone(false), 1200);
          })
        }
      >
        {done ? <Check size={13} /> : <Copy size={13} />}
      </button>
    </span>
  );
}

// Exported for tests.
export function AgentRow({ agent }: { agent: AboutAgent }) {
  const name = AGENT_NAMES[agent.name] ?? agent.name;
  return (
    <div className="about-agent">
      <div className="about-agent-head">
        <strong>{name}</strong>
        {agent.path ? (
          <span>{agent.version || agent.error || "version unknown"}</span>
        ) : (
          <span className="warn">{agent.error || "Not found"}</span>
        )}
      </div>
      {agent.path && (
        <CopyValue id={`about-path-${agent.name}`} value={agent.path} />
      )}
      {(agent.source === "setting" || agent.home) && (
        <p className="subtle about-agent-home">
          {agent.source === "setting" && "Program chosen in Settings. "}
          {agent.home &&
            `Home: ${agent.home}${HOME_SOURCES[agent.homeSource ?? "default"]}`}
        </p>
      )}
      {(agent.features?.length || agent.missing?.length) && (
        <ul className="about-features" aria-label={`${name} features`}>
          {agent.features?.map((f) => (
            <li key={f}>
              <Check size={12} aria-hidden /> {FEATURES[f] ?? f}
            </li>
          ))}
          {agent.missing?.map((f) => (
            <li key={f} className="warn">
              {FEATURES[f] ?? f}: needs a newer {name}
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}

type Update = { current: string; latest: string; url?: string };

// About: this workbench, the agents it runs, its data; read when the tab
// is shown, and gone with the dialog.
export function AboutTab({ active }: { active: boolean }) {
  const [about, setAbout] = useState<About | null>(null);
  const [error, setError] = useState("");
  const [update, setUpdate] = useState<Update | null>(null);
  const [checking, setChecking] = useState(false);
  const [updateError, setUpdateError] = useState("");
  const [copied, setCopied] = useState(false);
  useEffect(() => {
    if (!active || about) return;
    let live = true;
    api<About>("about")
      .then((v) => live && setAbout(v))
      .catch(
        (e) => live && setError(e instanceof Error ? e.message : String(e)),
      );
    return () => {
      live = false;
    };
  }, [active, about]);
  if (error)
    return (
      <p role="alert" className="error-text">
        {error}
      </p>
    );
  if (!about) return <p className="subtle">Loading…</p>;
  const w = about.workbench;
  const release = /^v\d+\.\d+\.\d+/.test(w.version);
  const check = async () => {
    setChecking(true);
    setUpdateError("");
    try {
      setUpdate(await api<Update>("updates"));
    } catch (e) {
      setUpdateError(e instanceof Error ? e.message : String(e));
    } finally {
      setChecking(false);
    }
  };
  const report = diagnostics(about);
  const sessions = about.data.sessions ?? {};
  return (
    <>
      <section className="settings-section">
        <h3>Agent Workbench</h3>
        <dl className="about-list">
          <dt>Version</dt>
          <dd>
            {w.version}
            {release && w.repo && (
              <a
                className="about-link"
                href={`${w.repo}/releases/tag/${w.version}`}
                target="_blank"
                rel="noreferrer"
              >
                Release notes <ArrowUpRight size={12} aria-hidden />
              </a>
            )}
          </dd>
          {w.build && (
            <>
              <dt>Web build</dt>
              <dd>
                <code>{w.build}</code>
              </dd>
            </>
          )}
          {w.started && (
            <>
              <dt>Running for</dt>
              <dd title={new Date(w.started).toLocaleString()}>
                {uptime(w.started)}
              </dd>
            </>
          )}
          {w.engines && (
            <>
              <dt>Engines</dt>
              <dd>{ENGINES[w.engines] ?? w.engines}</dd>
            </>
          )}
          <dt>Platform</dt>
          <dd>
            {w.platform} · {w.go}
          </dd>
          {w.instance && w.instance !== "agent-workbench" && (
            <>
              <dt>Instance</dt>
              <dd>
                <code>{w.instance}</code>
              </dd>
            </>
          )}
          {w.listen && (
            <>
              <dt>Address</dt>
              <dd>
                <code>{w.listen}</code>
              </dd>
            </>
          )}
          {w.dataDir && (
            <>
              <dt>Data directory</dt>
              <dd>
                <CopyValue id="about-data-dir" value={w.dataDir} />
              </dd>
            </>
          )}
          {w.autostart && (
            <>
              <dt>Start at login</dt>
              <dd>{w.autostart}</dd>
            </>
          )}
        </dl>
        <div className="about-update">
          <Button
            type="button"
            variant="outline"
            size="sm"
            disabled={checking}
            onClick={() => void check()}
          >
            <RefreshCw size={14} className={checking ? "spin" : undefined} />
            Check for updates
          </Button>
          {update &&
            (newer(update.current, update.latest) ? (
              <a href={update.url} target="_blank" rel="noreferrer">
                {update.latest} is available{" "}
                <ArrowUpRight size={12} aria-hidden />
              </a>
            ) : release ? (
              <span className="subtle">You have the latest release.</span>
            ) : (
              <span className="subtle">
                A development build; the latest release is {update.latest}.
              </span>
            ))}
          {updateError && <span className="warn">{updateError}</span>}
        </div>
      </section>
      <section className="settings-section">
        <h3>Agents</h3>
        {about.agents.map((a) => (
          <AgentRow key={a.name} agent={a} />
        ))}
      </section>
      <section className="settings-section">
        <h3>Data</h3>
        <dl className="about-list">
          <dt>Local data</dt>
          <dd>{bytes(about.data.storageBytes)}</dd>
          <dt>Live events kept</dt>
          <dd>{about.data.retentionDays ?? 7} days</dd>
          <dt>Sessions</dt>
          <dd>
            Claude {sessions.claude ?? 0} · Codex {sessions.codex ?? 0}
            {sessions.subagents ? ` · subagents ${sessions.subagents}` : ""}
          </dd>
        </dl>
        <p className="subtle">
          Conversations stay in each CLI's own records; the workbench keeps
          settings and a short buffer of live events.
        </p>
      </section>
      <section className="settings-section">
        <h3>Diagnostics</h3>
        <textarea
          id="about-diagnostics"
          className="about-diagnostics"
          readOnly
          rows={6}
          value={report}
          aria-label="Diagnostics"
        />
        <div className="about-update">
          <Button
            type="button"
            variant="outline"
            size="sm"
            onClick={() =>
              copyField("about-diagnostics", report, () => {
                setCopied(true);
                setTimeout(() => setCopied(false), 1200);
              })
            }
          >
            {copied ? <Check size={14} /> : <Copy size={14} />}
            Copy diagnostics
          </Button>
          {w.repo && (
            <a href={w.repo} target="_blank" rel="noreferrer">
              GitHub <ArrowUpRight size={12} aria-hidden />
            </a>
          )}
          {w.license && <span className="subtle">{w.license} license</span>}
        </div>
      </section>
    </>
  );
}
