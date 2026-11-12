import { useState } from "react";
import { ClipboardList } from "lucide-react";
import { Choices } from "../approval/Choices";
import { useEnv } from "./media";

// The choices Codex's own client offers when a plan-mode turn ends with a
// plan: implement it here (leaving plan mode), implement it in a new session
// that starts from the plan alone, or keep planning.
export function PlanPrompt({ plan }: { plan: string }) {
  const env = useEnv();
  const [selected, setSelected] = useState(0);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [dismissed, setDismissed] = useState(false);
  if (dismissed || !env.implementPlan || !env.implementPlanFresh) return null;
  const { implementPlan, implementPlanFresh } = env;
  const dismiss = () => {
    setDismissed(true);
    env.dismissPlan?.();
  };
  const options = [
    {
      label: "Yes, implement this plan",
      hint: "switch to Default and start coding",
    },
    {
      label: "Yes, clear context and implement",
      hint: "new session with this plan",
    },
    { label: "No, stay in Plan mode", hint: "continue planning with Codex" },
  ];
  const choose = async (i: number) => {
    setSelected(i);
    if (busy) return;
    if (i === 2) return dismiss();
    setBusy(true);
    setError("");
    try {
      if (i === 0) await implementPlan();
      else {
        await implementPlanFresh(plan);
        dismiss();
      }
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  };
  const onKey = (e: React.KeyboardEvent) => {
    if (e.key === "ArrowDown" || e.key === "ArrowUp") {
      e.preventDefault();
      setSelected((s) => (s + (e.key === "ArrowDown" ? 1 : 2)) % 3);
    } else if (/^[1-3]$/.test(e.key)) {
      e.preventDefault();
      void choose(Number(e.key) - 1);
    } else if (e.key === "Enter") {
      e.preventDefault();
      void choose(selected);
    } else if (e.key === "Escape") {
      e.preventDefault();
      dismiss();
    }
  };
  return (
    <div
      className="approval plan-prompt"
      tabIndex={-1}
      onKeyDown={onKey}
      role="group"
      aria-label="Implement this plan?"
    >
      <div className="approval-title">
        <ClipboardList size={15} />
        <strong>Implement this plan?</strong>
        {busy && <span className="ask-status">Starting…</span>}
      </div>
      <Choices
        options={options}
        selected={selected}
        onChoose={(i) => void choose(i)}
      />
      <p className="approval-keys">
        ↑↓ select · 1–3 choose · Enter confirm · Esc keep planning
      </p>
      {error && (
        <p role="alert" className="error-text">
          {error}
        </p>
      )}
    </div>
  );
}
