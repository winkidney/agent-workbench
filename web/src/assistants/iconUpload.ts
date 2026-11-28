// An uploaded icon (ADR 0030): what the user picks, cut to the square in
// its middle and scaled to SIDE pixels, as a PNG. The server takes exactly
// that, so every assistant's image shows at the same size and shape.
export const ICON_SIDE = 128;
export const ICON_TYPES = "image/png,image/jpeg,image/webp,image/gif";

// The square in the middle of a w×h picture: its corner and side.
export function centreSquare(w: number, h: number) {
  const side = Math.min(w, h);
  return {
    x: Math.floor((w - side) / 2),
    y: Math.floor((h - side) / 2),
    side,
  };
}

export async function squareIcon(file: Blob): Promise<Blob> {
  if (!ICON_TYPES.split(",").includes(file.type))
    throw new Error("Choose a PNG, JPEG, WebP or GIF image.");
  const bitmap = await createImageBitmap(file);
  const { x, y, side } = centreSquare(bitmap.width, bitmap.height);
  const canvas = document.createElement("canvas");
  canvas.width = canvas.height = ICON_SIDE;
  const ctx = canvas.getContext("2d");
  if (!ctx) throw new Error("This browser cannot resize images.");
  ctx.imageSmoothingQuality = "high";
  ctx.drawImage(bitmap, x, y, side, side, 0, 0, ICON_SIDE, ICON_SIDE);
  bitmap.close();
  return new Promise((resolve, reject) =>
    canvas.toBlob(
      (b) => (b ? resolve(b) : reject(new Error("Could not make the icon."))),
      "image/png",
    ),
  );
}

// Sends it as the assistant's icon; the server answers with the assistant.
export async function uploadIcon<T>(dir: string, png: Blob): Promise<T> {
  const r = await fetch(`/api/assistants/${encodeURIComponent(dir)}/icon`, {
    method: "PUT",
    headers: { "Content-Type": "image/png" },
    body: png,
  });
  if (!r.ok) throw new Error(await r.text());
  return r.json();
}
