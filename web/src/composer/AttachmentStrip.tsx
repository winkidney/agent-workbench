import { useRef, useState } from "react";
import { FileText, Loader2, Paperclip, TriangleAlert, X } from "lucide-react";
import { droppedFiles, type Attachment } from "./attachments";
import type { ViewItem } from "../transcript/media";

export const sizeText = (n: number) =>
  n < 1024
    ? `${n} B`
    : n < 1024 * 1024
      ? `${Math.round(n / 1024)} KB`
      : `${(n / 1048576).toFixed(1)} MB`;

// The images and files added to the message being written, above its text:
// an image opens in the viewer; each can be taken out.
export function AttachmentStrip({
  items,
  notice,
  onRemove,
  onView,
}: {
  items: Attachment[];
  notice?: string;
  onRemove: (key: string) => void;
  onView?: (items: ViewItem[], index: number) => void;
}) {
  const failed = items.find((a) => a.state === "failed");
  const message = notice || failed?.error;
  if (!items.length && !message) return null;
  const ready = items.filter((a) => a.state === "ready" && a.kind === "image");
  const gallery: ViewItem[] = ready.map((a) => ({
    kind: "image",
    src: a.url!,
    title: a.name,
  }));
  return (
    <div className="attachments" aria-label="Images in this message">
      {items.map((a) => (
        <div
          key={a.key}
          className={`attachment ${a.kind} ${a.state}`}
          title={a.state === "failed" ? `${a.name}: ${a.error}` : a.name}
        >
          {a.kind === "file" ? (
            <span className="attachment-file">
              {a.state === "uploading" ? (
                <Loader2 size={15} className="spin" aria-label="Adding" />
              ) : a.state === "failed" ? (
                <TriangleAlert size={15} aria-label="Not added" />
              ) : (
                <FileText size={15} />
              )}
              <span className="attachment-name">
                <span>{a.name}</span>
                {a.bytes !== undefined && <small>{sizeText(a.bytes)}</small>}
              </span>
            </span>
          ) : a.state === "ready" ? (
            <button
              type="button"
              className="attachment-view"
              aria-label={`View ${a.name}`}
              onClick={() => onView?.(gallery, ready.indexOf(a))}
            >
              <img src={a.url} alt={a.name} />
            </button>
          ) : (
            <span className="attachment-state">
              {a.state === "uploading" ? (
                <Loader2 size={15} className="spin" aria-label="Adding" />
              ) : (
                <TriangleAlert size={15} aria-label="Not added" />
              )}
            </span>
          )}
          <button
            type="button"
            className="attachment-remove"
            aria-label={`Remove ${a.name}`}
            title="Remove"
            onClick={() => onRemove(a.key)}
          >
            <X size={10} strokeWidth={2.5} />
          </button>
        </div>
      ))}
      {message && (
        <span className="attachments-notice" role="status">
          {message}
        </span>
      )}
    </div>
  );
}

// Adds images and files from the device: on a phone, also the photo
// library or camera.
export function AddFiles({
  disabled,
  onFiles,
}: {
  disabled?: boolean;
  onFiles: (files: File[]) => void;
}) {
  const input = useRef<HTMLInputElement>(null);
  return (
    <>
      <button
        type="button"
        className="composer-attach"
        aria-label="Attach images or files"
        title="Attach images or files · or paste images, or drop files here"
        disabled={disabled}
        onClick={() => input.current?.click()}
      >
        <Paperclip size={15} />
      </button>
      <input
        ref={input}
        type="file"
        multiple
        hidden
        onChange={(e) => {
          onFiles([...(e.target.files ?? [])]);
          e.target.value = "";
        }}
      />
    </>
  );
}

// Dropping images or files on the composer adds them; it shows it takes
// them.
export function useFileDrop(enabled: boolean, add: (files: File[]) => void) {
  const [over, setOver] = useState(false);
  const files = (e: React.DragEvent) =>
    enabled && [...e.dataTransfer.types].includes("Files");
  return {
    over,
    handlers: {
      onDragOver: (e: React.DragEvent) => {
        if (!files(e)) return;
        e.preventDefault();
        e.dataTransfer.dropEffect = "copy";
        setOver(true);
      },
      onDragLeave: (e: React.DragEvent) => {
        if (!e.currentTarget.contains(e.relatedTarget as Node | null))
          setOver(false);
      },
      onDrop: (e: React.DragEvent) => {
        if (!files(e)) return;
        e.preventDefault();
        setOver(false);
        add(droppedFiles(e.dataTransfer));
      },
    },
  };
}
