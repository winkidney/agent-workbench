import { createContext, useContext, useState } from "react";
import {
  ExternalLink,
  File as FileIcon,
  Film,
  FolderOpen,
  Globe,
  Image as ImageIcon,
  ImageOff,
} from "lucide-react";
import type { Media } from "./model";

// Preview hooks supplied by the workbench shell. Paths are relative to the
// session working directory; loopback URLs open in the preview panel.
// Paths are relative to the session's launch directory, or to base (the
// directory the agent was in) when given.
export type Env = {
  cwd: string;
  openUrl: (url: string) => void;
  openPath: (path: string, base?: string) => void;
  thumb: (path: string, base?: string) => string | null;
  // Shows the file in the file manager of the machine running the workbench.
  reveal: (path: string, base?: string) => void;
  // Opens images and videos in the zoomable viewer.
  view: (items: ViewItem[], index: number) => void;
  // Sends a message as the composer would: into the running turn if there
  // is one (answers to an agent's questions).
  answer?: (text: string) => Promise<void>;
  // Codex's proposed plan: implement it in this session (leaving plan
  // mode), or in a new session that starts from the plan alone.
  implementPlan?: () => Promise<void>;
  implementPlanFresh?: (plan: string) => Promise<void>;
  // The user decided about the plan here (kept planning, or implemented it
  // in a new session): it no longer needs them.
  dismissPlan?: () => void;
};

export type ViewItem = {
  kind: "image" | "video";
  src: string;
  title: string;
  path?: string;
  base?: string;
};

export const EnvContext = createContext<Env>({
  cwd: "",
  openUrl: (url) => window.open(url, "_blank", "noopener,noreferrer"),
  openPath: () => {},
  thumb: () => null,
  reveal: () => {},
  view: () => {},
});
export const useEnv = () => useContext(EnvContext);

// The directory the agent was in for the block being rendered.
export const BaseContext = createContext<string | undefined>(undefined);
export const useBase = () => useContext(BaseContext);

