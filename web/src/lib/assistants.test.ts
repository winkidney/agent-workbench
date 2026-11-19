import { describe, expect, it } from "vitest";
import {
  assistantOf,
  changeText,
  folderFor,
  latestFor,
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
