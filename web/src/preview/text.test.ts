import { describe, expect, it } from "vitest";
import {
  asLines,
  eolOf,
  indentUnit,
  lineCol,
  shiftLines,
  stampBytes,
  toDisk,
} from "./text";

// Applies a shiftLines result the way the editor does.
const shift = (
  value: string,
  start: number,
  end: number,
  unit: string,
  out = false,
) => {
  const r = shiftLines(value, start, end, unit, out);
  return {
    value: value.slice(0, r.from) + r.text + value.slice(r.to),
    start: r.start,
    end: r.end,
  };
};

describe("editing a text file", () => {
  it("puts the file's own line ends back", () => {
    const crlf = "\ufeffa\r\nb\r\n";
    expect(eolOf(crlf)).toBe("\r\n");
    expect(eolOf("a\nb\n")).toBe("\n");
    expect(eolOf("one line")).toBe("\n");
    // Mostly CRLF: CRLF.
    expect(eolOf("a\r\nb\r\nc\n")).toBe("\r\n");
    const lines = asLines(crlf);
    expect(lines).toBe("\ufeffa\nb\n");
    // The byte order mark stays.
    expect(toDisk(lines, eolOf(crlf))).toBe(crlf);
    expect(toDisk("a\nb", "\n")).toBe("a\nb");
    expect(asLines("old\rmac")).toBe("old\nmac");
  });

  it("indents as the file does", () => {
    expect(indentUnit("func f() {\n\treturn\n}\n")).toBe("\t");
    expect(indentUnit("def f():\n    if x:\n        pass\n")).toBe("    ");
    expect(indentUnit("{\n  a: {\n    b: 1\n  }\n}")).toBe("  ");
    expect(indentUnit("plain prose\nno indent")).toBe("  ");
    // A doc comment's single space is not the indentation.
    expect(indentUnit("/**\n * doc\n */\nf()\n")).toBe("  ");
    expect(indentUnit("a\n            b\n")).toBe(" ".repeat(8));
  });

  it("indents and outdents the lines a selection touches", () => {
    const text = "one\ntwo\nthree";
    // Lines one and two (the selection ends inside two).
    let r = shift(text, 1, 5, "  ");
    expect(r.value).toBe("  one\n  two\nthree");
    expect([r.start, r.end]).toEqual([3, 9]);
    // A selection ending at the start of a line leaves that line alone.
    r = shift(text, 0, 4, "  ");
    expect(r.value).toBe("  one\ntwo\nthree");
    // Empty lines stay empty.
    expect(shift("a\n\nb", 0, 4, "\t").value).toBe("\ta\n\n\tb");
    // Out: a unit, or what there is of one.
    r = shift("    a\n  b\nc", 0, 10, "    ", true);
    expect(r.value).toBe("a\nb\nc");
    expect(shift("\t\ta", 2, 2, "\t", true).value).toBe("\ta");
    // The caret moves with its line, never before it.
    r = shift("x\n    abc", 8, 8, "    ", true);
    expect(r.value).toBe("x\nabc");
    expect([r.start, r.end]).toEqual([4, 4]);
    r = shift("x\n  abc", 2, 2, "  ", true);
    expect([r.start, r.end]).toEqual([2, 2]);
  });

  it("says where the caret is", () => {
    expect(lineCol("", 0)).toEqual({ line: 1, col: 1 });
    expect(lineCol("ab\ncd", 2)).toEqual({ line: 1, col: 3 });
    expect(lineCol("ab\ncd", 3)).toEqual({ line: 2, col: 1 });
    expect(lineCol("ab\ncd\n", 6)).toEqual({ line: 3, col: 1 });
    expect(stampBytes("2048:1700000000")).toBe(2048);
    expect(stampBytes("")).toBe(0);
  });
});
