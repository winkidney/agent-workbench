import { useState } from "react";
import { Check, Copy } from "lucide-react";
import { copyText } from "../lib/copy";
import { ManualCopyDialog } from "../components/ManualCopyDialog";

export function CopyButton({
  text,
  label = "Copy",
}: {
  text: string;
  label?: string;
}) {
  const [done, setDone] = useState(false);
  const [manual, setManual] = useState<string | null>(null);
  return (
    <>
      <button
        type="button"
        className="copy-button"
        aria-label={label}
        title={label}
        onClick={() =>
          void copyText(text).then((copied) => {
            if (!copied) return setManual(text);
            setDone(true);
            setTimeout(() => setDone(false), 1200);
          })
        }
      >
        {done ? <Check size={12} /> : <Copy size={12} />}
      </button>
      <ManualCopyDialog text={manual} onClose={() => setManual(null)} />
    </>
  );
}
