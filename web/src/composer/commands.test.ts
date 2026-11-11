import { describe, expect, it } from "vitest";
import { complete, items, route, trigger, type Catalog } from "./commands";

const catalog: Catalog = {
  commands: [
    {
      name: "plan",
      argumentHint: "[task]",
      kind: "command",
      source: "workbench",
    },
    { name: "status", kind: "command", source: "workbench", action: "status" },
    {
      name: "compact",
      kind: "command",
      source: "native",
      description: "Free up context",
    },
    {
      name: "clear",
      aliases: ["reset", "new"],
      kind: "command",
      source: "native",
    },
    { name: "anthropic-skills:pdf", kind: "skill", source: "native" },
    { name: "context", kind: "command", source: "native" },
  ],
  skills: [
    { name: "dataviz", description: "Charts" },
    { name: "anthropic-skills:pdf" },
  ],
};

describe("trigger", () => {
  it("opens / only while the first word is typed", () => {
    expect(trigger("/co", 3, "claude")).toEqual({
      kind: "/",
      query: "co",
      start: 0,
      end: 3,
    });
    expect(trigger("/plan do it", 11, "claude")).toBeNull();
    expect(trigger("see /co", 7, "codex")).toBeNull();
    // Caret inside the word: the whole word is replaced on pick.
    expect(trigger("/compact", 3, "codex")?.end).toBe(8);
  });
  it("opens $ anywhere for Codex, only at the start for Claude", () => {
    expect(trigger("use $da", 7, "codex")).toEqual({
      kind: "$",
      query: "da",
      start: 4,
      end: 7,
    });
    expect(trigger("use $da", 7, "claude")).toBeNull();
    expect(trigger("$da", 3, "claude")).toEqual({
      kind: "$",
      query: "da",
      start: 0,
      end: 3,
    });
    expect(trigger("price $5", 8, "codex")?.query).toBe("5");
    expect(trigger("a$b", 3, "codex")).toBeNull();
  });
});

describe("items", () => {
  it("ranks exact and prefix matches first, and matches aliases", () => {
    const t = (q: string) => ({
      kind: "/" as const,
      query: q,
      start: 0,
      end: q.length + 1,
    });
    // Prefix matches in catalog order, then substring matches.
    expect(items(catalog, t("c")).map((i) => i.name)).toEqual([
      "compact",
      "clear",
      "context",
      "anthropic-skills:pdf",
    ]);
    expect(items(catalog, t("new")).map((i) => i.name)).toEqual(["clear"]);
    expect(items(catalog, t("pdf")).map((i) => [i.name, i.badge])).toEqual([
      ["anthropic-skills:pdf", "skill"],
    ]);
    expect(items(catalog, t("pl"))[0]).toMatchObject({
      name: "plan",
      badge: "workbench",
      hint: "[task]",
    });
  });
  it("lists skills for $", () => {
    expect(
      items(catalog, { kind: "$", query: "", start: 0, end: 1 }).map(
        (i) => i.name,
      ),
    ).toEqual(["dataviz", "anthropic-skills:pdf"]);
  });
});

describe("complete", () => {
  it("replaces the token and leaves one space", () => {
    expect(
      complete("/co", { kind: "/", query: "co", start: 0, end: 3 }, "compact"),
    ).toEqual({ text: "/compact ", caret: 9 });
    expect(
      complete(
        "use $da now",
        { kind: "$", query: "da", start: 4, end: 7 },
        "dataviz",
      ),
    ).toEqual({ text: "use $dataviz now", caret: 13 });
  });
});

describe("route", () => {
  it("runs workbench commands and page actions, passes Claude's own through", () => {
    expect(route("/plan fix the bug", catalog, "claude")).toEqual({
      kind: "command",
      name: "plan",
      args: "fix the bug",
    });
    expect(route("/status", catalog, "codex")).toEqual({
      kind: "action",
      action: "status",
      args: "",
    });
    expect(route("/compact keep tests", catalog, "claude")).toEqual({
      kind: "send",
    });
    expect(route("/whatever", catalog, "codex")).toEqual({
      kind: "command",
      name: "whatever",
      args: "",
    });
    expect(route("//etc is a path", catalog, "codex")).toEqual({
      kind: "send",
    });
    expect(route("hello /plan", catalog, "codex")).toEqual({ kind: "send" });
  });
});
