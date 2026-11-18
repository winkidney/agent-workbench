import { createContext, useContext, useState } from "react";
import {
  ExternalLink,
  File as FileIcon,
  Film,
  Globe,
  Image as ImageIcon,
  ImageOff,
  LocateFixed,
} from "lucide-react";
import type { Media } from "./model";

// Preview hooks supplied by the workbench shell. Paths are relative to the
// session working directory; loopback URLs open in the preview panel.
// Paths are relative to the session's launch directory, or to base (the
// directory the agent was in) when given.
export type Env = {
  cwd: string;
  openUrl: (url: string) => void;
  // at: the lines a link pointed to (file.go:12, #L12-L20).
  openPath: (path: string, base?: string, at?: LineAt) => void;
  // stamp: the block showing it (see StampContext).
  thumb: (path: string, base?: string, stamp?: string) => string | null;
  // On the workbench computer, shows the file or folder in its file manager;
  // anywhere else (another device, a tunnel), downloads it.
  reveal: (path: string, base?: string) => void;
  // Shows the file or folder in the preview panel's file list.
  locate: (path: string, base?: string) => void;
  // Whether this page runs on the workbench computer (reveal shows, rather
  // than downloads).
  here?: boolean;
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
  // A subagent's own transcript, once the session list has it: Claude's by
  // agent id, Codex's by thread; and opening it.
  subagentSession?: (ref: {
    agentId?: string;
    threadId?: string;
  }) => string | undefined;
  openSession?: (id: string) => void;
  // Whether the session list has this session.
  listed?: (id: string) => boolean;
};

export type ViewItem = {
  kind: "image" | "video";
  src: string;
  title: string;
  path?: string;
  base?: string;
  // An image added to the message being written (its attachment key): a
  // marked-up copy takes its place.
  attachment?: string;
};

export const EnvContext = createContext<Env>({
  cwd: "",
  openUrl: (url) => window.open(url, "_blank", "noopener,noreferrer"),
  openPath: () => {},
  thumb: () => null,
  reveal: () => {},
  locate: () => {},
  view: () => {},
});
export const useEnv = () => useContext(EnvContext);

// The directory the agent was in for the block being rendered.
export const BaseContext = createContext<string | undefined>(undefined);
export const useBase = () => useContext(BaseContext);

// The block being rendered. A workspace image shown by a later block gets an
// address of its own, so the page fetches it again: agents rewrite files
// under the same name, and the browser reuses an image it holds by address.
export const StampContext = createContext<string | undefined>(undefined);
export const useStamp = () => useContext(StampContext);

