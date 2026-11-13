import {
  useEffect,
  useLayoutEffect,
  useRef,
  useState,
  type ReactNode,
} from "react";
import { Button } from "../components/ui/button";
import { sizeText } from "../composer/AttachmentStrip";
import {
  Conflict,
  asLines,
  eolOf,
  indentUnit,
  lineCol,
  loadText,
  saveText,
  shiftLines,
  stampBytes,
  toDisk,
} from "./text";
import { markUnsaved, mayLeave } from "./unsaved";

const WRAP_KEY = "workbench-editor-wrap";
const SAVE_KEYS = /Mac|iPhone|iPad/.test(navigator.userAgent) ? "⌘S" : "Ctrl+S";

function storedWrap(): boolean {
  try {
    return localStorage.getItem(WRAP_KEY) !== "off";
  } catch {
    return true;
  }
}

type Meta = {
  stamp: string;
  eol: "\n" | "\r\n";
  unit: string;
  // Bumped when the text is loaded, to start the text box afresh.
  version: number;
};

// Replaces a range the way typing would, so the browser's own undo covers
// it.
function replace(
  ta: HTMLTextAreaElement,
  from: number,
  to: number,
  text: string,
) {
  ta.setSelectionRange(from, to);
  if (!document.execCommand(text ? "insertText" : "delete", false, text))
    ta.setRangeText(text, from, to, "end");
}

