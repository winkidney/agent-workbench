import { useEffect, useMemo, useRef, useState } from "react";
import {
  ChevronDown,
  Folder,
  FolderSearch,
  GitBranch,
  Pin,
} from "lucide-react";
import { api } from "../lib/api";
import { relativeTime } from "../sidebar/tree";
import { DirBrowser, type DirListing } from "./DirBrowser";
import {
  completionTarget,
  filterRecent,
  isPathInput,
  tildePath,
  type RecentProject,
} from "./recent";

type Option =
  | { kind: "recent"; project: RecentProject }
  | { kind: "dir"; path: string; name: string; git: boolean }
  | { kind: "typed"; path: string };

// Project combobox: recent launch directories, typed paths with completion,
// or a server-side folder browser.
export function ProjectPicker({
  value,
  recent,
  onChange,
}: {
  value: string;
  recent: RecentProject[];
  onChange: (path: string) => void;
}) {
  const [open, setOpen] = useState(false);
  const [browsing, setBrowsing] = useState(false);
  const [query, setQuery] = useState("");
  const [active, setActive] = useState(0);
  const [completion, setCompletion] = useState<DirListing | null>(null);
  const [home, setHome] = useState<string>();
  const [up, setUp] = useState(false);
  const root = useRef<HTMLDivElement>(null);
  const input = useRef<HTMLInputElement>(null);
  const now = Date.now();

  useEffect(() => {
    void api<DirListing>("dirs")
      .then((v) => setHome(v.home))
      .catch(() => {});
  }, []);
  useEffect(() => {
    if (!open) return;
    const down = (e: MouseEvent) => {
      if (!root.current?.contains(e.target as Node)) close();
    };
    document.addEventListener("mousedown", down);
    return () => document.removeEventListener("mousedown", down);
  }, [open]);
  // Shell-like completion for typed paths.
  const pathMode = isPathInput(query);
  const target = pathMode ? completionTarget(query) : null;
  // Browsing is limited to home; don't probe absolute paths outside it.
  const browsable =
    !!target &&
    (target.dir.startsWith("~") ||
      (!!home && (target.dir === home || target.dir.startsWith(home + "/"))));
  useEffect(() => {
    if (!target || !browsable) {
      setCompletion(null);
      return;
    }
    let cancelled = false;
    const t = setTimeout(() => {
      api<DirListing>("dirs?path=" + encodeURIComponent(target.dir))
        .then((v) => !cancelled && setCompletion(v))
        .catch(() => !cancelled && setCompletion(null));
    }, 120);
    return () => {
      cancelled = true;
      clearTimeout(t);
    };
  }, [target?.dir, browsable]);

  const options = useMemo<Option[]>(() => {
    if (!pathMode)
      return filterRecent(recent, query)
        .slice(0, 12)
        .map((project) => ({ kind: "recent", project }));
    const prefix = (target?.prefix ?? "").toLowerCase();
    const dirs: Option[] = (completion?.entries ?? [])
      .filter(
        (e) =>
          e.name.toLowerCase().startsWith(prefix) &&
          (!e.hidden || prefix.startsWith(".")),
      )
      .slice(0, 12)
      .map((e) => ({ kind: "dir", path: e.path, name: e.name, git: e.git }));
    return [
      { kind: "typed", path: query.trim().replace(/\/$/, "") || "/" },
      ...dirs,
    ];
  }, [pathMode, query, recent, completion, target?.prefix]);

  useEffect(() => setActive(0), [query]);

  const close = () => {
    setOpen(false);
    setBrowsing(false);
    setQuery("");
  };
  const pick = (path: string) => {
    onChange(path);
    close();
  };
  const choose = (o: Option) =>
    pick(o.kind === "recent" ? o.project.path : o.path);
  const current = recent.find((p) => p.path === value);
  const label =
    current?.name ??
    (value.split("/").filter(Boolean).pop() || "Choose a project");

  return (
    <div className="project-picker" ref={root}>
      <button
        type="button"
        className="picker-trigger"
        aria-haspopup="listbox"
        aria-expanded={open}
        onClick={(e) => {
          // Open upward when the space below cannot fit the list.
          const r = e.currentTarget.getBoundingClientRect();
          setUp(innerHeight - r.bottom < 440 && r.top > innerHeight - r.bottom);
          setOpen(!open);
          setBrowsing(false);
          setTimeout(() => input.current?.focus(), 0);
        }}
      >
        <Folder size={14} />
        <span className="picker-name">{label}</span>
        {value && <span className="picker-path">{tildePath(value, home)}</span>}
        <ChevronDown size={13} />
      </button>
      {open && (
        <div className={"picker-pop" + (up ? " up" : "")}>
          {browsing ? (
            <DirBrowser
              start={value}
              onPick={pick}
              onCancel={() => setBrowsing(false)}
            />
          ) : (
            <>
              <input
                ref={input}
                className="picker-input"
                aria-label="Search projects or type a path"
                placeholder="Search projects, or type a path like ~/work/app"
                value={query}
                onChange={(e) => setQuery(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === "ArrowDown" || e.key === "ArrowUp") {
                    e.preventDefault();
                    const n = options.length || 1;
                    setActive(
                      (a) => (a + (e.key === "ArrowDown" ? 1 : n - 1)) % n,
                    );
                  } else if (e.key === "Enter") {
                    e.preventDefault();
                    if (options[active]) choose(options[active]);
                  } else if (
                    e.key === "Tab" &&
                    pathMode &&
                    options[active]?.kind === "dir"
                  ) {
                    // Tab completes the highlighted folder and keeps typing.
                    e.preventDefault();
                    const o = options[active] as { path: string };
                    setQuery(tildePath(o.path, home) + "/");
                  } else if (e.key === "Escape") {
                    e.preventDefault();
                    close();
                  }
                }}
              />
              <div className="picker-list" role="listbox" aria-label="Projects">
                {!pathMode && !query && recent.length > 0 && (
                  <div className="picker-group">Recent projects</div>
                )}
                {options.map((o, i) => (
                  <button
                    type="button"
                    role="option"
                    aria-selected={i === active}
                    key={
                      o.kind + (o.kind === "recent" ? o.project.path : o.path)
                    }
                    className={"picker-item" + (i === active ? " active" : "")}
                    onMouseEnter={() => setActive(i)}
                    onClick={() => choose(o)}
                  >
                    {o.kind === "recent" ? (
                      <>
                        {o.project.pinned ? (
                          <Pin size={13} />
                        ) : (
                          <Folder size={13} />
                        )}
                        <span className="picker-item-main">
                          <span className="picker-item-name">
                            {o.project.name}
                          </span>
                          <span className="picker-item-path">
                            {tildePath(o.project.path, home)}
                          </span>
                        </span>
                        <span className="picker-item-time">
                          {relativeTime(o.project.latest, now)}
                        </span>
                      </>
                    ) : o.kind === "dir" ? (
                      <>
                        <Folder size={13} />
                        <span className="picker-item-main">
                          <span className="picker-item-name">{o.name}</span>
                          <span className="picker-item-path">
                            {tildePath(o.path, home)}
                          </span>
                        </span>
                        {o.git && <GitBranch size={12} className="dir-git" />}
                      </>
                    ) : (
                      <>
                        <Folder size={13} />
                        <span className="picker-item-main">
                          <span className="picker-item-name">Use {o.path}</span>
                          <span className="picker-item-path">
                            Tab completes a highlighted folder
                          </span>
                        </span>
                      </>
                    )}
                  </button>
                ))}
                {!options.length && (
                  <p className="dir-empty">No matching projects</p>
                )}
              </div>
              <button
                type="button"
                className="picker-browse"
                onClick={() => setBrowsing(true)}
              >
                <FolderSearch size={14} /> Browse folders…
              </button>
            </>
          )}
        </div>
      )}
    </div>
  );
}
