import { useEffect, useState } from "react";
import { Loader2 } from "lucide-react";
import {
  Brain,
  ChevronDown,
  ListChecks,
  Shield,
  Zap,
  type LucideIcon,
} from "lucide-react";
import { Button } from "./components/ui/button";
import {
  defaultLabel,
  findModel,
  modelName,
  optionName,
  resolveOption,
  type OptionItem,
  type Options,
} from "./agentOptions";
export type { Options } from "./agentOptions";

const ICONS: Partial<Record<keyof Options, LucideIcon>> = {
  effort: Brain,
  speed: Zap,
  mode: ListChecks,
  permissions: Shield,
};
type Model = {
  resolvedModel?: string;
  isDefault?: boolean;
  id?: string;
  model?: string;
  value?: string;
  displayName: string;
  supportedReasoningEfforts?: { reasoningEffort: string }[];
  supportedEffortLevels?: string[];
  serviceTiers?: {
    id?: string;
    serviceTier?: string;
    name?: string;
    displayName?: string;
  }[];
  supportsFastMode?: boolean;
};
type Usage = {
  options: Options;
  effective: Options;
  pending: boolean;
  total?: Record<string, number>;
  last?: Record<string, number>;
  context?: {
    used: number;
    window: number;
    estimated: boolean;
    autoCompactThreshold?: number;
    categories?: { name: string; tokens: number }[];
  };
  speed: number;
  average?: number;
  scope: string;
  updated?: string;
  // Usage is being read from a compressed record; it follows.
  reading?: boolean;
  // What the user would likely type next, after a turn.
  suggestion?: string;
};
type Meta = {
  models?: Model[];
  permissions?: {
    id: string;
    name?: string;
    displayName?: string;
    allowed?: boolean;
    disabledReason?: string;
  }[];
  modes?: { mode: string; name: string }[];
  usage?: Record<string, any>;
  // What the CLI uses for each setting without an override.
  defaults?: Options;
  updated?: string;
  error?: string;
};
async function request<T>(
  path: string,
  method = "GET",
  body?: unknown,
): Promise<T> {
  const r = await fetch("/api/" + path, {
    method,
    headers: body ? { "Content-Type": "application/json" } : {},
    body: body ? JSON.stringify(body) : undefined,
  });
  if (!r.ok) throw new Error(await r.text());
  return r.json();
}
const count = (n?: number) =>
  n == null
    ? "Unavailable"
    : new Intl.NumberFormat("en", {
        notation: "compact",
        maximumFractionDigits: 1,
      }).format(n);
