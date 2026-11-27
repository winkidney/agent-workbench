import { describe, expect, it } from "vitest";
import { ASSISTANT_SHOWN, visibleNodes } from "./Sidebar";
import type { Node } from "./tree";

const node = (id: string, children: string[] = []) =>
  ({
    session: { id },
    children: children.map((c) => ({ session: { id: c }, children: [] })),
  }) as unknown as Node;

describe("an assistant's sessions in the sidebar", () => {
  const nodes = Array.from({ length: 12 }, (_, i) =>
    node(`s${i}`, i === 9 ? ["child"] : []),
  );
  const ids = (list: Node[]) => list.map((n) => n.session.id);

  it("lists the newest five at first, as many as there are", () => {
    expect(ASSISTANT_SHOWN).toBe(5);
    expect(ids(visibleNodes(nodes, ASSISTANT_SHOWN))).toEqual([
      "s0",
      "s1",
      "s2",
      "s3",
      "s4",
    ]);
    expect(ids(visibleNodes(nodes.slice(0, 3), ASSISTANT_SHOWN))).toEqual([
      "s0",
      "s1",
      "s2",
    ]);
    expect(visibleNodes(nodes, 20)).toHaveLength(12);
  });

  it("keeps the open session listed past the limit", () => {
    expect(ids(visibleNodes(nodes, 5, "s7"))).toEqual([
      "s0",
      "s1",
      "s2",
      "s3",
      "s4",
      "s7",
    ]);
    // A subsession's parent stands for it.
    expect(ids(visibleNodes(nodes, 5, "child")).at(-1)).toBe("s9");
    expect(visibleNodes(nodes, 5, "s2")).toHaveLength(5);
  });
});
