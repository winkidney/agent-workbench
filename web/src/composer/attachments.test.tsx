import { describe, expect, it } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";
import {
  MAX_BYTES,
  fitted,
  imageFiles,
  needsReencode,
  pastedImages,
} from "./attachments";
import { AttachmentStrip } from "./AttachmentStrip";
import { QueueList } from "./QueueList";
import { applyEvents, emptyState, withSending } from "../transcript/reducer";
import { MediaList } from "../transcript/media";
import { splitFiles } from "../transcript/files";
import type { WorkbenchEvent } from "../transcript/model";

const png = (name = "shot.png") => new File(["x"], name, { type: "image/png" });
const clip = (text: string, files: File[], items: File[] = []) => ({
  getData: (type: string) => (type === "text/plain" ? text : ""),
  files: files as unknown as FileList,
  items: items.map((f) => ({
    kind: "file",
    type: f.type,
    getAsFile: () => f,
  })) as unknown as DataTransferItemList,
});
const id = "a".repeat(64) + ".png";

describe("adding images to a message", () => {
  it("pastes images only when the clipboard holds no text", () => {
    expect(pastedImages(clip("", [png()]))).toHaveLength(1);
    // A spreadsheet's cells come with a picture of them: the text wins.
    expect(pastedImages(clip("a\tb\n1\t2", [png()]))).toEqual([]);
    expect(pastedImages(clip("plain", []))).toEqual([]);
    expect(pastedImages(null)).toEqual([]);
    // Some browsers list a pasted image only among the items.
    expect(pastedImages(clip(" ", [], [png()]))).toHaveLength(1);
    // Other files are not images to add.
    const pdf = new File(["%PDF"], "a.pdf", { type: "application/pdf" });
    expect(imageFiles(clip("", [pdf, png()]))).toHaveLength(1);
  });

  it("redraws only what the agents cannot take as it is", () => {
    const fits = {
      type: "image/png",
      bytes: 500_000,
      width: 1920,
      height: 1080,
    };
    expect(needsReencode(fits)).toBe(false);
    expect(needsReencode({ ...fits, type: "image/jpeg" })).toBe(false);
    expect(needsReencode({ ...fits, type: "image/webp" })).toBe(false);
    expect(needsReencode({ ...fits, type: "image/gif" })).toBe(true);
    expect(needsReencode({ ...fits, type: "image/heic" })).toBe(true);
    expect(needsReencode({ ...fits, bytes: MAX_BYTES + 1 })).toBe(true);
    expect(needsReencode({ ...fits, width: 4000, height: 3000 })).toBe(true);
    expect(needsReencode({ ...fits, width: 900, height: 2400 })).toBe(true);
    expect(fitted(4000, 3000)).toEqual({ width: 2000, height: 1500 });
    expect(fitted(900, 2400)).toEqual({ width: 750, height: 2000 });
    expect(fitted(800, 600)).toEqual({ width: 800, height: 600 });
    expect(fitted(10000, 1)).toEqual({ width: 2000, height: 1 });
  });

  it("shows added images and files, their progress and what went wrong", () => {
    const html = renderToStaticMarkup(
      <AttachmentStrip
        items={[
          {
            key: "1",
            name: "shot.png",
            kind: "image",
            state: "ready",
            id,
            url: "/api/uploads/" + id,
          },
          { key: "2", name: "big.png", kind: "image", state: "uploading" },
          {
            key: "3",
            name: "huge.iso",
            kind: "file",
            state: "failed",
            error: "A file can be at most 100 MB",
          },
          {
            key: "4",
            name: "report.pdf",
            kind: "file",
            state: "ready",
            bytes: 245_760,
            id: "b".repeat(64) + "/report.pdf",
          },
        ]}
        onRemove={() => {}}
      />,
    );
    expect(html).toContain(`src="/api/uploads/${id}"`);
    expect(html).toContain('aria-label="Adding"');
    expect(html).toContain("A file can be at most 100 MB");
    // A file is shown by its name and size, never loaded.
    expect(html).toContain("report.pdf");
    expect(html).toContain("240 KB");
    expect(html.match(/<img/g)).toHaveLength(1);
    expect(html.match(/aria-label="Remove /g)).toHaveLength(4);
    expect(
      renderToStaticMarkup(<AttachmentStrip items={[]} onRemove={() => {}} />),
    ).toBe("");
  });

  it("shows queued messages' images", () => {
    const html = renderToStaticMarkup(
      <QueueList
        items={[{ id: "q", text: "after", images: [id] }]}
        paused={false}
        running
        onRemove={() => {}}
        onResume={() => {}}
        onSteer={() => {}}
      />,
    );
    expect(html).toContain(`src="/api/uploads/${id}"`);
  });
});

describe("sent images in the transcript", () => {
  const ev = (kind: string, data: Record<string, unknown>): WorkbenchEvent => ({
    seq: 1,
    session: "s",
    kind,
    data,
    time: new Date(0).toISOString(),
  });

  it("shows them with the message, at once and as echoed", () => {
    const sending = withSending(emptyState(), "", "sending:1", [
      "/api/uploads/" + id,
    ]);
    expect(sending.blocks.get("sending:1")!.media).toEqual([
      { kind: "url", url: "/api/uploads/" + id },
    ]);
    const echoed = applyEvents(
      emptyState(),
      [ev("user", { text: "what is this?", images: ["/api/uploads/" + id] })],
      { agent: "claude", history: false },
    );
    const b = echoed.blocks.get(echoed.keys[0])!;
    expect(b.text).toBe("what is this?");
    expect(b.media).toEqual([{ kind: "url", url: "/api/uploads/" + id }]);
  });

  it("finds a large image again by its name, or says it was left out", () => {
    const codex = applyEvents(
      emptyState(),
      [
        ev("native", {
          method: "item/completed",
          params: {
            item: {
              id: "u",
              type: "userMessage",
              content: [
                {
                  type: "image",
                  omitted: true,
                  bytes: 900_000,
                  media_type: "image/png",
                  upload: id,
                },
                { type: "text", text: "look" },
              ],
            },
          },
        }),
      ],
      { agent: "codex", history: true },
    );
    const media = codex.blocks.get("u")!.media!;
    expect(media).toEqual([{ kind: "omitted", bytes: 900_000, upload: id }]);
    const kept = renderToStaticMarkup(<MediaList media={media} />);
    expect(kept).toContain(`src="/api/uploads/${id}"`);
    const gone = renderToStaticMarkup(
      <MediaList media={[{ kind: "omitted", bytes: 2048 }]} />,
    );
    expect(gone).toContain("Image omitted (2 KB)");
    // Only the workbench's own names become addresses.
    const odd = renderToStaticMarkup(
      <MediaList
        media={[
          { kind: "omitted", upload: "../../secret.png" },
          { kind: "url", url: "/api/settings/secrets" },
        ]}
      />,
    );
    expect(odd).not.toContain("<img");
    // An inline image is not named by its data.
    const inline = renderToStaticMarkup(
      <MediaList
        media={[{ kind: "url", url: "data:image/png;base64,AAAA" }]}
      />,
    );
    expect(inline).toContain('alt="Image"');
    expect(inline).not.toContain('title="View data:');
  });
});

describe("files sent with a message", () => {
  it("reads the lines that say where they are", () => {
    expect(
      splitFiles(
        "read these\n\n[File (2 KB): /k/files/ab/a.pdf]\n[File (3.0 MB): C:\\k\\b c.zip]",
      ),
    ).toEqual({
      text: "read these",
      files: [
        { name: "a.pdf", size: "2 KB", path: "/k/files/ab/a.pdf" },
        { name: "b c.zip", size: "3.0 MB", path: "C:\\k\\b c.zip" },
      ],
    });
    expect(splitFiles("[File (1 B): /x/y]")).toEqual({
      text: "",
      files: [{ name: "y", size: "1 B", path: "/x/y" }],
    });
    // Only the lines that end the message.
    const quoted = "[File (1 B): /x/y]\nwhat is this line?";
    expect(splitFiles(quoted)).toEqual({ text: quoted, files: [] });
  });
});
