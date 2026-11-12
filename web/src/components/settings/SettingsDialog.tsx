import { useState } from "react";
import { X } from "lucide-react";
import { Dialog, DialogContent, DialogTitle } from "../ui/dialog";
import { AccessTab } from "./AccessTab";
import { AboutTab } from "./AboutTab";
import { GeneralTab } from "./GeneralTab";

export type SettingsTab = "general" | "access" | "about";
export const TABS: { id: SettingsTab; label: string }[] = [
  { id: "general", label: "General" },
  { id: "access", label: "Access & security" },
  { id: "about", label: "About" },
];
const KEY = "workbench:settings-tab";

function storedTab(): SettingsTab {
  try {
    const v = localStorage.getItem(KEY);
    if (TABS.some((t) => t.id === v)) return v as SettingsTab;
  } catch {
    // Private windows and blocked storage: the first tab.
  }
  return "general";
}

// The tab bar: arrow keys move between tabs, as in a tablist.
export function SettingsTabs({
  tab,
  dirty,
  onSelect,
}: {
  tab: SettingsTab;
  dirty: boolean;
  onSelect: (t: SettingsTab) => void;
}) {
  return (
    <div
      role="tablist"
      aria-label="Settings"
      className="settings-tabs"
      onKeyDown={(e) => {
        if (e.key !== "ArrowRight" && e.key !== "ArrowLeft") return;
        e.preventDefault();
        const i = TABS.findIndex((t) => t.id === tab);
        const next =
          TABS[
            (i + (e.key === "ArrowRight" ? 1 : TABS.length - 1)) % TABS.length
          ];
        onSelect(next.id);
        document.getElementById(`settings-tab-${next.id}`)?.focus();
      }}
    >
      {TABS.map((t) => (
        <button
          key={t.id}
          type="button"
          role="tab"
          id={`settings-tab-${t.id}`}
          aria-selected={tab === t.id}
          aria-controls={`settings-panel-${t.id}`}
          tabIndex={tab === t.id ? 0 : -1}
          onClick={() => onSelect(t.id)}
        >
          {t.label}
          {t.id === "access" && dirty && (
            <span
              className="tab-dot"
              title="Not saved"
              aria-label="(not saved)"
            />
          )}
        </button>
      ))}
    </div>
  );
}

// Settings in three tabs: General (applies at once), Access & security
// (saved with its button), About (read only). Unsaved access changes stay
// across tabs and are confirmed before closing.
export function SettingsDialog({
  open,
  onOpenChange,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const [tab, setTab] = useState<SettingsTab>(storedTab);
  const [dirty, setDirty] = useState(false);
  const [busy, setBusy] = useState(false);
  const select = (t: SettingsTab) => {
    setTab(t);
    try {
      localStorage.setItem(KEY, t);
    } catch {
      // Remembered for this page view only.
    }
  };
  const close = () => {
    if (busy) return;
    if (dirty && !confirm("Discard the access settings you haven't saved?"))
      return;
    onOpenChange(false);
  };
  const panel = (id: SettingsTab) => ({
    role: "tabpanel" as const,
    id: `settings-panel-${id}`,
    "aria-labelledby": `settings-tab-${id}`,
    hidden: tab !== id,
    className: "settings-panel",
  });
  return (
    <Dialog
      open={open}
      onOpenChange={(next) => (next ? onOpenChange(true) : close())}
    >
      <DialogContent className="modal modal-content settings-modal">
        <DialogTitle asChild>
          <h2>Settings</h2>
        </DialogTitle>
        <button
          className="modal-close"
          aria-label="Close settings"
          disabled={busy}
          onClick={close}
        >
          <X size={18} />
        </button>
        <SettingsTabs tab={tab} dirty={dirty} onSelect={select} />
        <div {...panel("general")}>
          <GeneralTab />
        </div>
        <div {...panel("access")}>
          <AccessTab open={open} onDirty={setDirty} onBusy={setBusy} />
        </div>
        <div {...panel("about")}>
          <AboutTab active={open && tab === "about"} />
        </div>
      </DialogContent>
    </Dialog>
  );
}