function tokens(u?: Record<string, number>) {
  if (!u) return undefined;
  if (u.totalTokens != null) return u.totalTokens;
  if (u.total_tokens != null) return u.total_tokens;
  const input = u.inputTokens ?? u.input_tokens ?? 0;
  return (
    input +
    (u.outputTokens ?? u.output_tokens ?? 0) +
    (u.cacheReadInputTokens ?? 0) +
    (u.cacheCreationInputTokens ?? 0) +
    (u.input_tokens != null
      ? (u.cache_read_input_tokens ?? 0) + (u.cache_creation_input_tokens ?? 0)
      : 0)
  );
}
export function useAgentControls({
  session,
}: {
  session: {
    id: string;
    agent: string;
    cwd: string;
    readOnly?: boolean;
    archived?: boolean;
    status: string;
  };
}) {
  const [meta, setMeta] = useState<Meta>({}),
    [data, setData] = useState<Usage>(),
    [options, setOptions] = useState<Options>({}),
    // Which session the capabilities and settings above were loaded for:
    // until both are this session's, the pills show "Loading…" rather than
    // what the previous session (or agent) had.
    [metaFor, setMetaFor] = useState(""),
    [dataFor, setDataFor] = useState(""),
    [error, setError] = useState(""),
    [saving, setSaving] = useState(false);
  const loading = metaFor !== session.id || dataFor !== session.id;
  useEffect(() => {
    let cancel = false;
    const id = session.id;
    setMeta({});
    setData(undefined);
    setOptions({});
    setError("");
    const load = (refresh: boolean) => {
      if (refresh && document.visibilityState !== "visible") return;
      request<Meta>(
        `agents/${session.agent}/capabilities?cwd=${encodeURIComponent(session.cwd)}`,
      )
        .then((v) => {
          if (!cancel) {
            setMeta(v);
            setMetaFor(id);
          }
        })
        .catch((e) => {
          if (!cancel) {
            setError(e.message);
            setMetaFor(id);
          }
        });
    };
    load(false);
    const timer = setInterval(() => load(true), 60000);
    request<Usage>(`sessions/${session.id}/telemetry`)
      .then((v) => {
        if (!cancel) {
          setData(v);
          setOptions(v.options);
          setDataFor(id);
        }
      })
      .catch((e) => {
        if (!cancel) {
          setError(e.message);
          setDataFor(id);
        }
      });
    const update = (e: Event) => {
      const v = (e as CustomEvent).detail;
      if (v.session !== session.id) return;
      setData(v.data);
      // Settings changed elsewhere (a /plan command, another page).
      if (v.data?.options) setOptions(v.data.options);
    };
    window.addEventListener("workbench:telemetry", update);
    const changed = (e: Event) => {
      const v = (e as CustomEvent).detail;
      if (v.session === session.id) setOptions(v.options);
    };
    window.addEventListener("workbench:options", changed);
    return () => {
      cancel = true;
      clearInterval(timer);
      window.removeEventListener("workbench:telemetry", update);
      window.removeEventListener("workbench:options", changed);
    };
  }, [session.id, session.agent, session.cwd]);
  useEffect(() => {
    if (session.status === "idle")
      void request<Usage>(`sessions/${session.id}/telemetry`)
        .then(setData)
        .catch(() => {});
  }, [session.id, session.status]);
  const save = async (next: Options) => {
    setSaving(true);
    setError("");
    try {
      const v = await request<Usage>(
        `sessions/${session.id}/options`,
        "PATCH",
        next,
      );
      setData(v);
      setOptions(v.options);
    } catch (e) {
      setError(String(e));
    } finally {
      setSaving(false);
    }
  };
  const live = data?.effective;
  const modelId = resolveOption("model", options, live, meta.defaults);
  // An unlisted model still gets the default model's effort and speed lists.
  const chosenModel =
    findModel(meta.models, modelId) ?? findModel(meta.models, "");
  const effort =
    chosenModel?.supportedEffortLevels ??
    chosenModel?.supportedReasoningEfforts?.map((e) => e.reasoningEffort) ??
    [];
  const quota: any[] =
    session.agent === "codex"
      ? Object.values(
          meta.usage?.rateLimitsByLimitId ?? { codex: meta.usage?.rateLimits },
        )
          .filter(Boolean)
          .flatMap((v: any) =>
            [v.primary, v.secondary].filter(Boolean).map((w: any) => ({
              kind:
                w.windowDurationMins === 10080
                  ? "Weekly"
                  : `${w.windowDurationMins}m`,
              percent: w.usedPercent,
              resets_at: w.resetsAt,
              scope: v.limitName ?? v.limitId,
            })),
          )
      : (meta.usage?.rate_limits?.limits ?? []);
  const freshQuota = quota.filter(
    (q) =>
      !q.resets_at ||
      new Date(
        typeof q.resets_at === "number" ? q.resets_at * 1000 : q.resets_at,
      ).getTime() > Date.now(),
  );
  const weekly = freshQuota.find(
    (q) => q.kind === "Weekly" || q.kind === "weekly_all",
  );
  const select = (label: string, key: keyof Options, items: OptionItem[]) => {
    const override = options[key] ?? "";
    // Choosing Default hands the setting back to the CLI. Live values already
    // include applied overrides, so an overridden setting falls back to the
    // configured default instead.
    const fallback = override
      ? resolveOption(key, meta.defaults)
      : resolveOption(key, live, meta.defaults);
    const name = (v: string) =>
      key === "model" ? modelName(meta.models, v) : optionName(key, v, items);
    return (
      <OptionPill
        label={label}
        icon={ICONS[key]}
        override={override}
        shown={name(override || fallback)}
        fallback={name(fallback)}
        items={items}
        loading={loading}
        disabled={saving || session.readOnly || session.archived}
        onChange={(value) =>
          void save({
            ...options,
            ...(key === "model" ? { effort: "", speed: "" } : {}),
            [key]: value,
          })
        }
      />
    );
  };
  const resetTime = (value: string | number) => {
    const d = new Date(typeof value === "number" ? value * 1000 : value);
    const m = Math.max(0, Math.ceil((d.getTime() - Date.now()) / 60000));
    return `${d.toLocaleString()} (in ${Math.floor(m / 60)}h ${m % 60}m)`;
  };
  return {
    meta,
    data,
    // This session's, not the previous one's while it loads.
    suggestion: dataFor === session.id ? data?.suggestion : undefined,
    options,
    loading,
    error,
    setError,
    saving,
    save,
    chosenModel,
    effort,
    quota,
    weekly,
    select,
    resetTime,
  };
}

