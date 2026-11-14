import { useEffect, useRef, useState } from "react";
import { api } from "../lib/api";
import { randomUUID } from "../lib/uuid";
import { bankedResets, resetUsable, type ResetCredit } from "../lib/resets";
import { Dialog, DialogContent, DialogTitle } from "./ui/dialog";
import { Button } from "./ui/button";

type Attempt = { accountId: string; creditId?: string; idempotencyKey: string };
export function BankedResets({
  agent,
  usage,
  onRefresh,
}: {
  agent: string;
  usage?: Record<string, any>;
  onRefresh: () => void;
}) {
  const [choice, setChoice] = useState<{ credit?: ResetCredit } | null>(null);
  const [busy, setBusy] = useState(false);
  const working = useRef(false);
  const [message, setMessage] = useState("");
  const [uncertain, setUncertain] = useState(false);
  const { count, credits, partial } = bankedResets(usage);
  const accountId = typeof usage?.accountId === "string" ? usage.accountId : "";
  const storageKey = "workbench:reset-attempt:" + accountId;
  useEffect(() => {
    try {
      const pending = !!sessionStorage.getItem(storageKey);
      setUncertain(pending);
      if (pending)
        setMessage(
          "An earlier attempt is unresolved. Retry the same attempt to check its result.",
        );
    } catch {}
  }, [storageKey]);
  const consume = async () => {
    if (working.current || !accountId) return;
    working.current = true;
    setBusy(true);
    setMessage("");
    try {
      const saved = sessionStorage.getItem(storageKey);
      const attempt: Attempt = saved
        ? JSON.parse(saved)
        : {
            accountId,
            creditId: choice?.credit?.id,
            idempotencyKey: randomUUID(),
          };
      if (attempt.accountId !== accountId)
        throw Error("Account changed. Refresh Usage first.");
      sessionStorage.setItem(storageKey, JSON.stringify(attempt));
      const result = await api<{
        outcome: string;
        usageError?: string;
        message?: string;
      }>("agents/codex/usage/reset", "POST", attempt);
      if (result.outcome === "notAttempted") {
        sessionStorage.removeItem(storageKey);
        setUncertain(false);
        setChoice(null);
        setMessage("No reset was used. " + result.message);
        onRefresh();
        return;
      }
      if (result.outcome === "uncertain")
        throw Error(result.message || "Reset result is unknown.");
      const messages: Record<string, string> = {
        reset: "Reset used successfully.",
        alreadyRedeemed: "This reset attempt was already completed.",
        nothingToReset: "No eligible usage window needs resetting.",
        noCredit: "No reset credit is available.",
      };
      if (!messages[result.outcome])
        throw Error("Unknown result. Retry only this same attempt.");
      sessionStorage.removeItem(storageKey);
      setUncertain(false);
      setChoice(null);
      setMessage(
        messages[result.outcome] +
          (result.usageError ? " " + result.usageError : ""),
      );
      window.dispatchEvent(
        new CustomEvent("workbench:usage", { detail: { agent } }),
      );
      onRefresh();
    } catch (e) {
      setChoice(null);
      setUncertain(true);
      setMessage(
        (e instanceof Error ? e.message : String(e)) +
          " The outcome may be unknown. Refresh to check, or retry this same attempt.",
      );
    } finally {
      working.current = false;
      setBusy(false);
    }
  };
  if (agent === "claude")
    return (
      <section className="banked-resets">
        <h4>Banked resets</h4>
        <p>This CLI does not provide banked reset information.</p>
        <a
          href="https://claude.ai/settings/usage"
          target="_blank"
          rel="noreferrer"
        >
          Open Claude Usage
        </a>
        <small>Check the signed-in account on the official page.</small>
      </section>
    );
  return (
    <section className="banked-resets">
      <h4>Banked resets</h4>
      <p>
        {count == null
          ? "Reset information unavailable"
          : `${count} reset${count === 1 ? "" : "s"} available`}
      </p>
      {credits.map((c) => (
        <div className="banked-reset-row" key={c.id}>
          <strong>{c.title || "Reset"}</strong>
          <span>{c.description || ""}</span>
          <small>
            {c.expiresAt === null
              ? "Does not expire"
              : typeof c.expiresAt === "number"
                ? `Expires ${new Date(c.expiresAt * 1000).toLocaleString()}`
                : "Expiry unavailable"}{" "}
            · {c.status}
          </small>
          <small>
            {c.resetType === "codexRateLimits"
              ? "Full reset: applicable 5h and weekly windows"
              : "Unsupported reset type"}
          </small>
          <Button
            type="button"
            size="sm"
            variant="outline"
            disabled={busy || uncertain || !accountId || !resetUsable(c)}
            onClick={() => setChoice({ credit: c })}
          >
            Use reset
          </Button>
        </div>
      ))}
      {partial && (
        <>
          <p>
            Only some reset details were provided. The CLI can select the next
            available reset.
          </p>
          <Button
            type="button"
            size="sm"
            variant="outline"
            disabled={busy || uncertain || !accountId}
            onClick={() => setChoice({})}
          >
            Use next reset
          </Button>
        </>
      )}
      {count != null && count > 0 && !accountId && (
        <p>Refresh Usage to verify the CLI account before using a reset.</p>
      )}
      {message && <p role="status">{message}</p>}
      {uncertain && (
        <Button
          type="button"
          size="sm"
          variant="outline"
          disabled={busy}
          onClick={() => void consume()}
        >
          Retry same attempt
        </Button>
      )}
      <Dialog
        open={choice !== null}
        onOpenChange={(open) => {
          if (!open && !busy) setChoice(null);
        }}
      >
        <DialogContent>
          <DialogTitle>Use banked reset?</DialogTitle>
          <p>
            {choice?.credit?.title ||
              "The CLI will select the next available reset."}
          </p>
          <p>
            This consumes one reset and restores applicable Codex 5h and weekly
            limits. Your weekly reset schedule may change.
          </p>
          {choice?.credit?.expiresAt != null && (
            <p>
              Expires{" "}
              {new Date(choice.credit.expiresAt * 1000).toLocaleString()}
            </p>
          )}
          <Button type="button" disabled={busy} onClick={() => void consume()}>
            {busy ? "Using reset…" : "Confirm use reset"}
          </Button>
          <Button
            type="button"
            variant="ghost"
            disabled={busy}
            onClick={() => setChoice(null)}
          >
            Cancel
          </Button>
        </DialogContent>
      </Dialog>
    </section>
  );
}
