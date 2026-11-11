import { describe, expect, it } from "vitest";
import { accessWarning, newPassword } from "./access";

describe("newPassword", () => {
  it("makes 16 unambiguous characters, different each time", () => {
    const seen = new Set<string>();
    for (let i = 0; i < 50; i++) {
      const p = newPassword();
      expect(p).toMatch(/^[a-hjkmnp-z2-9]{16}$/);
      seen.add(p);
    }
    expect(seen.size).toBe(50);
  });
});

describe("accessWarning", () => {
  const remote = {
    thisComputer: false,
    savedLocalSignin: false,
    localSignin: false,
    saved: "pairing" as const,
    credentialsChanged: false,
  };
  const here = { ...remote, thisComputer: true };
  it("never warns this computer while it skips sign-in", () => {
    expect(accessWarning({ ...here, draft: "off" })).toBe("");
    expect(accessWarning({ ...here, draft: "password" })).toBe("");
  });
  it("warns another device that it loses access", () => {
    expect(accessWarning({ ...remote, draft: "off" })).toMatch(/disconnected/);
  });
  it("warns another device that it must sign in again", () => {
    expect(accessWarning({ ...remote, draft: "password" })).toMatch(/ask/);
    expect(
      accessWarning({
        ...remote,
        saved: "password",
        draft: "password",
        credentialsChanged: true,
      }),
    ).toMatch(/ask/);
  });
  it("warns this computer when it starts signing in with the password", () => {
    expect(
      accessWarning({ ...here, localSignin: true, draft: "password" }),
    ).toMatch(/This browser will ask/);
    // Already signing in with the same password: nothing changes.
    expect(
      accessWarning({
        ...here,
        savedLocalSignin: true,
        localSignin: true,
        saved: "password",
        draft: "password",
      }),
    ).toBe("");
  });
  it("stays quiet when nothing changes for this device", () => {
    expect(accessWarning({ ...remote, draft: "pairing" })).toBe("");
    expect(
      accessWarning({ ...remote, saved: "password", draft: "password" }),
    ).toBe("");
    // Pairing keeps the saving browser signed in (it gets a session).
    expect(
      accessWarning({ ...remote, saved: "password", draft: "pairing" }),
    ).toBe("");
    expect(
      accessWarning({ ...here, localSignin: true, draft: "pairing" }),
    ).toBe("");
  });
});
