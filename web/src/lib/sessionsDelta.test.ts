import { describe, expect, it } from "vitest";
import {
  drifted,
  mergeSessions,
  type SessionsDelta,
  namesUnknownSession,
  withStatus,
} from "./sessionsDelta";
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

describe("namesUnknownSession", () => {
  const has = (id: string) => id === "claude:known";
  it("asks for the list when a run names a session the page lacks", () => {
    expect(
      namesUnknownSession("routine", { run: { session: "claude:new" } }, has),
    ).toBe(true);
  });
  it("not for a session it has, a run with none, or other events", () => {
    expect(
      namesUnknownSession("routine", { run: { session: "claude:known" } }, has),
    ).toBe(false);
    expect(namesUnknownSession("routine", { run: {} }, has)).toBe(false);
    expect(namesUnknownSession("routine", {}, has)).toBe(false);
    // An assistant's change names the conversation it read, not a new one.
    expect(
      namesUnknownSession("assistant", { session: "claude:new" }, has),
    ).toBe(false);
    expect(namesUnknownSession("prefs", { session: "claude:new" }, has)).toBe(
      false,
    );
  });
});

describe("withStatus", () => {
  const row = (
    id: string,
    updated: string,
    over: Record<string, unknown> = {},
  ) =>
    ({
      id,
      agent: "claude",
      nativeId: id.slice(7),
      updated,
      ...over,
    }) as unknown as Session;
  const now = "2026-11-28T05:00:00Z";
  it("puts a session the stream announces under its native id first, timed now when new", () => {
    const list = [row("claude:a", "2026-11-28T04:00:00Z")];
    const next = withStatus(list, row("claude:new", ""), now);
    expect(next.map((s) => s.id)).toEqual(["claude:new", "claude:a"]);
    // Without a time it would sort last and fold away.
    expect(next[0].updated).toBe(now);
  });
  it("keeps the time a known session had when the event names none", () => {
    const list = [row("claude:a", "2026-11-28T04:00:00Z", { status: "idle" })];
    const next = withStatus(
      list,
      row("claude:a", "", { status: "running" }),
      now,
    );
    expect(next[0]).toMatchObject({
      status: "running",
      updated: "2026-11-28T04:00:00Z",
    });
  });
  it("never moves a row's time back to an older one the event carries", () => {
    const list = [row("claude:a", "2026-11-28T04:30:00Z")];
    const next = withStatus(
      list,
      row("claude:a", "2026-11-28T03:00:00Z", { status: "idle" }),
      now,
    );
    expect(next[0]).toMatchObject({
      status: "idle",
      updated: "2026-11-28T04:30:00Z",
    });
    // A newer one wins.
    expect(
      withStatus(list, row("claude:a", "2026-11-28T04:45:00Z"), now)[0].updated,
    ).toBe("2026-11-28T04:45:00Z");
  });
  it("ignores a session under its temporary id", () => {
    const list = [row("claude:a", "t")];
    const temp = {
      id: "tok123",
      agent: "claude",
      updated: "",
    } as unknown as Session;
    expect(withStatus(list, temp, now)).toBe(list);
  });
});