// One setting as a compact pill: an icon names the setting and the text shows
// the value in effect. CLI defaults are muted; the user's overrides are not.
// The native select covers the pill, so its first entry can read
// "Default (low)" while the pill itself shows just "low".
function OptionPill({
  label,
  ariaLabel,
  icon: Icon,
  override,
  shown,
  fallback,
  items,
  loading,
  disabled,
  onChange,
}: {
  label: string;
  ariaLabel?: string;
  icon?: LucideIcon;
  override: string;
  shown: string;
  fallback: string;
  items: OptionItem[];
  // The agent's choices are not known yet: nothing to show or pick.
  loading?: boolean;
  disabled?: boolean;
  onChange: (value: string) => void;
}) {
  if (loading)
    return (
      <label className="agent-option is-loading" title={`${label}: loading`}>
        {Icon && <Icon size={13} className="agent-option-icon" aria-hidden />}
        <Loader2 size={11} className="agent-option-spin" aria-hidden />
        <span className="agent-value">Loading…</span>
        <select aria-label={ariaLabel ?? label} value="" disabled>
          <option value="">Loading…</option>
        </select>
      </label>
    );
  return (
    <label
      className={"agent-option " + (override ? "is-set" : "is-default")}
      title={`${label}: ${shown || "unknown"}${override ? "" : " (CLI default)"}`}
    >
      {Icon && <Icon size={13} className="agent-option-icon" aria-hidden />}
      <span className="agent-value" aria-hidden>
        {shown || "—"}
      </span>
      <ChevronDown size={10} className="agent-option-caret" aria-hidden />
      <select
        aria-label={ariaLabel ?? label}
        value={override}
        disabled={disabled}
        onChange={(e) => onChange(e.target.value)}
      >
        <option value="">{defaultLabel(fallback)}</option>
        {items.map((i) => (
          <option key={i.value} value={i.value} disabled={i.disabled}>
            {i.label}
          </option>
        ))}
      </select>
    </label>
  );
}

export type AgentControlsState = ReturnType<typeof useAgentControls>;
type Part = {
  ctl: AgentControlsState;
  session: {
    id: string;
    agent: string;
    cwd: string;
    readOnly?: boolean;
    status: string;
  };
};

// Model / effort / speed / mode / permission pills for the composer footer.
export function AgentSelectors({ ctl, session }: Part) {
  const { meta, data, chosenModel, effort, select, loading } = ctl;
  return (
    <div className="agent-selectors">
      {select(
        "Model",
        "model",
        (meta.models ?? []).map((m) => ({
          value: m.value ?? m.model ?? m.id ?? "",
          label: m.displayName,
        })),
      )}
      {(loading || effort.length > 0) &&
        select(
          "Effort",
          "effort",
          effort.map((v) => ({ value: v, label: v })),
        )}
      {(chosenModel?.supportsFastMode || chosenModel?.serviceTiers?.length) &&
        select(
          "Speed",
          "speed",
          session.agent === "claude"
            ? [
                { value: "standard", label: "Standard" },
                { value: "fast", label: "Fast" },
              ]
            : (
                [
                  { id: "default", name: "Standard" },
                  ...(chosenModel?.serviceTiers ?? []),
                ] as NonNullable<Model["serviceTiers"]>
              ).map((t) => ({
                value: t.id ?? t.serviceTier ?? "default",
                label:
                  t.displayName ??
                  t.name ??
                  t.id ??
                  t.serviceTier ??
                  "Standard",
              })),
        )}
      {select(
        "Mode",
        "mode",
        (meta.modes ?? []).map((v) => ({ value: v.mode, label: v.name })),
      )}
      {select(
        "Permissions",
        "permissions",
        (meta.permissions ?? []).map((v) => ({
          value: v.id,
          label: optionName("permissions", v.id, [
            { value: v.id, label: v.displayName ?? v.name ?? v.id },
          ]),
          disabled: v.allowed === false,
        })),
      )}
      {data?.pending && <span className="subtle">Pending — next turn</span>}
    </div>
  );
}

