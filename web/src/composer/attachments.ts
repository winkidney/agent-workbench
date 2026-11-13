import { useEffect, useRef, useState } from "react";
import { checkAccess } from "../lib/api";

// Images and files added to a message: images pasted, dropped or picked;
// other files dropped or picked. Each is uploaded as it is added (an image
// first made fit for the agents); the message then carries only their names.
// Images go to the agents with the message, and the page shows them from
// the workbench (an <img> of its address); a file stays on the workbench
// computer, and the message says where. Nothing here holds their data once
// the upload is done.

// One image an agent takes: Claude's limit is 5 MB of base64 per image, and
// it scales anything larger than about 2000 px down anyway (Claude Code
// itself sends at most 2000 × 2000).
export const MAX_BYTES = (5 * 1024 * 1024 * 3) / 4;
export const MAX_SIDE = 2000;
// Per message: images and files together.
export const MAX_ATTACHMENTS = 10;
export const MAX_FILE_BYTES = 100 * 1024 * 1024;
// What both agents take as they are.
const KEPT = ["image/png", "image/jpeg", "image/webp"];

export type Attachment = {
  key: string;
  name: string;
  kind: "image" | "file";
  state: "uploading" | "ready" | "failed";
  bytes?: number;
  // Once uploaded: its name in the workbench, and where an image is shown
  // from.
  id?: string;
  url?: string;
  error?: string;
};

export const uploadUrl = (id: string) => "/api/uploads/" + id;

type Clipboard = Pick<DataTransfer, "getData" | "files" | "items">;

// The files a drop carries.
export function droppedFiles(data: Clipboard | null): File[] {
  return [...(data?.files ?? [])];
}

// The image files a drop or paste carries. Some browsers list a pasted
// image only among the items.
export function imageFiles(data: Clipboard | null): File[] {
  if (!data) return [];
  const files = [...(data.files ?? [])].filter((f) =>
    f.type.startsWith("image/"),
  );
  if (files.length) return files;
  return [...(data.items ?? [])]
    .filter((i) => i.kind === "file" && i.type.startsWith("image/"))
    .map((i) => i.getAsFile())
    .filter((f): f is File => !!f);
}

// What a paste adds: the clipboard's images (never other files), unless it
// holds text too (a spreadsheet's cells come with a picture of them); then
// the text is pasted, as usual.
export function pastedImages(data: Clipboard | null): File[] {
  if (!data || data.getData("text/plain").trim()) return [];
  return imageFiles(data);
}

// Whether an image must be redrawn before the agents can take it.
export function needsReencode(i: {
  type: string;
  bytes: number;
  width: number;
  height: number;
}): boolean {
  return (
    !KEPT.includes(i.type) ||
    i.bytes > MAX_BYTES ||
    Math.max(i.width, i.height) > MAX_SIDE
  );
}

// The size that fits within max on its long side, keeping the shape.
export function fitted(width: number, height: number, max = MAX_SIDE) {
  const scale = Math.min(1, max / Math.max(width, height));
  return {
    width: Math.max(1, Math.round(width * scale)),
    height: Math.max(1, Math.round(height * scale)),
  };
}

const toBlob = (canvas: HTMLCanvasElement, type: string, quality?: number) =>
  new Promise<Blob>((resolve, reject) =>
    canvas.toBlob(
      (b) => (b ? resolve(b) : reject(new Error("could not encode"))),
      type,
      quality,
    ),
  );

// The image as the agents take it: as it is when it fits, else redrawn
// within 2000 px as PNG, or JPEG when the PNG is still too large. The
// bitmap and canvas are let go at once.
export async function normalize(file: Blob): Promise<Blob> {
  let bitmap: ImageBitmap;
  try {
    bitmap = await createImageBitmap(file);
  } catch {
    throw new Error("This image could not be read");
  }
  const canvas = document.createElement("canvas");
  try {
    const { width, height } = bitmap;
    if (!needsReencode({ type: file.type, bytes: file.size, width, height }))
      return file;
    const size = fitted(width, height);
    canvas.width = size.width;
    canvas.height = size.height;
    const ctx = canvas.getContext("2d")!;
    ctx.drawImage(bitmap, 0, 0, size.width, size.height);
    let out = await toBlob(canvas, "image/png");
    if (out.size > MAX_BYTES) {
      // JPEG has no transparency: what was clear turns white, not black.
      ctx.globalCompositeOperation = "destination-over";
      ctx.fillStyle = "#fff";
      ctx.fillRect(0, 0, size.width, size.height);
      out = await toBlob(canvas, "image/jpeg", 0.9);
    }
    if (out.size > MAX_BYTES) throw new Error("This image is too large");
    return out;
  } finally {
    bitmap.close();
    canvas.width = canvas.height = 0;
  }
}

