import { describe, expect, it } from "vitest";
import {
  EMPTY,
  addMark,
  box,
  exportName,
  foreign,
  penPath,
  redo,
  toImage,
  undo,
  type Mark,
} from "./markup";

const rect = (x: number): Mark => ({
  kind: "rect",
  x,
  y: 0,
  w: 1,
  h: 1,
  color: "#ef4444",
  width: 2,
});

describe("markup history", () => {
  it("undoes and redoes in order", () => {
    let h = addMark(addMark(EMPTY, rect(1)), rect(2));
    h = undo(h);
    expect(h.done).toEqual([rect(1)]);
    h = undo(h);
    expect(h.done).toEqual([]);
    expect(undo(h)).toBe(h);
    h = redo(h);
    expect(h.done).toEqual([rect(1)]);
    h = redo(h);
    expect(h.done).toEqual([rect(1), rect(2)]);
    expect(redo(h)).toBe(h);
  });

  it("drops what was undone once something new is drawn", () => {
    const h = addMark(undo(addMark(EMPTY, rect(1))), rect(3));
    expect(h).toEqual({ done: [rect(3)], undone: [] });
  });
});

describe("markup geometry", () => {
  it("maps stage points to image pixels under zoom and pan", () => {
    expect(toImage({ s: 2, x: 10, y: 20 }, 30, 60)).toEqual({ x: 10, y: 20 });
    expect(toImage({ s: 0.5, x: -5, y: 0 }, 0, 10)).toEqual({ x: 10, y: 20 });
  });

  it("builds a box whichever way it was dragged", () => {
    expect(box(10, 10, 4, 2)).toEqual({ x: 4, y: 2, w: 6, h: 8 });
  });

  it("draws a stroke through its points", () => {
    expect(penPath([1, 2, 3, 4, 5, 6])).toBe("M1 2 L3 4 L5 6");
  });
});

describe("what can be marked up", () => {
  const origin = "https://aw.example";
  it("allows the workbench's own images and inline data", () => {
    expect(foreign("/api/media?path=a.png", origin)).toBe(false);
    expect(foreign("/api/uploads/ab.png", origin)).toBe(false);
    expect(foreign("data:image/png;base64,AA", origin)).toBe(false);
    expect(foreign("blob:https://aw.example/1", origin)).toBe(false);
  });
  it("refuses images from other sites", () => {
    expect(foreign("https://cdn.example/a.png", origin)).toBe(true);
  });
});

describe("export names", () => {
  it("names the copy after the image", () => {
    expect(exportName("shots/out.jpg", true)).toBe("out-marked.png");
    expect(exportName("Image", false)).toBe("image.png");
  });
});