// One-line usage summary under the composer; opens the full usage panel.
export function UsageDetails({ ctl, session, hint }: Part & { hint?: string }) {
  const { meta, data, options, quota, weekly, resetTime, setError, error } =
    ctl;
  return (
    <>
      <details className="usage-details">
        <summary>
          {hint && <span className="usage-hint">{hint}</span>}
          {session.status === "running" && data?.speed
            ? `~${data.speed.toFixed(1)} tok/s · `
            : !["running", "waiting"].includes(session.status) && data?.average
              ? `${data.average.toFixed(1)} tok/s avg · `
              : ""}
          {data?.reading ? (
            "Reading usage from the compressed record…"
          ) : (
            <>
              {count(tokens(data?.total))} tokens · Context{" "}
              {data?.context?.window
                ? `${Math.round(((data.context.used ?? 0) / data.context.window) * 100)}%`
                : "unavailable"}
            </>
          )}
          {weekly
            ? ` · Weekly ${Math.max(0, 100 - weekly.percent).toFixed(0)}% left`
            : ""}
        </summary>
        <div className="usage-body">
          <p>Current model: {data?.effective.model || "CLI default"}</p>
          <p title={data?.scope}>
            Session tokens: {count(tokens(data?.total))} · This turn:{" "}
            {count(tokens(data?.last))}
          </p>
          {data?.total && (
            <dl>
              {Object.entries(data.total)
                .filter(([k]) => /token/i.test(k))
                .map(([k, v]) => (
                  <div key={k}>
                    <dt>{k.replace(/_/g, " ").replace(/([A-Z])/g, " $1")}</dt>
                    <dd>{count(v)}</dd>
                  </div>
                ))}
            </dl>
          )}
          <p>
            Context: {data?.context?.estimated ? "~" : ""}
            {count(data?.context?.used)} / {count(data?.context?.window)}
          </p>
          {data?.context?.autoCompactThreshold != null && (
            <p>
              Auto-compact threshold: {count(data.context.autoCompactThreshold)}
            </p>
          )}
          {data?.context?.categories && (
            <dl>
              {data.context.categories.map((c, i) => (
                <div key={i}>
                  <dt>{c.name}</dt>
                  <dd>{count(c.tokens)}</dd>
                </div>
              ))}
            </dl>
          )}
          <p className="subtle">
            Live speed estimates visible output over five seconds. Turn average
            uses reported output tokens and elapsed turn time, including tools
            and approvals. {data?.scope}
          </p>
          {quota.length ? (
            quota.map((q, i) => (
              <p key={i}>
                {q.kind}{" "}
                {typeof q.scope === "string"
                  ? q.scope
                  : q.scope?.model?.display_name}{" "}
                · {Math.max(0, 100 - q.percent).toFixed(1)}% left · Resets{" "}
                {q.resets_at ? resetTime(q.resets_at) : "Unavailable"}
              </p>
            ))
          ) : (
            <p>
              Quota unavailable for this account or no windows have been
              reported yet.
            </p>
          )}
          <p className="subtle">
            Last updated:{" "}
            {meta.updated
              ? new Date(meta.updated).toLocaleString()
              : "Unavailable"}
          </p>
          <Button
            type="button"
            variant="outline"
            size="sm"
            onClick={() =>
              void request(`agents/${session.agent}/defaults`, "PUT", options)
                .then(() =>
                  setError("Saved as the default for new Workbench sessions."),
                )
                .catch((e) => setError(String(e)))
            }
          >
            Use as default for new sessions
          </Button>
        </div>
      </details>
      {(error || meta.error) && (
        <span role="status" className="usage-error">
          {error || meta.error}
        </span>
      )}
    </>
  );
}