export async function uploadImage(
  blob: Blob,
  signal?: AbortSignal,
): Promise<{ id: string; url: string }> {
  const r = await fetch("/api/uploads", {
    method: "POST",
    headers: { "Content-Type": blob.type || "application/octet-stream" },
    body: blob,
    signal,
  });
  checkAccess(r, "uploads");
  if (!r.ok) throw new Error((await r.text()).trim() || r.statusText);
  return r.json();
}

// A file of any kind, under its name; the browser sends it from disk.
export async function uploadFile(
  file: File,
  signal?: AbortSignal,
): Promise<{ id: string; name: string; bytes: number }> {
  if (file.size > MAX_FILE_BYTES)
    throw new Error(`A file can be at most ${MAX_FILE_BYTES >> 20} MB`);
  const r = await fetch(
    "/api/uploads/files?name=" + encodeURIComponent(file.name || "file"),
    {
      method: "POST",
      headers: { "Content-Type": "application/octet-stream" },
      body: file,
      signal,
    },
  );
  checkAccess(r, "uploads");
  if (!r.ok) throw new Error((await r.text()).trim() || r.statusText);
  return r.json();
}

// Uploads one added file: an image the agents can see, made fit; anything
// else (or an image the browser cannot read, such as SVG) as a file.
async function send(file: File, signal: AbortSignal) {
  if (file.type.startsWith("image/")) {
    const blob = await normalize(file).catch(() => null);
    if (blob) {
      const { id, url } = await uploadImage(blob, signal);
      return { kind: "image" as const, id, url, bytes: blob.size };
    }
  }
  const { id, bytes } = await uploadFile(file, signal);
  return { kind: "file" as const, id, bytes };
}

// The images of the message being written. Uploads still running when the
// composer goes away are stopped; what was uploaded can be kept as a draft.
export function useAttachments(initial: Attachment[] = []) {
  const [items, setItems] = useState<Attachment[]>(initial);
  const [notice, setNotice] = useState("");
  const live = useRef(new Map<string, AbortController>());
  const seq = useRef(0);
  useEffect(() => {
    const running = live.current;
    return () => {
      running.forEach((c) => c.abort());
      running.clear();
    };
  }, []);
  const update = (key: string, change: Partial<Attachment>) =>
    setItems((list) => {
      const next = list.map((a) => (a.key === key ? { ...a, ...change } : a));
      // The same image twice is kept once.
      const id = change.id;
      return id && next.some((a) => a.key !== key && a.id === id)
        ? next.filter((a) => a.key !== key)
        : next;
    });
  // The list as last rendered, for how many more fit.
  const shown = useRef(items);
  shown.current = items;
  const add = (files: File[]) => {
    const room = Math.max(0, MAX_ATTACHMENTS - shown.current.length);
    setNotice(
      files.length > room
        ? `A message can have at most ${MAX_ATTACHMENTS} images and files`
        : "",
    );
    const added = files.slice(0, room).map((file): Attachment => {
      const key = "a" + ++seq.current;
      const control = new AbortController();
      live.current.set(key, control);
      void send(file, control.signal)
        .then((up) => update(key, { state: "ready", ...up }))
        .catch((e: unknown) => {
          if (control.signal.aborted) return;
          update(key, {
            state: "failed",
            error: e instanceof Error ? e.message : String(e),
          });
        })
        .finally(() => live.current.delete(key));
      return {
        key,
        name: file.name || "Image",
        kind: file.type.startsWith("image/") ? "image" : "file",
        bytes: file.size,
        state: "uploading",
      };
    });
    shown.current = [...shown.current, ...added];
    setItems((list) => [...list, ...added]);
  };
  const remove = (key: string) => {
    live.current.get(key)?.abort();
    live.current.delete(key);
    setNotice("");
    setItems((list) => list.filter((a) => a.key !== key));
  };
  const ready = items.filter((a) => a.state === "ready");
  return {
    items,
    notice,
    add,
    remove,
    // Replaces them all (sent, or put back after a failed send).
    set: (next: Attachment[]) => {
      setNotice("");
      setItems(next);
    },
    images: ready.filter((a) => a.kind === "image"),
    files: ready.filter((a) => a.kind === "file"),
    ready,
    uploading: items.some((a) => a.state === "uploading"),
  };
}
