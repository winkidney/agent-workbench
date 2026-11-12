import { describe, expect, it } from "vitest";
import { afterError, footerConnection } from "./connection";

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
    expect(at({ live: "closed" })).toMatchObject({
      label: "Disconnected",
      tone: "down",
    });
  });
  it("follows the stream", () => {
    expect(afterError("open", false)).toBe("reconnecting");
    expect(afterError("connecting", false)).toBe("connecting");
    expect(afterError("reconnecting", false)).toBe("reconnecting");
    expect(afterError("open", true)).toBe("closed");
  });
});
