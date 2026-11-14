import { useEffect, useRef, useState } from "react";
import { RefreshCw } from "lucide-react";
import { api } from "../../lib/api";
import { AGENT_IDS, type AgentId } from "../../lib/agents";
import { AGENT_NAMES } from "../../lib/about";
import {
  quotaWindows,
  quotaExpired,
  resetTime,
  type QuotaWindow,
} from "../../lib/quota";
import { Button } from "../ui/button";

type Usage = {
  usage?: Record<string, any>;
  updated?: string;
  error?: string;
  usageError?: string;
};

export function UsageWindow({ window: q }: { window: QuotaWindow }) {
  const expired = quotaExpired(q);
  return (
    <div className="settings-quota-window">
      <strong>
        {q.label}
        {q.scope ? ` · ${q.scope}` : ""}
      </strong>
      <div>
        {expired
          ? "Awaiting updated usage"
          : q.percent == null
            ? "Usage unavailable"
            : `${q.percent.toFixed(0)}% used · ${(100 - q.percent).toFixed(0)}% left`}
      </div>
      <small>Resets {resetTime(q.resetsAt) ?? "Unavailable"}</small>
    </div>
  );
}

function AgentUsage({ agent, active }: { agent: AgentId; active: boolean }) {
  const [data, setData] = useState<Usage>();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const refresh = useRef<(force?: boolean) => void>(() => {});
  useEffect(() => {
    if (!active) {
      refresh.current = () => {};
      setBusy(false);
      return;
    }
    let live = true,
      pending = false;
    const load = async (force = false) => {
      if (pending || document.visibilityState !== "visible") return;
      pending = true;
      setBusy(true);
      setError("");
      try {
        const next = await api<Usage>(
          `agents/${agent}/usage${force ? "?refresh=1" : ""}`,
        );
        const failure = next.usageError || (!next.usage && next.error);
        if (failure) throw new Error(failure);
        if (live) setData(next);
      } catch (e) {
        if (live) setError(e instanceof Error ? e.message : String(e));
      } finally {
        pending = false;
        if (live) setBusy(false);
      }
    };
    refresh.current = load;
    void load();
    const timer = setInterval(() => void load(), 60000);
    const visible = () => {
      if (document.visibilityState === "visible") void load();
    };
    document.addEventListener("visibilitychange", visible);
    return () => {
      live = false;
      clearInterval(timer);
      document.removeEventListener("visibilitychange", visible);
      refresh.current = () => {};
    };
  }, [active, agent]);
  const windows = quotaWindows(agent, data?.usage);
  return (
    <section
      className="settings-section"
      aria-label={`${AGENT_NAMES[agent]} usage`}
    >
      <div className="settings-usage-head">
        <h3>{AGENT_NAMES[agent]}</h3>
        <Button
          type="button"
          variant="outline"
          size="sm"
          disabled={busy}
          onClick={() => refresh.current(true)}
          aria-label={`Refresh ${AGENT_NAMES[agent]} usage`}
        >
          <RefreshCw size={13} className={busy ? "spin" : undefined} />
          {busy ? "Refreshing…" : "Refresh"}
        </Button>
      </div>
      {error && (
        <p role="alert" className="error-text">
          {data ? "Update failed. Showing the last successful reading. " : ""}
          {error}
        </p>
      )}
      {windows.map((q, i) => (
        <UsageWindow key={i} window={q} />
      ))}
      {!windows.length && (
        <p className="subtle">
          {busy && !data
            ? "Loading usage…"
            : error && !data
              ? "Usage unavailable. Check this CLI in Settings → Agents."
              : "No quota windows reported. This does not establish unlimited usage."}
        </p>
      )}
      <p className="subtle">
        Last successful update:{" "}
        {data?.updated
          ? new Date(data.updated).toLocaleString()
          : "Unavailable"}
      </p>
    </section>
  );
}

export function UsageTab({ active }: { active: boolean }) {
  return (
    <>
      <p className="subtle">
        Account limits reported by each configured CLI. Refresh reads the latest
        usage; it does not reset your allowance.
      </p>
      {AGENT_IDS.map((agent) => (
        <AgentUsage key={agent} agent={agent} active={active} />
      ))}
    </>
  );
}
