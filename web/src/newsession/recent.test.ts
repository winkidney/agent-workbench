import { describe, expect, it } from "vitest";
import { applyOp, emptyPrefs, type Session } from "../sidebar/tree";
import {
  completionTarget,
  filterRecent,
  isPathInput,
  recentProjects,
  tildePath,
} from "./recent";

const s = (
  id: string,
  ws: string,
  updated: string,
  extra: Partial<Session> = {},
): Session => ({
  id,
  agent: "codex",
  cwd: ws,
  workspace: ws,
  title: id,
  nativeId: id,
  status: "sleeping",
  updated,
  ...extra,
});

describe("recent projects", () => {
  const sessions = [
    s("a", "/w/app", "2026-11-01T00:00:00Z"),
    s("b", "/w/app", "2026-11-09T00:00:00Z", { title: "Latest app task" }),
    s("c", "/w/lib", "2026-11-05T00:00:00Z"),
    s("d", "/tmp/run", "2026-11-10T00:00:00Z", { origin: "temporary" }),
    s("e", "/w/app/sub", "2026-11-08T00:00:00Z"),
  ];
  it("orders by activity, keeps pins first and skips non-project origins", () => {
    const list = recentProjects(sessions, emptyPrefs(), ["/w/registered"]);
    expect(list.map((p) => p.path)).toEqual([
      "/w/app",
      "/w/app/sub",
      "/w/lib",
      "/w/registered",
    ]);
    expect(list[0]).toMatchObject({
      latest: "2026-11-09T00:00:00Z",
      sample: "Latest app task",
    });
    const pinned = recentProjects(
      sessions,
      applyOp(emptyPrefs(), "pinWorkspace", "/w/lib"),
    );
    expect(pinned[0]).toMatchObject({ path: "/w/lib", pinned: true });
  });
  it("filters by name and path", () => {
    const list = recentProjects(sessions, emptyPrefs());
    expect(filterRecent(list, "sub").map((p) => p.path)).toEqual([
      "/w/app/sub",
    ]);
    expect(filterRecent(list, "w lib").map((p) => p.path)).toEqual(["/w/lib"]);
  });
});

describe("path input", () => {
  it("detects typed paths and splits them for completion", () => {
    expect(isPathInput("~/work")).toBe(true);
    expect(isPathInput("/home")).toBe(true);
    expect(isPathInput("api-server")).toBe(false);
    expect(completionTarget("~/work/ag")).toEqual({
      dir: "~/work",
      prefix: "ag",
    });
    expect(completionTarget("~")).toEqual({ dir: "~", prefix: "" });
    expect(completionTarget("~/")).toEqual({ dir: "~", prefix: "" });
    expect(completionTarget("/home/")).toEqual({ dir: "/home", prefix: "" });
    expect(completionTarget("/ho")).toEqual({ dir: "/", prefix: "ho" });
    expect(tildePath("/home/u/work", "/home/u")).toBe("~/work");
    expect(tildePath("/opt/x", "/home/u")).toBe("/opt/x");
  });
});
