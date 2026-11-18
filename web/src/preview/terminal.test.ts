import { describe, expect, it } from "vitest";
import {
  inTerminal,
  parseControl,
  retryDelay,
  socketUrl,
  terminalTheme,
} from "./terminal";

describe("terminal tabs", () => {
  it("attaches over ws, or wss on a secure page", () => {
    expect(
      socketUrl("ab12", { protocol: "http:", host: "127.0.0.1:17880" }),
    ).toBe("ws://127.0.0.1:17880/api/terminals/ab12/socket");
    expect(socketUrl("ab12", { protocol: "https:", host: "aw.example" })).toBe(
      "wss://aw.example/api/terminals/ab12/socket",
    );
  });

  it("reads what the workbench says besides output", () => {
    expect(parseControl('{"exit":3}')).toEqual({ exit: 3 });
    expect(parseControl('{"missing":true}')).toEqual({ missing: true });
    expect(parseControl("not json")).toEqual({});
    expect(parseControl("null")).toEqual({});
  });

  it("waits longer after each lost connection, up to 30 s", () => {
    expect([0, 1, 2, 5, 9].map(retryDelay)).toEqual([
      1000, 2000, 4000, 30000, 30000,
    ]);
  });

  it("leaves a terminal's keys to its shell", () => {
    const inside = { closest: (s: string) => (s === ".xterm" ? {} : null) };
    const outside = { closest: () => null };
    expect(inTerminal(inside as unknown as EventTarget)).toBe(true);
    expect(inTerminal(outside as unknown as EventTarget)).toBe(false);
    expect(inTerminal(null)).toBe(false);
    // A window or document has no closest().
    expect(inTerminal({} as EventTarget)).toBe(false);
  });

  it("takes the theme's colors", () => {
    const tokens: Record<string, string> = {
      "--background": " #2d353b",
      "--foreground": "#d3c6aa",
      "--primary": "#a7c080",
    };
    const theme = terminalTheme({ getPropertyValue: (n) => tokens[n] ?? "" });
    expect(theme).toMatchObject({
      background: "#2d353b",
      foreground: "#d3c6aa",
      cursor: "#a7c080",
      cursorAccent: "#2d353b",
    });
  });
});
