import { describe, expect, it } from "vitest";
import {
  assistantOf,
  changeText,
  folderFor,
  latestFor,
  MAIN_NAME,
  MAIN_STARTERS,
  SET_UP_EVENT,
  setUpAssistants,
  type Assistant,
  type AssistantChange,
} from "./assistants";

describe("assistants", () => {
  it("names a folder as the server does", () => {
    expect(folderFor("Weekly Report!")).toBe("weekly-report");
    expect(folderFor("周报")).toBe("");
  });
  it("finds a session's assistant by the folder it started in", () => {
    const a = { dir: "main", path: "/a/main" } as Assistant;
    expect(assistantOf([a], "/a/main")).toBe(a);
    expect(assistantOf([a], "/a/main/sub")).toBeUndefined();
  });
});

describe("changes", () => {
  const base = {
    id: "c",
    assistant: "main",
    kind: "ingest",
    title: "Tea",
    started: "2026-11-19T09:00:00Z",
  } as const;
  it("says what a change did", () => {
    expect(changeText({ ...base, status: "running" })).toBe("Updating memory…");
    expect(
      changeText({ ...base, status: "done", files: ["a"], summary: "tea" }),
    ).toBe("Memory updated: tea");
    expect(changeText({ ...base, status: "done", files: [] })).toBe(
      "Nothing new to remember",
    );
    expect(changeText({ ...base, status: "failed", error: "exit 1" })).toBe(
      "Memory update failed: exit 1",
    );
    expect(
      changeText({ ...base, kind: "routine", title: "Mail", status: "undone" }),
    ).toBe("Undone");
  });
  it("finds a conversation's latest ingest", () => {
    const list: AssistantChange[] = [
      { ...base, id: "r", kind: "routine", status: "done", session: "s" },
      { ...base, id: "new", status: "done", session: "s" },
      { ...base, id: "old", status: "done", session: "s" },
    ];
    expect(latestFor(list, "s")?.id).toBe("new");
    expect(latestFor(list, "x")).toBeUndefined();
  });
});

describe("setting up", () => {
  it("asks the page to show the set-up dialog", () => {
    // Tests run without a DOM: an EventTarget stands in for the window.
    const g = globalThis as unknown as { window?: EventTarget };
    const before = g.window;
    g.window = new EventTarget();
    let asked = 0;
    g.window.addEventListener(SET_UP_EVENT, () => asked++);
    setUpAssistants();
    g.window = before;
    expect(asked).toBe(1);
    expect(MAIN_NAME).toBe("Moss");
  });
});

describe("quick start", () => {
  it("offers short, distinct starts for the personal assistant", () => {
    const labels = MAIN_STARTERS.map((s) => s.label);
    expect(new Set(labels).size).toBe(labels.length);
    for (const s of MAIN_STARTERS) {
      expect(s.label.length).toBeLessThanOrEqual(20);
      expect(!!s.action || s.text.trim() !== "").toBe(true);
    }
    expect(
      MAIN_STARTERS.find((s) => s.label === "Schedule a routine")?.action,
    ).toBe("routine");
    expect(labels).toContain("New assistant");
  });
});
