import { describe, expect, it } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";
import { LIST_SHOWN, LIST_STEP, ShowMoreLess, limited } from "./limit";
import { holdsNode, keepsNode } from "./Sidebar";
import type { Node } from "./tree";

const node = (id: string, children: string[] = []) =>
  ({
    session: { id },
    children: children.map((c) => ({ session: { id: c }, children: [] })),
  }) as unknown as Node;

describe("a folded session list in the sidebar", () => {
  const nodes = Array.from({ length: 12 }, (_, i) =>
    node(`s${i}`, i === 9 ? ["child"] : []),
  );
  const ids = (list: Node[]) => list.map((n) => n.session.id);
  const holding = (id: string) => (n: Node) => holdsNode(n, id);

  it("shows the newest five, then fifteen more at a time", () => {
    expect([LIST_SHOWN, LIST_STEP]).toEqual([5, 15]);
    expect(ids(limited(nodes, LIST_SHOWN))).toEqual([
      "s0",
      "s1",
      "s2",
      "s3",
      "s4",
    ]);
    expect(limited(nodes.slice(0, 3), LIST_SHOWN)).toHaveLength(3);
    expect(limited(nodes, LIST_SHOWN + LIST_STEP)).toHaveLength(12);
  });

  it("keeps the open session listed past the limit", () => {
    expect(ids(limited(nodes, 5, holding("s7")))).toEqual([
      "s0",
      "s1",
      "s2",
      "s3",
      "s4",
      "s7",
    ]);
    // A subsession's parent stands for it.
    expect(ids(limited(nodes, 5, holding("child"))).at(-1)).toBe("s9");
    expect(limited(nodes, 5, holding("s2"))).toHaveLength(5);
  });

  it("keeps every session that needs the user listed past the limit", () => {
    const waiting = nodes.map((n, i) =>
      i === 8 || i === 11
        ? { ...n, session: { ...n.session, attention: "question" } }
        : n,
    ) as Node[];
    expect(ids(limited(waiting, 5, (n) => keepsNode(n, "s7")))).toEqual([
      "s0",
      "s1",
      "s2",
      "s3",
      "s4",
      "s7",
      "s8",
      "s11",
    ]);
    // A subsession waiting keeps its parent listed.
    const child = nodes.map((n, i) =>
      i === 9
        ? {
            ...n,
            children: [
              { session: { id: "child", attention: "approval" }, children: [] },
            ],
          }
        : n,
    ) as unknown as Node[];
    expect(ids(limited(child, 5, (n) => keepsNode(n, null)))).toEqual([
      "s0",
      "s1",
      "s2",
      "s3",
      "s4",
      "s9",
    ]);
  });

  it("shows every item while searching", () => {
    expect(limited(nodes, 5, undefined, true)).toHaveLength(12);
  });

  const buttons = (total: number, limit: number, all = false) =>
    renderToStaticMarkup(
      <ShowMoreLess
        total={total}
        limit={limit}
        setLimit={() => {}}
        all={all}
      />,
    );

  it("counts only the rows not shown yet: kept rows are already there", () => {
    const more = (total: number, shown: number) =>
      renderToStaticMarkup(
        <ShowMoreLess
          total={total}
          limit={5}
          shown={shown}
          setLimit={() => {}}
        />,
      );
    // Five newest plus two kept past the limit, of nine.
    expect(more(9, 7)).toContain("Show 2 more");
    // Every row already shown: nothing more to offer.
    expect(more(7, 7)).not.toContain("more");
  });

  it("offers Show more and Show less as the list needs", () => {
    expect(buttons(12, 5)).toContain("Show 7 more");
    expect(buttons(12, 5)).not.toContain("Show less");
    expect(buttons(40, 5)).toContain("Show 15 more");
    expect(buttons(40, 20)).toContain("Show 15 more");
    expect(buttons(40, 20)).toContain("Show less");
    expect(buttons(12, 20)).not.toContain(" more<");
    expect(buttons(12, 20)).toContain("Show less");
    expect(buttons(5, 5)).toBe("");
    expect(buttons(40, 5, true)).toBe("");
  });

  it("lines its buttons up with the rows they follow", () => {
    expect(buttons(12, 5)).toContain("padding-left:36px");
    expect(
      renderToStaticMarkup(
        <ShowMoreLess total={12} limit={5} setLimit={() => {}} depth={3} />,
      ),
    ).toContain("padding-left:60px");
  });
});
