import { useState } from "react";
import { Loader2, RefreshCw } from "lucide-react";
import { api } from "../lib/api";
import {
  assistantsChanged,
  UPDATE_RESULT,
  type Assistant,
  type CatalogUpdate,
} from "../lib/assistants";
import { Button } from "../components/ui/button";

// An installed assistant's update from its catalog (ADR 0031): first what
// it would do, file by file (what runs stands out); a conflict is the
// user's to choose, or to merge with Moss; then Update.
export function CatalogUpdatePanel({ assistant: a }: { assistant: Assistant }) {
  const [plan, setPlan] = useState<CatalogUpdate | null>(
    a.catalog?.update ?? null,
  );
  const [choices, setChoices] = useState<Record<string, "mine" | "theirs">>({});
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [note, setNote] = useState("");
  const run = async (apply: boolean) => {
    setBusy(true);
    setError("");
    setNote("");
    try {
      const v = await api<CatalogUpdate>(
        `assistants/${encodeURIComponent(a.dir)}/update`,
        "POST",
        { apply, choices },
      );
      setPlan(v);
      if (!v.commit || !v.files?.length)
        setNote("Up to date with its catalog.");
      if (v.applied) {
        setNote(`Updated to ${v.commit?.slice(0, 7)}.`);
        setPlan(null);
        assistantsChanged();
      }
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  };
  const askMoss = async (f: NonNullable<CatalogUpdate["files"]>[number]) => {
    const text = `Merge the catalog's change into ${a.path}/${f.rel}, keeping my own changes there. The catalog's version it was based on: ${f.base ?? "(none: new there)"}; its new version: ${f.theirs}. Show me the merged file and ask before writing it; then I will update ${a.dir} keeping mine.`;
    try {
      await api("assistants/main/message", "POST", { text });
      setNote("Asked Moss: its conversation shows the merge.");
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    }
  };
  if (!a.catalog) return null;
  const files = plan?.commit ? (plan.files ?? []) : [];
  const runs = files.filter((f) => f.runs);
  return (
    <div className="catalog-update">
      <div className="catalog-bar">
        <Button
          type="button"
          variant="outline"
          size="sm"
          disabled={busy}
          onClick={() => void run(false)}
        >
          {busy ? (
            <Loader2 size={14} className="spin" />
          ) : (
            <RefreshCw size={14} />
          )}
          {files.length
            ? `Update available (${files.length})`
            : "Check for updates"}
        </Button>
        {note && <span className="subtle">{note}</span>}
      </div>
      {files.length > 0 && (
        <>
          <p className="subtle">
            From <code>{plan!.from.slice(0, 7)}</code> to{" "}
            <code>{plan!.commit!.slice(0, 7)}</code>. Your settings, data and
            changes stay yours.
          </p>
          {runs.length > 0 && (
            <p className="catalog-warn">
              What runs on this computer changes:{" "}
              {runs.map((f) => f.rel).join(", ")}. Read it before updating.
            </p>
          )}
          <ul className="catalog-plan">
            {files.map((f) => (
              <li key={f.rel}>
                <code>{f.rel}</code>: {UPDATE_RESULT[f.result] ?? f.result}
                {f.runs && <strong className="catalog-runs"> runs</strong>}
                {f.result === "conflict" && (
                  <span
                    className="catalog-choice"
                    role="radiogroup"
                    aria-label={f.rel}
                  >
                    {(["mine", "theirs"] as const).map((c) => (
                      <label key={c}>
                        <input
                          type="radio"
                          name={`choice-${f.rel}`}
                          checked={(choices[f.rel] ?? "mine") === c}
                          onChange={() =>
                            setChoices({ ...choices, [f.rel]: c })
                          }
                        />
                        {c === "mine" ? "Keep mine" : "Use the catalog's"}
                      </label>
                    ))}
                    <Button
                      type="button"
                      variant="ghost"
                      size="sm"
                      onClick={() => void askMoss(f)}
                    >
                      Ask Moss to merge
                    </Button>
                  </span>
                )}
              </li>
            ))}
          </ul>
          <div className="confirm-actions">
            <Button
              type="button"
              size="sm"
              disabled={busy}
              onClick={() => void run(true)}
            >
              {busy && <Loader2 size={14} className="spin" />}
              Update
            </Button>
          </div>
        </>
      )}
      {error && <p className="error-text">{error}</p>}
    </div>
  );
}
