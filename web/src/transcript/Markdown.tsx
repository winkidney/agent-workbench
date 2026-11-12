import { memo, useMemo, useState } from "react";
import ReactMarkdown, { type Components } from "react-markdown";
import remarkGfm from "remark-gfm";
import { Check, Copy } from "lucide-react";
import {
  PATHLIKE,
  PREVIEWABLE,
  PathChip,
  PathCode,
  RASTER,
  UrlLink,
  pathItem,
  useBase,
  useEnv,
  workspacePath,
} from "./media";
import { useHighlighter } from "./highlight";
import { rehypeBlockWhitespace } from "./whitespace";

export function CopyButton({
  text,
  label = "Copy",
}: {
  text: string;
  label?: string;
}) {
  const [done, setDone] = useState(false);
  return (
    <button
      type="button"
      className="copy-button"
      aria-label={label}
      title={label}
      onClick={() =>
        void navigator.clipboard?.writeText(text).then(() => {
          setDone(true);
          setTimeout(() => setDone(false), 1200);
        })
      }
    >
      {done ? <Check size={12} /> : <Copy size={12} />}
    </button>
  );
}

const nodeText = (children: React.ReactNode): string =>
  Array.isArray(children)
    ? children.map(nodeText).join("")
    : typeof children === "string" || typeof children === "number"
      ? String(children)
      : children && typeof children === "object" && "props" in children
        ? nodeText(
            (children as { props: { children?: React.ReactNode } }).props
              .children,
          )
        : "";

function MarkdownImage({ src, alt }: { src?: string; alt?: string }) {
  const env = useEnv();
  const base = useBase();
  if (!src) return null;
  if (/^(https?:|data:image\/)/i.test(src))
    return (
      <button
        type="button"
        className="media-thumb"
        title="View image"
        onClick={() =>
          env.view([{ kind: "image", src, title: alt || "Image" }], 0)
        }
      >
        <img className="md-image" src={src} alt={alt ?? ""} loading="lazy" />
      </button>
    );
  const rel = workspacePath(src, env.cwd);
  const item = rel && RASTER.test(rel) ? pathItem(env, rel, base) : null;
  if (item)
    return (
      <button
        type="button"
        className="media-thumb"
        title={`View ${rel}`}
        onClick={() => env.view([item], 0)}
      >
        <img src={item.src} alt={alt ?? rel ?? ""} loading="lazy" />
      </button>
    );
  return <PathChip path={src} />;
}

const components: Components = {
  a: ({ href, children }) => <UrlLink href={href ?? ""}>{children}</UrlLink>,
  img: ({ src, alt }) => (
    <MarkdownImage src={typeof src === "string" ? src : undefined} alt={alt} />
  ),
  pre: ({ node, children }) => {
    // Fences that name a language are highlighted; Claude draws the others in
    // its inline-code color, Codex leaves them plain.
    const code = node?.children[0];
    const names =
      code?.type === "element" && Array.isArray(code.properties.className)
        ? (code.properties.className as string[])
        : [];
    const lang = names.find((c) => c.startsWith("language-"))?.slice(9);
    return (
      <div
        className={"md-code" + (lang ? "" : " unlabeled")}
        data-lang={lang || undefined}
      >
        <CopyButton
          text={nodeText(children).replace(/\n$/, "")}
          label="Copy code"
        />
        <pre>{children}</pre>
      </div>
    );
  },
  code: ({ node, className, children }) => {
    const inline =
      node?.position && node.position.start.line === node.position.end.line;
    const value = nodeText(children);
    if (inline && !className && !/\s/.test(value)) {
      if (PREVIEWABLE.test(value)) return <PathChip path={value} />;
      if (PATHLIKE.test(value)) return <PathCode path={value} />;
    }
    return <code className={className}>{children}</code>;
  },
  table: ({ children }) => (
    <div className="md-table">
      <table>{children}</table>
    </div>
  ),
};

export const Markdown = memo(function Markdown({ text }: { text: string }) {
  const highlight = useHighlighter(text);
  const rehypePlugins = useMemo(
    () => [rehypeBlockWhitespace, ...(highlight ?? [])],
    [highlight],
  );
  return (
    <div className="markdown">
      <ReactMarkdown
        remarkPlugins={[remarkGfm]}
        rehypePlugins={rehypePlugins}
        components={components}
        skipHtml
      >
        {text}
      </ReactMarkdown>
    </div>
  );
});
