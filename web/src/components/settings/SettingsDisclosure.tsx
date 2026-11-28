import { useEffect, useState, type ReactNode } from "react";
import { ChevronRight } from "lucide-react";

const key = (id: string) => `workbench:settings-open:${id}`;

// Whether a disclosure was left open; browser storage may be unavailable
// (a private window), in which case it opens as it opens by default.
export function remembered(id: string): boolean | undefined {
  try {
    const v = localStorage.getItem(key(id));
    return v === null ? undefined : v === "1";
  } catch {
    return undefined;
  }
}

function remember(id: string, open: boolean) {
  try {
    localStorage.setItem(key(id), open ? "1" : "0");
  } catch {
    // Not remembered: it opens as by default next time.
  }
}

// A part of a settings tab that folds away: its title (and a note, such as
// a state, beside it) always shows; what is inside, only when open. Open
// or closed is remembered per viewer. `force` opens it whatever was
// remembered (something asked to see it, or needs the user).
export function SettingsDisclosure({
  id,
  title,
  note,
  defaultOpen = false,
  force = false,
  small = false,
  className = "",
  onOpenChange,
  children,
}: {
  id: string;
  title: ReactNode;
  note?: ReactNode;
  defaultOpen?: boolean;
  // Opens it; a number opens it again each time it changes.
  force?: boolean | number;
  onOpenChange?: (open: boolean) => void;
  small?: boolean;
  className?: string;
  children: ReactNode;
}) {
  const [open, setOpen] = useState(
    () => !!force || (remembered(id) ?? defaultOpen),
  );
  useEffect(() => {
    if (force) setOpen(true);
  }, [force]);
  // Opened by force at first: told as any opening is.
  useEffect(() => {
    if (open) onOpenChange?.(true);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);
  return (
    <details
      className={
        (small
          ? "settings-disclosure small "
          : "settings-section settings-disclosure ") + className
      }
      open={open}
      onToggle={(e) => {
        const now = e.currentTarget.open;
        if (now !== open) {
          setOpen(now);
          remember(id, now);
          onOpenChange?.(now);
        }
      }}
    >
      <summary>
        <ChevronRight size={14} aria-hidden />
        {small ? (
          <span className="settings-disclosure-title">{title}</span>
        ) : (
          <h3>{title}</h3>
        )}
        {note && (
          <span className="subtle settings-disclosure-note">{note}</span>
        )}
      </summary>
      {open && <div className="settings-disclosure-body">{children}</div>}
    </details>
  );
}