// A plain text box for a text file the preview shows. Its text lives only
// here: closing the editor lets it go.
export function TextEditor({
  session,
  path,
  controls,
  onDone,
  onSaved,
  onError,
}: {
  session: string;
  // Relative to the session's directory.
  path: string;
  // The preview's own buttons kept while editing (expand, close).
  controls?: ReactNode;
  onDone: () => void;
  onSaved: (stamp: string) => void;
  onError: (e: unknown) => void;
}) {
  const name = path.split("/").pop() || path;
  const area = useRef<HTMLTextAreaElement>(null);
  // The text as last loaded or saved, with \n line ends.
  const original = useRef("");
  const [meta, setMeta] = useState<Meta | null>(null);
  const [reloads, setReloads] = useState(0);
  const [dirty, setDirty] = useState(false);
  const [saved, setSaved] = useState(false);
  const saving = useRef(false);
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<
    { conflict: true } | { conflict: false; message: string } | null
  >(null);
  const [pos, setPos] = useState({ line: 1, col: 1 });
  const [wrap, setWrap] = useState(storedWrap);
  const frame = useRef(0);

  useEffect(() => {
    let live = true;
    setMeta(null);
    loadText(session, path)
      .then(({ text, stamp }) => {
        if (!live) return;
        original.current = asLines(text);
        setDirty(false);
        setPos({ line: 1, col: 1 });
        setMeta({
          stamp,
          eol: eolOf(text),
          unit: indentUnit(original.current),
          version: reloads + 1,
        });
      })
      .catch((e) => {
        if (!live) return;
        onError(e);
        onDone();
      });
    return () => {
      live = false;
    };
  }, [session, path, reloads]);

  // Loaded: the text in the box (set here, not as a prop React would write
  // into the page again on every render), the caret at the start.
  useLayoutEffect(() => {
    const ta = area.current;
    if (!meta || !ta) return;
    ta.value = original.current;
    ta.focus({ preventScroll: true });
    ta.setSelectionRange(0, 0);
    ta.scrollTop = 0;
  }, [meta?.version]);

  useEffect(() => markUnsaved(dirty ? name : null), [dirty, name]);
  useEffect(
    () => () => {
      markUnsaved(null);
      cancelAnimationFrame(frame.current);
    },
    [],
  );

  const track = () => {
    if (frame.current) return;
    frame.current = requestAnimationFrame(() => {
      frame.current = 0;
      const ta = area.current;
      if (ta) setPos(lineCol(ta.value, ta.selectionStart));
    });
  };
  // Unsaved: differs from the text as saved (an edit undone is not one).
  const changed = () => {
    const ta = area.current;
    if (!ta) return;
    const o = original.current;
    const d = ta.textLength !== o.length || ta.value !== o;
    setDirty(d);
    if (d) setSaved(false);
    track();
  };

  const save = async (overwrite = false) => {
    const ta = area.current;
    if (!ta || !meta || saving.current) return;
    if (!overwrite && ta.value === original.current) return;
    const value = ta.value;
    saving.current = true;
    setBusy(true);
    setProblem(null);
    try {
      const stamp = await saveText(
        session,
        path,
        toDisk(value, meta.eol),
        overwrite ? undefined : meta.stamp,
      );
      original.current = value;
      setMeta((m) => m && { ...m, stamp });
      // Typed while it saved: still unsaved.
      setDirty(area.current ? area.current.value !== value : false);
      setSaved(true);
      onSaved(stamp);
    } catch (e) {
      setProblem(
        e instanceof Conflict
          ? { conflict: true }
          : {
              conflict: false,
              message: e instanceof Error ? e.message : String(e),
            },
      );
    } finally {
      saving.current = false;
      setBusy(false);
    }
  };

  const done = () => {
    if (dirty && !mayLeave()) return;
    onDone();
  };

  const onKeyDown = (e: React.KeyboardEvent) => {
    if (
      (e.ctrlKey || e.metaKey) &&
      !e.altKey &&
      !e.shiftKey &&
      e.key.toLowerCase() === "s"
    ) {
      e.preventDefault();
      void save();
      return;
    }
    const ta = area.current;
    if (!ta || !meta || e.target !== ta) return;
    // Typing with an input method: its keys are its own.
    if (e.nativeEvent.isComposing || e.keyCode === 229) return;
    const plain = !e.ctrlKey && !e.metaKey && !e.altKey;
    const { selectionStart: start, selectionEnd: end, value } = ta;
    if (e.key === "Escape") {
      // Not the conversation's Esc (stop the turn), nor the expanded
      // view's: here it lets go of the text box, so Tab moves on.
      e.preventDefault();
      ta.blur();
    } else if (e.key === "Tab" && plain) {
      e.preventDefault();
      const nl = value.indexOf("\n", start);
      if (e.shiftKey || (nl >= 0 && nl < end)) {
        const r = shiftLines(value, start, end, meta.unit, e.shiftKey);
        if (r.text !== value.slice(r.from, r.to))
          replace(ta, r.from, r.to, r.text);
        ta.setSelectionRange(r.start, r.end);
      } else replace(ta, start, end, meta.unit);
      changed();
    } else if (e.key === "Enter" && plain && !e.shiftKey) {
      // A new line keeps the indentation of the one it breaks.
      const from = value.lastIndexOf("\n", start - 1) + 1;
      const lead = /^[ \t]*/.exec(value.slice(from, start))![0];
      if (!lead) return;
      e.preventDefault();
      replace(ta, start, end, "\n" + lead);
      changed();
    }
  };

  const pickWrap = (on: boolean) => {
    setWrap(on);
    try {
      localStorage.setItem(WRAP_KEY, on ? "on" : "off");
    } catch {
      // For this view only.
    }
  };

  return (
    <div className="text-editor" onKeyDown={onKeyDown}>
      <div className="preview-toolbar">
        <span title={path}>
          {dirty && (
            <b className="text-editor-dot" title="Unsaved changes">
              ●{" "}
            </b>
          )}
          {path}
        </span>
        <Button
          size="sm"
          variant="outline"
          className="text-editor-action"
          onClick={done}
        >
          Done
        </Button>
        <Button
          size="sm"
          className="text-editor-action"
          disabled={!dirty || busy}
          title={`Save (${SAVE_KEYS})`}
          onClick={() => void save()}
        >
          {busy ? "Saving…" : "Save"}
        </Button>
        {controls}
      </div>
      {problem && (
        <div className="text-editor-bar" role="alert">
          {problem.conflict ? (
            <>
              <span>
                {name} changed on disk after you opened it (the agent may have
                edited it).
              </span>
              <Button
                size="sm"
                variant="outline"
                disabled={busy}
                onClick={() => void save(true)}
              >
                Overwrite
              </Button>
              <Button
                size="sm"
                variant="outline"
                title="Discards your changes"
                onClick={() => {
                  setProblem(null);
                  setReloads((n) => n + 1);
                }}
              >
                Load the disk version
              </Button>
            </>
          ) : (
            <>
              <span>Not saved: {problem.message}</span>
              <Button
                size="sm"
                variant="outline"
                onClick={() => setProblem(null)}
              >
                OK
              </Button>
            </>
          )}
        </div>
      )}
      {meta ? (
        <textarea
          ref={area}
          className={wrap ? "wrap" : undefined}
          aria-label={`Text of ${name}`}
          wrap={wrap ? "soft" : "off"}
          spellCheck={false}
          autoCapitalize="off"
          autoCorrect="off"
          autoComplete="off"
          translate="no"
          onInput={changed}
          onSelect={track}
        />
      ) : (
        <p className="preview-note">Loading…</p>
      )}
      <div className="text-editor-status">
        <span>
          Ln {pos.line}, Col {pos.col}
        </span>
        {saved && !dirty && <span>Saved</span>}
        <span className="text-editor-meta">
          {meta
            ? `${meta.eol === "\r\n" ? "CRLF" : "LF"} · ${sizeText(stampBytes(meta.stamp))}`
            : ""}
        </span>
        <label>
          <input
            type="checkbox"
            checked={wrap}
            onChange={(e) => pickWrap(e.target.checked)}
          />
          Wrap
        </label>
      </div>
    </div>
  );
}
