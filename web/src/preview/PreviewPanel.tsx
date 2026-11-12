import {
  useEffect,
  useLayoutEffect,
  useRef,
  useState,
  type ReactNode,
} from "react";
import {
  ExternalLink,
  FileText,
  Folder,
  Globe,
  Maximize,
  Maximize2,
  Minimize2,
  RefreshCw,
  X,
} from "lucide-react";
import { api } from "../lib/api";
import {
  canonicalAddress,
  fileFrameSandbox,
  previewMode,
  type PreviewMode,
} from "../lib/preview";
import { Button } from "../components/ui/button";
import {
  VIEWPORTS,
  scaleFor,
  tabLabel,
  tabTitle,
  type Preview,
  type PreviewTab,
  type ViewportId,
} from "./tabs";
import type { PreviewTabsApi } from "./usePreviewTabs";

const VIEWPORT_KEY = "workbench-preview-viewport";

function storedViewport(): ViewportId {
  try {
    const v = localStorage.getItem(VIEWPORT_KEY);
    if (VIEWPORTS.some((x) => x.id === v)) return v as ViewportId;
  } catch {
    // Blocked storage: Fit.
  }
  return "fit";
}

// The preview panel's tabs: the file list, then what the session opened.
// Only the tab shown has a page; the panel hidden, it has none.
export function PreviewPanel({
  session,
  tabs,
  files,
  expanded,
  onExpand,
  onError,
}: {
  session: string;
  tabs: PreviewTabsApi;
  files: ReactNode;
  expanded: boolean;
  onExpand: (expanded: boolean) => void;
  onError: (e: unknown) => void;
}) {
  const { state } = tabs;
  const active = state.tabs[state.active];
  return (
    <>
      <div className="preview-tabs" role="tablist" aria-label="Previews">
        <button
          role="tab"
          aria-selected={state.active === -1}
          className="preview-tab files-tab"
          onClick={() => tabs.show(-1)}
        >
          <Folder size={13} aria-hidden />
          <span>Files</span>
        </button>
        {state.tabs.map((t, i) => (
          <div
            key={`${t.kind}:${t.path ?? t.command ?? t.target}`}
            className={
              "preview-tab" +
              (i === state.active ? " active" : "") +
              (t.missing ? " missing" : "")
            }
            role="presentation"
          >
            <button
              role="tab"
              aria-selected={i === state.active}
              title={tabLabel(t)}
              onClick={() => tabs.show(i)}
            >
              {t.kind === "file" ? (
                <FileText size={13} aria-hidden />
              ) : (
                <Globe size={13} aria-hidden />
              )}
              <span>{tabTitle(t)}</span>
            </button>
            <button
              className="preview-tab-close"
              aria-label={`Close ${tabTitle(t)}`}
              onClick={() => void closeTab(tabs, i, onError)}
            >
              <X size={12} />
            </button>
          </div>
        ))}
      </div>
      {active ? (
        <TabView
          key={`${session}:${state.active}:${tabLabel(active)}`}
          session={session}
          tab={active}
          onUpdate={(patch) => tabs.update(state.active, patch)}
          onClose={() => void closeTab(tabs, state.active, onError)}
          expanded={expanded}
          onExpand={onExpand}
          onError={onError}
        />
      ) : (
        files
      )}
    </>
  );
}

// Closing a tab of a service the workbench started asks whether to stop it.
async function closeTab(
  tabs: PreviewTabsApi,
  i: number,
  onError: (e: unknown) => void,
) {
  const t = tabs.state.tabs[i];
  if (t?.command && t.preview && t.running) {
    if (confirm(`Stop ${t.command} too? Cancel keeps it running.`))
      await api("previews/" + t.preview, "DELETE").catch(onError);
  }
  tabs.close(i);
}

type View =
  | { kind: "loading" }
  | { kind: "page"; preview: Preview; grant: string; mode: PreviewMode }
  | { kind: "stopped" }
  | { kind: "error"; message: string };

