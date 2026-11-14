export type ResetCredit = {
  id: string;
  title?: string;
  description?: string;
  status: string;
  resetType: string;
  expiresAt?: number | null;
};
export function bankedResets(usage?: Record<string, any>) {
  const raw = usage?.rateLimitResetCredits;
  const count =
    typeof raw?.availableCount === "number" &&
    Number.isFinite(raw.availableCount) &&
    raw.availableCount >= 0
      ? raw.availableCount
      : undefined;
  const credits: ResetCredit[] = Array.isArray(raw?.credits)
    ? raw.credits
        .filter((c: any) => c && typeof c.id === "string")
        .slice()
        .sort(
          (a: ResetCredit, b: ResetCredit) =>
            (a.expiresAt ?? Infinity) - (b.expiresAt ?? Infinity),
        )
    : [];
  return { count, credits, partial: count != null && count > credits.length };
}
export function resetUsable(c: ResetCredit, now = Date.now()) {
  return (
    c.status === "available" &&
    c.resetType === "codexRateLimits" &&
    (c.expiresAt === null ||
      (typeof c.expiresAt === "number" && c.expiresAt * 1000 > now))
  );
}
