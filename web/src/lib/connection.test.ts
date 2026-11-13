import { describe, expect, it } from "vitest";
import {
  STALL_MS,
  WAKE_MS,
  afterError,
  footerConnection,
  reconnectDelay,
  shouldRenew,
} from "./connection";

const at = (o: Partial<Parameters<typeof footerConnection>[0]>) =>
  footerConnection({
    here: true,
    open: true,
    protocol: "http:",
    live: "open",
    ...o,
  });

describe("the sidebar's connection line", () => {
  it("says where the page is", () => {
    expect(at({})).toEqual({
      label: "Connected locally",
      tone: "ok",
      title:
        "This computer, no sign-in needed; plain HTTP within this computer",
    });
    const tunnel = at({
      here: false,
      open: false,
      method: "password",
      protocol: "https:",
    });
    expect(tunnel.label).toBe("Connected remotely");
    expect(tunnel.title).toBe(
      "Another device, signed in with a password; encrypted (HTTPS)",
    );
    expect(at({ here: false, open: false, method: "pairing" }).title).toBe(
      "Another device, signed in with the pairing code; plain HTTP across the network",
    );
    // Sign in on this computer too.
    expect(at({ open: false, method: "password" }).title).toBe(
      "This computer, signed in with a password; plain HTTP within this computer",
    );
  });
  it("says when live updates are not arriving", () => {
    expect(at({ live: "connecting" })).toMatchObject({
      label: "Connecting…",
      tone: "wait",
    });
    expect(
      at({ here: false, open: false, live: "reconnecting" }),
    ).toMatchObject({ label: "Reconnecting…", tone: "wait" });
  });
  it("follows the stream, never giving up", () => {
    expect(afterError("open")).toBe("reconnecting");
    expect(afterError("connecting")).toBe("connecting");
    expect(afterError("reconnecting")).toBe("reconnecting");
  });
  it("waits longer each time, up to every 30 seconds", () => {
    expect([0, 1, 2, 3, 4, 5, 6, 50].map(reconnectDelay)).toEqual([
      1000, 2000, 5000, 10000, 20000, 30000, 30000, 30000,
    ]);
    expect(reconnectDelay(-1)).toBe(1000);
  });
  it("renews a stream that went quiet, or that is not open on waking", () => {
    const now = 100_000;
    const open = { now, live: "open" as const, waking: false };
    expect(shouldRenew({ ...open, last: now - 1000 })).toBe(false);
    expect(shouldRenew({ ...open, last: now - STALL_MS - 1 })).toBe(true);
    // Shown again: a shorter quiet is enough.
    expect(
      shouldRenew({ ...open, waking: true, last: now - WAKE_MS + 1 }),
    ).toBe(false);
    expect(
      shouldRenew({ ...open, waking: true, last: now - WAKE_MS - 1 }),
    ).toBe(true);
    // Not open: the retry timer handles it, unless the page just woke.
    expect(
      shouldRenew({ now, last: 0, live: "reconnecting", waking: false }),
    ).toBe(false);
    expect(
      shouldRenew({ now, last: 0, live: "reconnecting", waking: true }),
    ).toBe(true);
  });
});
