import { useRef, useState } from "react";
import { Loader2, Upload, X } from "lucide-react";
import {
  AssistantIcon,
  DEFAULT_ICON,
  ICONS,
  iconURL,
} from "../components/AssistantIcon";
import { Button } from "../components/ui/button";
import { api } from "../lib/api";
import { assistantsChanged, type Assistant } from "../lib/assistants";
import { ICON_TYPES, squareIcon, uploadIcon } from "./iconUpload";

// Its icon in Settings (ADR 0030): how it looks, with its agent's badge;
// the built-in drawings to choose from (saved with the other settings);
// and an image to upload, which takes effect at once.
export function IconPicker({
  assistant: a,
  agent,
  icon,
  onPick,
}: {
  assistant: Assistant;
  // The agent chosen in the form, for the badge.
  agent: string;
  // The drawing chosen in the form, or "file" for the uploaded image.
  icon: string;
  onPick: (icon: string) => void;
}) {
  const input = useRef<HTMLInputElement>(null);
  const [version, setVersion] = useState(a.iconVersion ?? 0);
  const [hasFile, setHasFile] = useState(a.icon === "file");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const upload = async (file?: File) => {
    if (!file) return;
    setBusy(true);
    setError("");
    try {
      const v = await uploadIcon<Assistant>(a.dir, await squareIcon(file));
      setVersion(v.iconVersion ?? Date.now());
      setHasFile(true);
      onPick("file");
      assistantsChanged();
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
      if (input.current) input.current.value = "";
    }
  };
  const remove = async () => {
    setBusy(true);
    setError("");
    try {
      await api(`assistants/${encodeURIComponent(a.dir)}/icon`, "DELETE");
      setHasFile(false);
      onPick(DEFAULT_ICON);
      assistantsChanged();
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  };
  const shown =
    icon === "file" && hasFile
      ? { src: iconURL(a.dir, version) }
      : { icon: icon === "file" ? DEFAULT_ICON : icon };
  return (
    <div className="routine-row icon-row">
      <span className="routine-label">Icon</span>
      <div className="icon-picker">
        <AssistantIcon {...shown} agent={agent} size={64} name={a.name} />
        <div className="icon-choices">
          <div className="icon-grid" role="radiogroup" aria-label="Icon">
            {ICONS.map((i) => (
              <button
                type="button"
                role="radio"
                key={i.name}
                aria-checked={icon === i.name}
                aria-label={i.label}
                title={i.label}
                className={icon === i.name ? "on" : ""}
                onClick={() => onPick(i.name)}
              >
                <AssistantIcon
                  icon={i.name}
                  agent={agent}
                  size={32}
                  decorative
                  badge={false}
                />
              </button>
            ))}
          </div>
          <div className="icon-actions">
            <input
              ref={input}
              type="file"
              accept={ICON_TYPES}
              hidden
              onChange={(e) => void upload(e.target.files?.[0])}
            />
            <Button
              type="button"
              variant="outline"
              size="sm"
              disabled={busy}
              onClick={() => input.current?.click()}
            >
              {busy ? (
                <Loader2 size={14} className="spin" />
              ) : (
                <Upload size={14} />
              )}
              Upload image…
            </Button>
            {hasFile && (
              <>
                {icon !== "file" && (
                  <Button
                    type="button"
                    variant="ghost"
                    size="sm"
                    disabled={busy}
                    onClick={() => onPick("file")}
                  >
                    Use the uploaded image
                  </Button>
                )}
                <Button
                  type="button"
                  variant="ghost"
                  size="sm"
                  disabled={busy}
                  onClick={() => void remove()}
                >
                  <X size={14} />
                  Remove upload
                </Button>
              </>
            )}
            <span className="subtle">
              Square, shown at {ICON_SIDE_TEXT}; a larger picture is cut to its
              middle.
            </span>
          </div>
          {error && <p className="error-text">{error}</p>}
        </div>
      </div>
    </div>
  );
}

const ICON_SIDE_TEXT = "16, 32 and 64 pixels";
