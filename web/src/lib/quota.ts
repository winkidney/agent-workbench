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

export function quotaSummary(
  agent: string,
  usage?: Record<string, any>,
  now = Date.now(),
): { label: string; left: number; reset?: string }[] {
  // Account-wide limits only; model-specific windows remain in the details.
  const pool = usage?.rateLimitsByLimitId?.codex ?? usage?.rateLimits;
  const limits: any[] =
    agent === "codex"
      ? [pool?.primary, pool?.secondary].filter(Boolean).map((w) => ({
          kind:
            w.windowDurationMins === 300
              ? "session"
              : w.windowDurationMins === 10080
                ? "weekly_all"
                : "other",
          percent: w.usedPercent,
          resets_at: w.resetsAt,
        }))
      : (usage?.rate_limits?.limits ?? []);
  return [
    ["session", "5h"],
    ["weekly_all", "Weekly"],
  ].flatMap(([kind, label]) => {
    const q = limits.find((q) => q.kind === kind && !q.scope);
    if (!q || typeof q.percent !== "number" || !Number.isFinite(q.percent))
      return [];
    if (q.resets_at != null && !(resetMillis(q.resets_at) > now)) return [];
    return [
      {
        label,
        left: Math.max(0, Math.min(100, 100 - q.percent)),
        reset: resetTime(q.resets_at, now),
      },
    ];
  });
}
