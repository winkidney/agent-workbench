import { useEffect, useRef, useState, type ReactNode } from "react";
import { Button } from "../components/ui/button";
import { api } from "../lib/api";
import type { PreviewTab } from "./tabs";
import { parseControl, retryDelay, socketUrl, terminalTheme } from "./terminal";

type State =
  | { kind: "connecting" }
  | { kind: "live" }
  | { kind: "lost" }
  | { kind: "exited"; code: number }
  | { kind: "missing" }
  | { kind: "signedout" }
  | { kind: "error"; message: string };

// A terminal tab's view. Everything here (xterm, its socket, observers and
// timers) exists only while the tab is shown and is let go when it is not;
// the shell keeps running on the workbench until the terminal is closed.
export function TerminalView({
  tab,
  controls,
  onEnded,
  onRestart,
  onClose,
}: {
  tab: PreviewTab;
  controls: ReactNode;
  // The terminal ended (its shell exited) or is gone (the workbench
  // restarted): the tab says so.
  onEnded: (patch: Partial<PreviewTab>) => void;
  onRestart: () => void;
  onClose: () => void;
}) {
  const host = useRef<HTMLDivElement>(null);
  const [state, setState] = useState<State>(
    tab.missing
      ? { kind: "missing" }
      : tab.code !== undefined
        ? { kind: "exited", code: tab.code }
        : { kind: "connecting" },
  );
  const ended = state.kind === "exited" || state.kind === "missing";
  const id = tab.terminal ?? "";

  useEffect(() => {
    if (!id || ended) return;
    let disposed = false;
    let socket: WebSocket | null = null;
    let retry: ReturnType<typeof setTimeout> | undefined;
    let resizing: ReturnType<typeof setTimeout> | undefined;
    let observer: ResizeObserver | null = null;
    let themes: MutationObserver | null = null;
    let scheme: MediaQueryList | null = null;
    let term: import("@xterm/xterm").Terminal | null = null;
    let lost = 0;
    let sent = "";
    const onScheme = () => {
      if (term)
        term.options.theme = terminalTheme(
          getComputedStyle(document.documentElement),
        );
    };

    void import("./xterm")
      .then(({ Terminal, FitAddon }) => {
        if (disposed || !host.current) return;
        const style = getComputedStyle(document.documentElement);
        const t = new Terminal({
          fontFamily: style.getPropertyValue("--mono").trim() || "monospace",
          fontSize: 13,
          cursorBlink: true,
          scrollback: 5000,
          theme: terminalTheme(style),
        });
        term = t;
        const fit = new FitAddon();
        t.loadAddon(fit);
        t.open(host.current);
        // The workbench is told the size; the shell redraws to it.
        const size = (now = false) => {
          try {
            fit.fit();
          } catch {
            return;
          }
          const key = `${t.cols}x${t.rows}`;
          if (key === sent || !t.cols || !t.rows) return;
          clearTimeout(resizing);
          const tell = () => {
            sent = key;
            void api(`terminals/${encodeURIComponent(id)}/resize`, "POST", {
              cols: t.cols,
              rows: t.rows,
            }).catch(() => {});
          };
          // At once when attaching, so the shell draws at the right width
          // from the start; while dragging, once it settles.
          if (now) tell();
          else resizing = setTimeout(tell, 100);
        };
        observer = new ResizeObserver(() => size());
        observer.observe(host.current);
        // Theme changes: the page's attribute, or the system's scheme.
        themes = new MutationObserver(onScheme);
        themes.observe(document.documentElement, {
          attributes: true,
          attributeFilter: ["data-theme", "class", "style"],
        });
        scheme = matchMedia("(prefers-color-scheme: dark)");
        scheme.addEventListener("change", onScheme);
        // Keys go to the shell as they are typed.
        const encoder = new TextEncoder();
        t.onData((d) => {
          if (socket?.readyState === WebSocket.OPEN)
            socket.send(encoder.encode(d));
        });
        t.onBinary((d) => {
          if (socket?.readyState !== WebSocket.OPEN) return;
          const bytes = Uint8Array.from(d, (c) => c.charCodeAt(0) & 0xff);
          socket.send(bytes);
        });

        const connect = () => {
          if (disposed) return;
          setState({ kind: "connecting" });
          const ws = new WebSocket(socketUrl(id, location));
          ws.binaryType = "arraybuffer";
          socket = ws;
          let done = false;
          ws.onopen = () => {
            lost = 0;
            // The workbench sends what the terminal showed so far first.
            t.reset();
            sent = "";
            size(true);
            setState({ kind: "live" });
            t.focus();
          };
          ws.onmessage = (e) => {
            if (typeof e.data !== "string") {
              t.write(new Uint8Array(e.data as ArrayBuffer));
              return;
            }
            const c = parseControl(e.data);
            if (c.exit !== undefined) {
              done = true;
              setState({ kind: "exited", code: c.exit });
              onEnded({ running: false, code: c.exit });
            } else if (c.missing) {
              done = true;
              setState({ kind: "missing" });
              onEnded({ running: false, missing: true });
            }
          };
          ws.onclose = (e) => {
            if (disposed || done) return;
            if (e.code === 1008) {
              setState({ kind: "signedout" });
              return;
            }
            setState({ kind: "lost" });
            retry = setTimeout(connect, retryDelay(lost++));
          };
        };
        connect();
      })
      .catch((e) => {
        if (!disposed)
          setState({
            kind: "error",
            message: e instanceof Error ? e.message : String(e),
          });
      });

    return () => {
      disposed = true;
      clearTimeout(retry);
      clearTimeout(resizing);
      observer?.disconnect();
      themes?.disconnect();
      scheme?.removeEventListener("change", onScheme);
      if (socket) {
        socket.onclose = null;
        socket.onmessage = null;
        socket.close();
      }
      term?.dispose();
    };
  }, [id, ended]);

  return (
    <div className="preview-view terminal-view">
      <div className="preview-toolbar">
        <span title={tab.title}>
          {tab.title || "Terminal"}
          {state.kind === "connecting" && " · connecting…"}
          {state.kind === "lost" && " · reconnecting…"}
        </span>
        {controls}
      </div>
      <div className="terminal-stage">
        {!ended && <div className="terminal-host" ref={host} />}
        {state.kind === "exited" && (
          <div className="preview-note">
            <p>The shell exited ({state.code}).</p>
            <div className="terminal-actions">
              <Button size="sm" onClick={onRestart}>
                Start a new shell
              </Button>
              <Button size="sm" variant="outline" onClick={onClose}>
                Close
              </Button>
            </div>
          </div>
        )}
        {state.kind === "missing" && (
          <div className="preview-note">
            <p>This terminal ended when the workbench restarted.</p>
            <div className="terminal-actions">
              <Button size="sm" onClick={onRestart}>
                New terminal here
              </Button>
              <Button size="sm" variant="outline" onClick={onClose}>
                Close
              </Button>
            </div>
          </div>
        )}
        {state.kind === "signedout" && (
          <p className="preview-note warn">
            This device was signed out. Sign in again to use the terminal; it is
            still running.
          </p>
        )}
        {state.kind === "error" && (
          <p className="preview-note warn">{state.message}</p>
        )}
      </div>
    </div>
  );
}
