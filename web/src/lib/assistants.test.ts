import { describe, expect, it } from "vitest";
import { assistantOf, folderFor, type Assistant } from "./assistants";

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