// Context window occupancy as a small ring.
export function ContextRing({ ctl }: { ctl: AgentControlsState }) {
  const c = ctl.data?.context;
  if (!c?.window) return null;
  const used = Math.min(1, (c.used ?? 0) / c.window);
  const r = 7;
  const len = 2 * Math.PI * r;
  return (
    <span
      className={"context-ring" + (used > 0.8 ? " high" : "")}
      title={`Context ${Math.round(used * 100)}% · ${count(c.used)} / ${count(c.window)}`}
      aria-label={`Context ${Math.round(used * 100)}% used`}
      role="img"
    >
      <svg width="18" height="18" viewBox="0 0 18 18">
        <circle cx="9" cy="9" r={r} className="ring-track" />
        <circle
          cx="9"
          cy="9"
          r={r}
          className="ring-value"
          strokeDasharray={`${used * len} ${len}`}
          transform="rotate(-90 9 9)"
        />
      </svg>
      <span className="ring-label">{Math.round(used * 100)}%</span>
    </span>
  );
}
export function NewSessionOptions({
  agent,
  cwd,
  value,
  onChange,
}: {
  agent: string;
  cwd: string;
  value: Options;
  onChange: (o: Options) => void;
}) {
  // Capabilities and saved defaults count only for the agent and directory
  // they were loaded for; until then the pills show "Loading…", not the
  // previous agent's models.
  const key = agent + "\n" + cwd;
  const [loaded, setLoaded] = useState<{ key: string; meta: Meta }>({
    key: "",
    meta: {},
  });
  const [defaultsFor, setDefaultsFor] = useState("");
  const meta: Meta = loaded.key === key ? loaded.meta : {};
  const loading = loaded.key !== key || defaultsFor !== agent;
  useEffect(() => {
    let cancelled = false;
    const timer = setTimeout(() => {
      void request<Meta>(
        `agents/${agent}/capabilities?cwd=${encodeURIComponent(cwd || "/tmp")}`,
      )
        .then((v) => {
          if (!cancelled) setLoaded({ key, meta: v });
        })
        .catch(() => {
          if (!cancelled) setLoaded({ key, meta: {} });
        });
    }, 600);
    return () => {
      cancelled = true;
      clearTimeout(timer);
    };
  }, [agent, cwd, key]);
  useEffect(() => {
    let cancelled = false;
    void request<Options>(`agents/${agent}/defaults`)
      .then((v) => {
        if (!cancelled) {
          onChange(v);
          setDefaultsFor(agent);
        }
      })
      .catch(() => {
        if (!cancelled) setDefaultsFor(agent);
      });
    return () => {
      cancelled = true;
    };
  }, [agent]);
  const model =
    findModel(meta.models, resolveOption("model", value, meta.defaults)) ??
    findModel(meta.models, "");
  const efforts =
    model?.supportedEffortLevels ??
    model?.supportedReasoningEfforts?.map((e) => e.reasoningEffort) ??
    [];
  return (
    <div className="agent-selectors">
      {(
        [
          [
            "Model",
            "model",
            meta.models?.map((m) => ({
              value: m.value ?? m.model ?? m.id ?? "",
              label: m.displayName,
            })) ?? [],
          ],
          ["Effort", "effort", efforts.map((v) => ({ value: v, label: v }))],
          [
            "Mode",
            "mode",
            meta.modes?.map((m) => ({ value: m.mode, label: m.name })) ?? [],
          ],
          [
            "Permissions",
            "permissions",
            meta.permissions
              ?.filter((p) => p.allowed !== false)
              .map((p) => ({
                value: p.id,
                label: optionName("permissions", p.id, [
                  { value: p.id, label: p.displayName ?? p.name ?? p.id },
                ]),
              })) ?? [],
          ],
        ] as [string, keyof Options, OptionItem[]][]
      )
        .filter(
          ([, key, items]) => key !== "effort" || loading || items.length > 0,
        )
        .map(([label, key, items]) => {
          const override = value[key] ?? "";
          const fallback = resolveOption(key, meta.defaults);
          const name = (v: string) =>
            key === "model"
              ? modelName(meta.models, v)
              : optionName(key, v, items);
          return (
            <OptionPill
              key={key}
              label={label}
              ariaLabel={`New session ${label}`}
              icon={ICONS[key]}
              override={override}
              shown={name(override || fallback)}
              fallback={name(fallback)}
              items={items}
              loading={loading}
              onChange={(v) =>
                onChange({
                  ...value,
                  ...(key === "model" ? { effort: "", speed: "" } : {}),
                  [key]: v,
                })
              }
            />
          );
        })}
    </div>
  );
}
