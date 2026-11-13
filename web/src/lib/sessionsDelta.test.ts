import { describe, expect, it } from "vitest";
import { drifted, mergeSessions, type SessionsDelta } from "./sessionsDelta";
import type { Session } from "../sidebar/tree";

const s = (id: string, updated: string, status = "sleeping"): Session => ({
  id,
  agent: "claude",
  cwd: "/p",
  title: id,
  nativeId: id,
  status,
  updated,
});

describe("the session list by deltas", () => {
  const list = [s("c", "03"), s("b", "02"), s("a", "01")];

  it("takes the whole list as it comes", () => {
    const full: SessionsDelta = {
      token: "t",
      count: 1,
      full: true,
      changed: [s("z", "09")],
    };
    expect(mergeSessions(list, full)).toEqual([s("z", "09")]);
    expect(mergeSessions(list, { token: "t", count: 0, full: true })).toEqual(
      [],
    );
  });

  it("keeps the same array when nothing changed", () => {
    expect(mergeSessions(list, { token: "t", count: 3 })).toBe(list);
  });

  it("replaces, adds and removes rows, newest first", () => {
    const next = mergeSessions(list, {
      token: "u",
      count: 3,
      changed: [s("a", "05", "running"), s("d", "04")],
      removed: ["b"],
    });
    expect(next.map((x) => x.id)).toEqual(["a", "d", "c"]);
    expect(next[0].status).toBe("running");
    // The page's array is not changed in place.
    expect(list.map((x) => x.id)).toEqual(["c", "b", "a"]);
    // Equal times keep their order.
    const tie = mergeSessions([s("x", "01"), s("y", "01")], {
      token: "v",
      count: 3,
      changed: [s("w", "02")],
    });
    expect(tie.map((x) => x.id)).toEqual(["w", "x", "y"]);
  });

  it("notices a copy that drifted from the list", () => {
    expect(drifted(list, { token: "t", count: 3 })).toBe(false);
    // A row the list does not have.
    expect(drifted(list, { token: "t", count: 2 })).toBe(true);
  });
});