const LOOPBACK =
  /^https?:\/\/(?:127\.\d+\.\d+\.\d+|localhost|\[::1\]|0\.0\.0\.0)(?::\d+)?(?:[/?#]|$)/i;
export const isLoopback = (url: string) => LOOPBACK.test(url);

export const RASTER = /\.(png|jpe?g|gif|webp|avif|bmp)$/i;
export const VIDEO = /\.(mp4|webm|mov|m4v)$/i;
export const isMedia = (path: string) => RASTER.test(path) || VIDEO.test(path);
export const PREVIEWABLE =
  /\.(png|jpe?g|gif|webp|avif|bmp|svg|ico|mp4|webm|mov|m4v|html?|pdf)$/i;

// Map a path as written to one the server resolves: absolute paths must be
// under the launch directory (made relative); relative paths stay as they
// are, since they are relative to where the agent was. The server confines
// the result to the launch directory.
export function workspacePath(path: string, cwd: string): string | null {
  let p = path.trim().replace(/^file:\/\//, "");
  if (/^[a-z]+:/i.test(p)) return null;
  const root = cwd.replace(/\/$/, "");
  if (p.startsWith("/")) {
    if (!root || !p.startsWith(root + "/")) return null;
    p = p.slice(root.length + 1);
  }
  p = p.replace(/^\.\//, "");
  return p || null;
}

// A viewer item for a workspace image or video path.
export function pathItem(
  env: Env,
  path: string,
  base?: string,
): ViewItem | null {
  const src = isMedia(path) ? env.thumb(path, base) : null;
  if (!src) return null;
  return {
    kind: VIDEO.test(path) ? "video" : "image",
    src,
    title: path,
    path,
    base,
  };
}

// Images and videos open in the viewer, other previewable files in the
// side panel, anything else in the file manager.
export function openWorkspacePath(env: Env, path: string, base?: string) {
  const item = pathItem(env, path, base);
  if (item) env.view([item], 0);
  else if (PREVIEWABLE.test(path)) env.openPath(path, base);
  else env.reveal(path, base);
}

export function PathChip({ path }: { path: string }) {
  const env = useEnv();
  const base = useBase();
  const rel = workspacePath(path, env.cwd);
  if (!rel) return <code>{path}</code>;
  const Icon = VIDEO.test(rel) ? Film : RASTER.test(rel) ? ImageIcon : FileIcon;
  return (
    <span className="path-chip-group">
      <button
        type="button"
        className="path-chip"
        title={isMedia(rel) ? `View ${rel}` : `Preview ${rel}`}
        onClick={() => openWorkspacePath(env, rel, base)}
      >
        <Icon size={12} />
        {path}
      </button>
      <RevealButton path={rel} base={base} />
    </span>
  );
}

export function RevealButton({ path, base }: { path: string; base?: string }) {
  const env = useEnv();
  return (
    <button
      type="button"
      className="path-reveal"
      title="Show in Files (opens on this computer)"
      aria-label={`Show ${path} in Files`}
      onClick={() => env.reveal(path, base)}
    >
      <FolderOpen size={12} />
    </button>
  );
}

// Inline code that names a file or folder (not previewable): it keeps the
// terminal look and shows the item in the file manager when clicked.
export const PATHLIKE =
  /^(?:~?\/|\.{1,2}\/)?[\w@.+-]+(?:\/[\w@.+-]+)+\/?$|^[\w@+-][\w@.+-]*\.(?:md|json|jsonl|ya?ml|toml|ini|cfg|conf|txt|log|csv|tsv|go|mod|sum|rs|py|ipynb|js|mjs|cjs|ts|tsx|jsx|css|scss|sh|bash|zsh|fish|sql|rb|java|kt|swift|c|h|cc|cpp|hpp|cs|lua|gd|gdshader|tscn|tres|glsl|wgsl|shader|lock|env|dockerfile|makefile|zip|tar|gz|wav|mp3|ogg|flac|blend|fbx|glb|gltf|obj|psd|kra|aseprite)$/i;

export function PathCode({ path }: { path: string }) {
  const env = useEnv();
  const base = useBase();
  const rel = workspacePath(path, env.cwd);
  if (!rel || /:\/\//.test(path)) return <code>{path}</code>;
  return (
    <code
      className="path-code"
      role="button"
      tabIndex={0}
      title={`Show ${rel} in Files (opens on this computer)`}
      onClick={() => env.reveal(rel, base)}
      onKeyDown={(e) => {
        if (e.key === "Enter" || e.key === " ") {
          e.preventDefault();
          env.reveal(rel, base);
        }
      }}
    >
      {path}
    </code>
  );
}

export function UrlLink({
  href,
  children,
}: {
  href: string;
  children: React.ReactNode;
}) {
  const env = useEnv();
  const base = useBase();
  if (isLoopback(href))
    return (
      <span className="url-link">
        <a
          href={href}
          onClick={(e) => {
            e.preventDefault();
            env.openUrl(href);
          }}
        >
          {children}
        </a>
        <button
          type="button"
          className="url-preview"
          title="Open in preview panel"
          onClick={() => env.openUrl(href)}
        >
          <Globe size={11} /> Preview
        </button>
      </span>
    );
  if (/^https?:/i.test(href))
    return (
      <a href={href} target="_blank" rel="noopener noreferrer">
        {children}
        <ExternalLink size={11} className="inline-icon" />
      </a>
    );
  const rel = workspacePath(href, env.cwd);
  if (rel)
    return (
      <a
        href={"#" + rel}
        onClick={(e) => {
          e.preventDefault();
          openWorkspacePath(env, rel, base);
        }}
      >
        {children}
      </a>
    );
  return <span>{children}</span>;
}

function Thumb({
  src,
  title,
  onOpen,
  quiet,
}: {
  src: string;
  title: string;
  onOpen: () => void;
  // Guessed from plain text: hide instead of showing a broken image.
  quiet?: boolean;
}) {
  const [failed, setFailed] = useState(false);
  if (failed && quiet) return null;
  if (failed)
    return (
      <span className="media-missing">
        <ImageOff size={13} /> {title}
      </span>
    );
  return (
    <button
      type="button"
      className="media-thumb"
      title={`View ${title}`}
      onClick={onOpen}
    >
      <img
        src={src}
        alt={title}
        loading="lazy"
        onError={() => setFailed(true)}
      />
    </button>
  );
}

export function MediaList({
  media,
  quiet,
}: {
  media?: Media[];
  quiet?: boolean;
}) {
  const env = useEnv();
  const base = useBase();
  if (!media?.length) return null;
  // Everything viewable in this list forms one gallery in the viewer.
  const items: ViewItem[] = [];
  const slot = media.map((m) => {
    let item: ViewItem | null = null;
    if (m.kind === "data") item = { kind: "image", src: m.src, title: "Image" };
    else if (m.kind === "url" && /^(https?:|data:image\/)/i.test(m.url))
      item = { kind: "image", src: m.url, title: m.url };
    else if (m.kind === "path") {
      const rel = workspacePath(m.path, env.cwd);
      item = rel ? pathItem(env, rel, base) : null;
    }
    if (!item) return -1;
    items.push(item);
    return items.length - 1;
  });
  return (
    <div className="media-list">
      {media.map((m, i) => {
        const at = slot[i];
        const open = () => env.view(items, at);
        if (at >= 0 && items[at].kind === "video")
          return (
            <button
              key={i}
              type="button"
              className="media-video"
              title={`Play ${items[at].title}`}
              onClick={open}
            >
              <Film size={14} /> {items[at].title.split("/").pop()}
            </button>
          );
        if (at >= 0)
          return (
            <Thumb
              key={i}
              src={items[at].src}
              title={items[at].title}
              quiet={quiet && m.kind === "path"}
              onOpen={open}
            />
          );
        switch (m.kind) {
          case "path":
            return <PathChip key={i} path={m.path} />;
          case "omitted":
            return (
              <span key={i} className="media-missing">
                <ImageOff size={13} /> Image omitted
                {m.bytes ? ` (${Math.round(m.bytes / 1024)} KB)` : ""}
              </span>
            );
        }
        return null;
      })}
    </div>
  );
}

// Workspace media paths and URLs mentioned in plain output.
const MENTION =
  /(https?:\/\/[^\s'"<>)\]]+)|((?:\/|\.\/)?[\w.@~-]+(?:\/[\w.@~-]+)*\.(?:png|jpe?g|gif|webp|avif|bmp|svg|mp4|webm|mov|html?|pdf))\b/gi;

export function mentions(text: string, cwd: string, max = 6) {
  const urls = new Set<string>();
  const paths = new Set<string>();
  for (const m of text.matchAll(MENTION)) {
    if (m[1] && isLoopback(m[1])) urls.add(m[1].replace(/[.,;:]+$/, ""));
    else if (m[2]) {
      const rel = workspacePath(m[2], cwd);
      if (rel) paths.add(rel);
    }
    if (urls.size + paths.size >= max) break;
  }
  return { urls: [...urls], paths: [...paths] };
}

export function Mentions({ text }: { text?: string }) {
  const env = useEnv();
  if (!text) return null;
  const { urls, paths } = mentions(text, env.cwd);
  if (!urls.length && !paths.length) return null;
  const images = paths.filter(isMedia);
  const others = paths.filter((p) => !isMedia(p));
  return (
    <div className="mentions">
      {images.length > 0 && (
        <MediaList
          quiet
          media={images.map((path) => ({ kind: "path", path }))}
        />
      )}
      {(urls.length > 0 || others.length > 0) && (
        <div className="chip-row">
          {urls.map((u) => (
            <button
              key={u}
              type="button"
              className="path-chip"
              onClick={() => env.openUrl(u)}
            >
              <Globe size={12} /> {u.replace(/^https?:\/\//, "")}
            </button>
          ))}
          {others.map((p) => (
            <PathChip key={p} path={p} />
          ))}
        </div>
      )}
    </div>
  );
}