const LOOPBACK =
  /^https?:\/\/(?:127\.\d+\.\d+\.\d+|localhost|\[::1\]|0\.0\.0\.0)(?::\d+)?(?:[/?#]|$)/i;
export const isLoopback = (url: string) => LOOPBACK.test(url);

export const RASTER = /\.(png|jpe?g|gif|webp|avif|bmp)$/i;
export const VIDEO = /\.(mp4|webm|mov|m4v)$/i;
export const isMedia = (path: string) => RASTER.test(path) || VIDEO.test(path);
export const PREVIEWABLE =
  /\.(png|jpe?g|gif|webp|avif|bmp|svg|ico|mp4|webm|mov|m4v|html?|pdf)$/i;
export const MARKDOWN = /\.(md|markdown|mdx)$/i;
// Text the preview panel shows as it is (Markdown drawn); the panel asks the
// server whether a file really is text before showing it so.
export const TEXT =
  /(?:^|\/)(?:makefile|dockerfile|license|readme|changelog|\.env[\w.-]*|\.gitignore|\.editorconfig)$|\.(?:md|markdown|mdx|txt|log|json|jsonl|ndjson|ipynb|ya?ml|toml|ini|cfg|conf|properties|env|csv|tsv|xml|go|mod|sum|rs|py|js|mjs|cjs|ts|tsx|jsx|vue|svelte|css|scss|less|sh|bash|zsh|fish|ps1|sql|rb|php|pl|java|kt|kts|gradle|swift|dart|c|h|cc|cpp|hpp|cs|lua|r|gd|gdshader|tscn|tres|glsl|wgsl|shader|proto|graphql|diff|patch|lock|dockerfile|makefile)$/i;
// Whether a path opens in the preview panel; anything else is shown in its
// file list.
export const opensInPanel = (path: string) =>
  !path.endsWith("/") && (PREVIEWABLE.test(path) || TEXT.test(path));

// The lines a link points to, written after the path as editors and both
// CLIs do: file.go:12, :12:5, :12-20, :12:5-20:3, or GitHub's #L12, #L12C5,
// #L12-L20. Only a trailing reference counts; anything else stays the path.
export type LineAt = { line: number; end?: number };
const LINE_REF =
  /(?::(\d+)(?::\d+)?(?:-(\d+)(?::\d+)?)?|#L(\d+)(?:C\d+)?(?:-L(\d+)(?:C\d+)?)?)$/;
export function splitLineRef(href: string): { path: string; at?: LineAt } {
  // A web address keeps its port and fragment (host:8080, #L12 on GitHub);
  // only file:// is a path.
  if (/^[a-z][\w+.-]*:\/\//i.test(href) && !/^file:\/\//i.test(href))
    return { path: href };
  const m = LINE_REF.exec(href);
  if (!m || m.index === 0) return { path: href };
  const line = Number(m[1] ?? m[3]);
  const end = Number(m[2] ?? m[4]) || undefined;
  if (!line) return { path: href };
  return {
    path: href.slice(0, m.index),
    at: end && end > line ? { line, end } : { line },
  };
}

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
  stamp?: string,
): ViewItem | null {
  const src = isMedia(path) ? env.thumb(path, base, stamp) : null;
  if (!src) return null;
  return {
    kind: VIDEO.test(path) ? "video" : "image",
    src,
    title: path,
    path,
    base,
  };
}

// Images and videos open in the viewer, pages, documents and text in the
// side panel, anything else is shown in the panel's file list (from there,
// in the file manager).
export function openWorkspacePath(
  env: Env,
  path: string,
  base?: string,
  at?: LineAt,
) {
  const item = pathItem(env, path, base);
  if (item) env.view([item], 0);
  else if (opensInPanel(path))
    at ? env.openPath(path, base, at) : env.openPath(path, base);
  else env.locate(path, base);
}

// What opening a path does, said on it.
export const openLabel = (path: string, at?: LineAt) =>
  isMedia(path)
    ? `View ${path}`
    : opensInPanel(path)
      ? `Preview ${path}` + (at ? ` at ${lineLabel(at)}` : "")
      : `Show ${path} in Files`;
const lineLabel = (at: LineAt) =>
  at.end ? `lines ${at.line}–${at.end}` : `line ${at.line}`;

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
        title={openLabel(rel)}
        onClick={() => openWorkspacePath(env, rel, base)}
      >
        <Icon size={12} />
        {path}
      </button>
      <LocateButton path={rel} base={base} />
    </span>
  );
}

// Shows a path in the preview panel's file list (where its menu shows it in
// the file manager, or downloads it).
export function LocateButton({ path, base }: { path: string; base?: string }) {
  const env = useEnv();
  return (
    <button
      type="button"
      className="path-reveal"
      title="Show in Files"
      aria-label={`Show ${path} in Files`}
      onClick={() => env.locate(path, base)}
    >
      <LocateFixed size={12} />
    </button>
  );
}

// Inline code that names a file or folder: it keeps the terminal look, and
// opens as a link to it would (preview, or the panel's file list).
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
      title={openLabel(rel)}
      onClick={() => openWorkspacePath(env, rel, base)}
      onKeyDown={(e) => {
        if (e.key === "Enter" || e.key === " ") {
          e.preventDefault();
          openWorkspacePath(env, rel, base);
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
  // A file link may name lines after the path (file.go:12).
  const { path, at } = splitLineRef(href);
  const rel = workspacePath(path, env.cwd);
  if (rel)
    return (
      <a
        href={"#" + rel}
        title={openLabel(rel, at)}
        onClick={(e) => {
          e.preventDefault();
          openWorkspacePath(env, rel, base, at);
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
  missing,
}: {
  src: string;
  title: string;
  onOpen: () => void;
  // Guessed from plain text: hide instead of showing a broken image.
  quiet?: boolean;
  // What a missing image says instead of its title.
  missing?: string;
}) {
  const [failed, setFailed] = useState(false);
  if (failed && quiet) return null;
  if (failed)
    return (
      <span className="media-missing">
        <ImageOff size={13} /> {missing ?? title}
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

// Images added in the page, kept by the workbench (composer/attachments.ts).
const UPLOAD = /^\/api\/uploads\/[0-9a-f]{64}\.(png|jpg|webp)$/;
const omittedText = (bytes?: number) =>
  "Image omitted" + (bytes ? ` (${Math.round(bytes / 1024)} KB)` : "");

export function MediaList({
  media,
  quiet,
}: {
  media?: Media[];
  quiet?: boolean;
}) {
  const env = useEnv();
  const base = useBase();
  const stamp = useStamp();
  if (!media?.length) return null;
  // Everything viewable in this list forms one gallery in the viewer.
  const items: ViewItem[] = [];
  const slot = media.map((m) => {
    let item: ViewItem | null = null;
    if (m.kind === "data") item = { kind: "image", src: m.src, title: "Image" };
    else if (m.kind === "url" && UPLOAD.test(m.url))
      item = { kind: "image", src: m.url, title: "Image" };
    else if (m.kind === "url" && /^(https?:|data:image\/)/i.test(m.url))
      // An inline image is named "Image", not by its data.
      item = {
        kind: "image",
        src: m.url,
        title: /^data:/i.test(m.url) ? "Image" : m.url,
      };
    else if (m.kind === "omitted" && UPLOAD.test("/api/uploads/" + m.upload))
      // Too large for the record sent here: the copy kept when it was added
      // in the page, if it is still kept.
      item = { kind: "image", src: "/api/uploads/" + m.upload, title: "Image" };
    else if (m.kind === "path") {
      const rel = workspacePath(m.path, env.cwd);
      item = rel ? pathItem(env, rel, base, stamp) : null;
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
              missing={m.kind === "omitted" ? omittedText(m.bytes) : undefined}
              onOpen={open}
            />
          );
        switch (m.kind) {
          case "path":
            return <PathChip key={i} path={m.path} />;
          case "omitted":
            return (
              <span key={i} className="media-missing">
                <ImageOff size={13} /> {omittedText(m.bytes)}
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
