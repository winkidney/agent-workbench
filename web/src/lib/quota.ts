// CLI reset timestamps are Unix seconds (Codex) or ISO strings (Claude).
function resetMillis(value: unknown): number {
  return typeof value === "number"
    ? value * 1000
    : typeof value === "string"
      ? Date.parse(value)
      : NaN;
}

export function resetTime(
  value: unknown,
  now = Date.now(),
): string | undefined {
  const at = resetMillis(value);
  if (!Number.isFinite(at)) return undefined;
  const minutes = Math.max(0, Math.ceil((at - now) / 60000));
  const days = Math.floor(minutes / 1440);
  return `${new Date(at).toLocaleString()} (in ${days ? `${days}d ` : ""}${Math.floor((minutes % 1440) / 60)}h ${minutes % 60}m)`;
}

export type QuotaWindow = {
  kind: string;
  label: string;
  scope?: string;
  percent?: number;
  resetsAt?: string | number;
};

// One interpretation of the CLI data for Settings, details and the footer.
export function quotaWindows(
  agent: string,
  usage?: Record<string, any>,
): QuotaWindow[] {
  const percent = (value: unknown) =>
    typeof value === "number" && Number.isFinite(value)
      ? Math.max(0, Math.min(100, value))
      : undefined;
  if (agent === "codex") {
    const pools = { ...(usage?.rateLimitsByLimitId ?? {}) };
    if (!pools.codex && usage?.rateLimits) pools.codex = usage.rateLimits;
    return Object.entries(pools).flatMap(([id, raw]) => {
      const pool = raw as any;
      if (!pool) return [];
      return [pool.primary, pool.secondary].filter(Boolean).map((w) => ({
        kind:
          w.windowDurationMins === 300
            ? "session"
            : w.windowDurationMins === 10080
              ? "weekly_all"
              : String(w.windowDurationMins),
        label:
          w.windowDurationMins === 300
            ? "5h"
            : w.windowDurationMins === 10080
              ? "Weekly"
              : w.windowDurationMins > 0
                ? `${w.windowDurationMins}m`
                : "Quota",
        scope:
          id === "codex" ? undefined : pool.limitName || pool.limitId || id,
        percent: percent(w.usedPercent),
        resetsAt: w.resetsAt,
      }));
    });
  }
  const limits = usage?.rate_limits?.limits;
  return (Array.isArray(limits) ? limits : []).filter(Boolean).map((q) => ({
    kind: q.kind,
    label:
      q.kind === "session"
        ? "5h"
        : q.kind === "weekly_all" || q.kind === "weekly_scoped"
          ? "Weekly"
          : String(q.kind || "Quota"),
    scope:
      typeof q.scope === "string"
        ? q.scope
        : q.scope?.model?.display_name ||
          (q.kind === "weekly_scoped" ? "Model-specific" : undefined),
    percent: percent(q.percent),
    resetsAt: q.resets_at,
  }));
}

export function quotaExpired(q: QuotaWindow, now = Date.now()): boolean {
  return q.resetsAt != null && !(resetMillis(q.resetsAt) > now);
}

export function quotaSummary(
  agent: string,
  usage?: Record<string, any>,
  now = Date.now(),
): { label: string; left: number; reset?: string }[] {
  const windows = quotaWindows(agent, usage);
  return ["session", "weekly_all"].flatMap((kind) => {
    const q = windows.find((q) => q.kind === kind && !q.scope);
    if (!q || q.percent == null || quotaExpired(q, now)) return [];
    return [
      {
        label: q.label,
        left: 100 - q.percent,
        reset: resetTime(q.resetsAt, now),
      },
    ];
  });
}
