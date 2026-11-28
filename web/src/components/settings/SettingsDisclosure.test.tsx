import { afterEach, describe, expect, it } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";
import { SettingsDisclosure, remembered } from "./SettingsDisclosure";
import {
  browseCatalog,
  browseRequested,
  mainStarters,
  MAIN_ACTIONS,
  MAIN_STARTERS,
  takeBrowseRequest,
} from "../../lib/assistants";

const store = new Map<string, string>();
globalThis.localStorage = {
  getItem: (k: string) => store.get(k) ?? null,
  setItem: (k: string, v: string) => void store.set(k, v),
  removeItem: (k: string) => void store.delete(k),
  clear: () => store.clear(),
  key: () => null,
  length: 0,
} as Storage;

describe("settings disclosure", () => {
  afterEach(() => store.clear());
  const html = (p: Partial<Parameters<typeof SettingsDisclosure>[0]> = {}) =>
    renderToStaticMarkup(
      <SettingsDisclosure id="backup" title="Backup" note="Up to date" {...p}>
        <p>inside</p>
      </SettingsDisclosure>,
    );

  it("shows its title and note, and what is inside only when open", () => {
    const shut = html();
    expect(shut).toContain("<h3>Backup</h3>");
    expect(shut).toContain("Up to date");
    expect(shut).not.toContain("inside");
    expect(html({ defaultOpen: true })).toContain("inside");
  });

  it("opens as it was left, unless something asks to show it", () => {
    store.set("workbench:settings-open:backup", "1");
    expect(remembered("backup")).toBe(true);
    expect(html()).toContain("inside");
    store.set("workbench:settings-open:backup", "0");
    expect(html({ defaultOpen: true })).not.toContain("inside");
    expect(html({ force: true })).toContain("inside");
    expect(html({ force: 2 })).toContain("inside");
  });

  it("opens by default without storage", () => {
    const keep = globalThis.localStorage;
    globalThis.localStorage = {
      getItem: () => {
        throw new Error("blocked");
      },
    } as unknown as Storage;
    try {
      expect(remembered("backup")).toBeUndefined();
      expect(html({ defaultOpen: true })).toContain("inside");
    } finally {
      globalThis.localStorage = keep;
    }
  });
});

describe("Moss's starts", () => {
  it("adds installing and routines to the card's requests", () => {
    const s = mainStarters(["What can you do?"]);
    expect(s[0]).toEqual({
      label: "What can you do?",
      text: "What can you do?",
    });
    expect(s.map((x) => x.label)).toEqual([
      "What can you do?",
      ...MAIN_ACTIONS.map((x) => x.label),
    ]);
    expect(mainStarters(undefined)).toBe(MAIN_STARTERS);
    expect(MAIN_STARTERS.some((x) => x.label === "Install an assistant")).toBe(
      true,
    );
  });

  it("a request to browse the catalog waits to be taken once", () => {
    // No browser here: an event target stands in for the window.
    const had = globalThis.window;
    globalThis.window = new EventTarget() as Window & typeof globalThis;
    let heard = 0;
    const on = () => heard++;
    window.addEventListener("workbench:browse-catalog", on);
    browseCatalog();
    window.removeEventListener("workbench:browse-catalog", on);
    expect(heard).toBe(1);
    expect(browseRequested()).toBe(true);
    expect(browseRequested()).toBe(true);
    expect(takeBrowseRequest()).toBe(true);
    expect(takeBrowseRequest()).toBe(false);
    globalThis.window = had;
  });
});
