import { useEffect, useLayoutEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import type { Item } from "./commands";

const TIP_ID = "command-tip";
// How long a row stays selected (by keys or the mouse) before its tooltip.
const TIP_DELAY = 500;

// The "/" command and "$" skill list above the composer.
export function CommandMenu({
  kind,
  items,
  active,
  loading,
  onPick,
  onHover,
}: {
  kind: "/" | "$";
  items: Item[];
  active: number;
  loading: boolean;
  onPick: (item: Item) => void;
  onHover: (index: number) => void;
}) {
  const list = useRef<HTMLDivElement>(null);
  // The selected row, once it has stayed selected, if its text is cut.
  const [tip, setTip] = useState<{ item: Item; row: DOMRect } | null>(null);
  // Keyed by name: the list is rebuilt on every composer render.
  const selected = items[active]?.name;
  useEffect(() => {
    setTip(null);
    const row = list.current?.querySelector<HTMLElement>(
      '[aria-selected="true"]',
    );
    row?.scrollIntoView({ block: "nearest" });
    const item = items.find((it) => it.name === selected);
    if (!row || !item) return;
    const t = window.setTimeout(() => {
      if (cut(row)) setTip({ item, row: row.getBoundingClientRect() });
    }, TIP_DELAY);
    return () => window.clearTimeout(t);
  }, [active, selected]);
  return (
    <div
      ref={list}
      className="command-menu"
      role="listbox"
      aria-label={kind === "/" ? "Commands" : "Skills"}
      // The tooltip is placed for where the row was.
      onScroll={() => setTip(null)}
      onMouseLeave={() => setTip(null)}
    >
      {!items.length && (
        <div className="command-empty">
          {loading
            ? "Loading…"
            : kind === "/"
              ? "No matching command"
              : "No matching skill"}
        </div>
      )}
      {items.map((it, i) => (
        <button
          type="button"
          role="option"
          aria-selected={i === active}
          aria-describedby={tip?.item.name === it.name ? TIP_ID : undefined}
          key={it.name}
          className={"command-item" + (i === active ? " active" : "")}
          // Keep focus (and the caret) in the composer.
          onMouseDown={(e) => e.preventDefault()}
          onMouseEnter={() => onHover(i)}
          onClick={() => onPick(it)}
        >
          <span className="command-name">
            {kind}
            {it.name}
          </span>
          {/* Always present, so the columns line up across rows. */}
          <span className="command-hint">{it.hint}</span>
          <span className="command-desc">{it.description}</span>
          {it.badge !== "command" ? (
            // Namespaced: a bare "workbench" class is the app layout.
            <span className={"command-badge command-badge-" + it.badge}>
              {it.badge}
            </span>
          ) : (
            <span />
          )}
        </button>
      ))}
      {tip && createPortal(<Tip kind={kind} {...tip} />, document.body)}
    </div>
  );
}

// Whether any of the row's text is cut short or hidden (the hint on phones).
function cut(row: HTMLElement) {
  return [...row.querySelectorAll<HTMLElement>("span")].some(
    (el) =>
      el.textContent &&
      (el.scrollWidth > el.clientWidth || !el.getClientRects().length),
  );
}

// The full name, hint and description of a row, below it (above it when
// there is no room), as wide as the menu at most. Fixed in the page so the
// menu's scrolling does not clip it.
function Tip({ kind, item, row }: { kind: string; item: Item; row: DOMRect }) {
  const ref = useRef<HTMLDivElement>(null);
  const [top, setTop] = useState<number | null>(null);
  useLayoutEffect(() => {
    const h = ref.current?.offsetHeight ?? 0;
    const below = row.bottom + 4;
    setTop(
      below + h <= window.innerHeight - 8
        ? below
        : Math.max(8, row.top - 4 - h),
    );
  }, [row]);
  return (
    <div
      ref={ref}
      id={TIP_ID}
      role="tooltip"
      className="command-tip"
      style={{
        top: top ?? 0,
        left: row.left + 8,
        maxWidth: Math.min(640, row.width - 16),
        visibility: top === null ? "hidden" : undefined,
      }}
    >
      <div className="command-tip-head">
        <span>
          {kind}
          {item.name}
        </span>
        {item.hint && <span className="command-tip-hint">{item.hint}</span>}
      </div>
      {item.description && (
        <div className="command-tip-desc">{item.description}</div>
      )}
    </div>
  );
}
