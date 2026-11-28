import { describe, expect, it } from "vitest";
import { assistantRows, waitingFor } from "./waitingFor";
import type { Node } from "./tree";

const node = (id: string, attention?: string, children: Node[] = []) =>
  ({ session: { id, title: id, attention }, children }) as unknown as Node;

describe("what waits for the user at an assistant", () => {
  it("counts its sessions that need the user and its runs waiting", () => {
    const w = waitingFor(
      [
        node("a", "question"),
        node("b"),
        node("c", undefined, [node("c1", "approval")]),
      ],
      2,
    );
    expect(w.count).toBe(4);
    expect(w.title).toBe(
      "Waiting for you: 1 question, 1 approval, 2 routine runs",
    );
  });
  it("is nothing when nothing waits", () => {
    expect(waitingFor([node("a"), node("b")], 0)).toEqual({
      count: 0,
      title: "",
    });
  });
  it("names one run and a plan", () => {
    expect(waitingFor([node("p", "plan")], 1).title).toBe(
      "Waiting for you: 1 plan, 1 routine run",
    );
  });
});

describe("the Assistants heading and its rows", () => {
  const at = (cwd: string, id: string, attention?: string) =>
    ({ session: { id, cwd, attention }, children: [] }) as unknown as Node;
  const assistants = [
    { dir: "mail", path: "/a/mail" },
    { dir: "old", path: "/a/old", enabled: false },
    { dir: "idle", path: "/a/idle", enabled: false },
  ];
  const nodes = [
    at("/a/mail", "m1", "question"),
    at("/a/old", "o1", "approval"),
    // In the assistants folder but no assistant's own: no row, not counted.
    at("/a", "root", "question"),
  ];
  it("counts what the listed rows' bells add up to", () => {
    const v = assistantRows(assistants, nodes, { mail: { count: 1 } }, false);
    expect(v.listed.map((r) => [r.assistant.dir, r.wait.count])).toEqual([
      ["mail", 2],
      ["old", 1],
    ]);
    expect(v.waiting).toBe(3);
    expect(v.folded).toBe(1);
  });
  it("lists every assistant once shown", () => {
    const v = assistantRows(assistants, nodes, {}, true);
    expect(v.listed).toHaveLength(3);
    expect(v.folded).toBe(0);
  });
});
