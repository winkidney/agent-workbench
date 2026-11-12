import { useEffect, useState } from "react";
import { ArrowUp, Eye, EyeOff, Folder, GitBranch } from "lucide-react";
import { api } from "../lib/api";
import { tildePath } from "./recent";

export type DirListing = {
  path: string;
  parent?: string;
  home: string;
  entries: { name: string; path: string; git: boolean; hidden: boolean }[];
};

// Server-side folder browser: the browser's own picker cannot return paths.
// Only the home directory tree is listed.
export function DirBrowser({
  start,
  hidden = false,
  onPick,
  onCancel,
}: {
  start?: string;
  // Show hidden folders from the start (a CLI's home is one).
  hidden?: boolean;
  onPick: (path: string) => void;
  onCancel: () => void;
}) {
  const [listing, setListing] = useState<DirListing | null>(null);
  const [path, setPath] = useState(start ?? "");
  const [selected, setSelected] = useState("");
  const [showHidden, setShowHidden] = useState(hidden);
  const [error, setError] = useState("");
  useEffect(() => {
    let cancelled = false;
    setError("");
    api<DirListing>("dirs?path=" + encodeURIComponent(path))
      .then((v) => {
        if (cancelled) return;
        setListing(v);
        setSelected("");
      })
      .catch((e) => {
        if (cancelled) return;
        setError(e instanceof Error ? e.message : String(e));
        // Fall back to home when the start folder is outside it or missing.
        if (path) setPath("");
      });
    return () => {
      cancelled = true;
    };
  }, [path]);
  const crumbs = (() => {
    if (!listing) return [];
    const rel = tildePath(listing.path, listing.home);
    const parts = rel.split("/").filter(Boolean);
    let acc = listing.home;
    return parts.map((name, i) => {
      if (i > 0) acc = acc + "/" + name;
      return { name, path: i === 0 ? listing.home : acc };
    });
  })();
  const entries = (listing?.entries ?? []).filter(
    (e) => showHidden || !e.hidden,
  );
  const target = selected || listing?.path || "";
  return (
    <div className="dir-browser" role="dialog" aria-label="Choose a folder">
      <div className="dir-head">
        <button
          type="button"
          className="icon-button"
          aria-label="Parent folder"
          title="Parent folder"
          disabled={!listing?.parent}
          onClick={() => listing?.parent && setPath(listing.parent)}
        >
          <ArrowUp size={14} />
        </button>
        <nav className="dir-crumbs" aria-label="Folder path">
          {crumbs.map((c, i) => (
            <span key={c.path}>
              {i > 0 && <span className="dir-sep">/</span>}
              <button type="button" onClick={() => setPath(c.path)}>
                {c.name}
              </button>
            </span>
          ))}
        </nav>
        <button
          type="button"
          className="icon-button"
          aria-pressed={showHidden}
          aria-label={
            showHidden ? "Hide hidden folders" : "Show hidden folders"
          }
          title={showHidden ? "Hide hidden folders" : "Show hidden folders"}
          onClick={() => setShowHidden(!showHidden)}
        >
          {showHidden ? <EyeOff size={14} /> : <Eye size={14} />}
        </button>
      </div>
      <div className="dir-list" role="listbox" aria-label="Folders">
        {entries.map((e) => (
          <button
            type="button"
            role="option"
            aria-selected={selected === e.path}
            key={e.path}
            className={"dir-item" + (selected === e.path ? " selected" : "")}
            onClick={() => setSelected(e.path)}
            onDoubleClick={() => setPath(e.path)}
            onKeyDown={(k) => {
              if (k.key === "Enter") {
                k.preventDefault();
                setPath(e.path);
              }
            }}
          >
            <Folder size={14} />
            <span className="dir-name">{e.name}</span>
            {e.git && (
              <GitBranch
                size={12}
                className="dir-git"
                aria-label="Git repository"
              />
            )}
          </button>
        ))}
        {listing && !entries.length && (
          <p className="dir-empty">No folders here</p>
        )}
        {error && <p className="error-text dir-empty">{error}</p>}
      </div>
      <div className="dir-foot">
        <span className="dir-target" title={target}>
          {tildePath(target, listing?.home)}
        </span>
        <button type="button" className="text-button" onClick={onCancel}>
          Back
        </button>
        <button
          type="button"
          className="primary-button"
          disabled={!target}
          onClick={() => onPick(target)}
        >
          Use this folder
        </button>
      </div>
    </div>
  );
}