function TabView({
  session,
  tab,
  onUpdate,
  onClose,
  expanded,
  onExpand,
  onError,
}: {
  session: string;
  tab: PreviewTab;
  onUpdate: (patch: Partial<PreviewTab>) => void;
  onClose: () => void;
  expanded: boolean;
  onExpand: (expanded: boolean) => void;
  onError: (e: unknown) => void;
}) {
  const [view, setView] = useState<View>({ kind: "loading" });
  // Bumped to load the page again.
  const [load, setLoad] = useState(0);
  // The file as it was when shown, to tell when it changed.
  const [shownStamp, setShownStamp] = useState<string | undefined>();
  const [viewport, setViewport] = useState<ViewportId>(storedViewport);
  const stage = useRef<HTMLDivElement>(null);
  const root = useRef<HTMLDivElement>(null);
  const [size, setSize] = useState({ w: 0, h: 0 });
  const frameName = `preview-frame-${load}`;

  useEffect(() => {
    let live = true;
    setView({ kind: "loading" });
    const page = (r: { preview: Preview; grant: string }) => {
      if (!live) return;
      setShownStamp(tab.stamp);
      setView({
        kind: "page",
        ...r,
        mode: previewMode(r.preview, location.host),
      });
    };
    const fail = (e: unknown) =>
      live &&
      setView({
        kind: "error",
        message: e instanceof Error ? e.message : String(e),
      });
    type Opened = { preview: Preview; grant: string };
    if (tab.kind === "file") {
      api<Opened>("previews", "POST", { session, kind: "file", path: tab.path })
        .then(page)
        .catch(fail);
    } else {
      // A running preview opens; an attached server is attached again; a
      // service the workbench started waits to be started again.
      const attach = () =>
        tab.command
          ? Promise.reject(new Error("stopped"))
          : api<Opened>("previews", "POST", {
              session,
              kind: "service",
              target: tab.target,
            }).then((r) => {
              onUpdate({ preview: r.preview.id, running: true });
              return r;
            });
      (tab.preview
        ? api<Opened>(`previews/${tab.preview}/authorize`, "POST", {}).catch(
            attach,
          )
        : attach()
      )
        .then(page)
        .catch((e) =>
          tab.command && String(e).includes("stopped")
            ? live && setView({ kind: "stopped" })
            : fail(e),
        );
    }
    return () => {
      live = false;
    };
  }, [session, load]);

  // A page on its preview origin is signed in by posting its grant into
  // the frame.
  useEffect(() => {
    if (view.kind !== "page" || view.mode !== "origin") return;
    submitGrant(view.preview, view.grant, frameName);
  }, [view]);

  useLayoutEffect(() => {
    const el = stage.current;
    if (!el) return;
    const measure = () => setSize({ w: el.clientWidth, h: el.clientHeight });
    measure();
    const o = new ResizeObserver(measure);
    o.observe(el);
    return () => o.disconnect();
  }, [view.kind]);

  // Esc leaves the expanded view (full screen handles its own).
  useEffect(() => {
    if (!expanded) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape" && !document.fullscreenElement) onExpand(false);
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [expanded]);

  const pickViewport = (v: ViewportId) => {
    setViewport(v);
    try {
      localStorage.setItem(VIEWPORT_KEY, v);
    } catch {
      // For this view only.
    }
  };
  const width = VIEWPORTS.find((v) => v.id === viewport)!.width;
  const scale = scaleFor(size.w, width);
  const changed =
    tab.kind === "file" &&
    !!shownStamp &&
    !!tab.stamp &&
    tab.stamp !== shownStamp;
  const page = view.kind === "page" ? view : null;
  const openTab = () => {
    if (!page) return;
    if (page.preview.fileUrl) {
      window.open(page.preview.fileUrl, "_blank", "noopener");
      return;
    }
    // A service on its preview origin: a new tab signed in with a grant of
    // its own (opened first, so the browser lets it open).
    const name = `preview-${page.preview.id}-${Date.now()}`;
    const w = window.open("about:blank", name);
    if (!w) return;
    api<{ preview: Preview; grant: string }>(
      `previews/${page.preview.id}/authorize`,
      "POST",
      {},
    )
      .then((r) => submitGrant(r.preview, r.grant, name))
      .catch((e) => {
        w.close();
        onError(e);
      });
  };
  const canNewTab =
    !!page &&
    (!!page.preview.fileUrl || page.mode === "origin") &&
    page.mode !== "unavailable";

  return (
    <div className="preview-view" ref={root}>
      <div className="preview-toolbar">
        <span title={tabLabel(tab)}>{tabLabel(tab)}</span>
        {changed && (
          <button
            className="preview-updated"
            onClick={() => setLoad((n) => n + 1)}
          >
            Updated · Reload
          </button>
        )}
        <select
          aria-label="Page width"
          value={viewport}
          onChange={(e) => pickViewport(e.target.value as ViewportId)}
        >
          {VIEWPORTS.map((v) => (
            <option key={v.id} value={v.id}>
              {v.width
                ? `${v.label} ${v.width}${v.id === viewport && scale < 1 ? ` · ${Math.round(scale * 100)}%` : ""}`
                : v.label}
            </option>
          ))}
        </select>
        <Button
          variant="ghost"
          size="icon"
          aria-label="Reload"
          title="Reload"
          onClick={() => setLoad((n) => n + 1)}
        >
          <RefreshCw size={14} />
        </Button>
        <Button
          variant="ghost"
          size="icon"
          aria-label={expanded ? "Back to the conversation" : "Expand"}
          title={
            expanded
              ? "Back to the conversation (Esc)"
              : "Expand over the conversation"
          }
          onClick={() => onExpand(!expanded)}
        >
          {expanded ? <Minimize2 size={14} /> : <Maximize2 size={14} />}
        </Button>
        {document.fullscreenEnabled && (
          <Button
            variant="ghost"
            size="icon"
            aria-label="Full screen"
            title="Full screen"
            onClick={() =>
              void root.current?.requestFullscreen().catch(() => {})
            }
          >
            <Maximize size={14} />
          </Button>
        )}
        {canNewTab && (
          <Button
            variant="ghost"
            size="icon"
            aria-label="Open in a new tab"
            title="Open in a new tab"
            onClick={openTab}
          >
            <ExternalLink size={14} />
          </Button>
        )}
        <Button
          variant="ghost"
          size="icon"
          aria-label="Close tab"
          title="Close tab"
          onClick={onClose}
        >
          <X size={14} />
        </Button>
      </div>
      <div className="preview-stage" ref={stage}>
        {view.kind === "loading" && <p className="preview-note">Loading…</p>}
        {view.kind === "error" && (
          <p className="preview-note warn">
            {tab.missing ? "This file is no longer there." : view.message}
          </p>
        )}
        {view.kind === "stopped" && (
          <div className="preview-note">
            <p>
              {tab.command} stopped (the workbench restarted, or it was
              stopped).
            </p>
            <Button
              size="sm"
              onClick={() =>
                api<{ preview: Preview; grant: string }>("previews", "POST", {
                  session,
                  kind: "service",
                  command: tab.command,
                  port: tab.port,
                })
                  .then((r) => {
                    onUpdate({ preview: r.preview.id, running: true });
                    setLoad((n) => n + 1);
                  })
                  .catch(onError)
              }
            >
              Start again
            </Button>
          </div>
        )}
        {page && page.mode === "unavailable" && (
          <p className="preview-note">
            Web services open only in a browser on the workbench computer, at{" "}
            {canonicalAddress(page.preview)}. Files open anywhere.
          </p>
        )}
        {page && page.mode !== "unavailable" && (
          <iframe
            key={`${frameName}`}
            title="Preview"
            name={frameName}
            className={"preview-frame" + (width ? " sized" : "")}
            style={
              width
                ? {
                    width,
                    height: scale < 1 ? size.h / scale : size.h,
                    transform: scale < 1 ? `scale(${scale})` : undefined,
                    left: scale < 1 ? 0 : Math.max(0, (size.w - width) / 2),
                  }
                : undefined
            }
            {...(page.mode === "file"
              ? {
                  src: page.preview.fileUrl + (load ? `?r=${load}` : ""),
                  sandbox: fileFrameSandbox(page.preview.entry),
                  referrerPolicy: "no-referrer" as const,
                }
              : {
                  sandbox:
                    "allow-scripts allow-same-origin allow-forms allow-downloads allow-popups",
                  referrerPolicy: "strict-origin" as const,
                })}
          />
        )}
      </div>
    </div>
  );
}

// Signs a preview origin in for one frame or window, by name.
function submitGrant(p: Preview, grant: string, target: string) {
  const form = document.createElement("form");
  form.method = "POST";
  form.action = p.url + "/__authorize";
  form.target = target;
  const input = document.createElement("input");
  input.name = "grant";
  input.value = grant;
  form.appendChild(input);
  document.body.appendChild(form);
  form.submit();
  form.remove();
}
