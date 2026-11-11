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
  const base = {
    local: false,
    saved: "pairing" as const,
    credentialsChanged: false,
  };
  it("never warns on this computer", () => {
    expect(accessWarning({ ...base, local: true, draft: "off" })).toBe("");
  });
  it("warns another device that it loses access", () => {
    expect(accessWarning({ ...base, draft: "off" })).toMatch(/disconnected/);
  });
  it("warns another device that it must sign in again", () => {
    expect(accessWarning({ ...base, draft: "password" })).toMatch(/ask/);
    expect(
      accessWarning({
        ...base,
        saved: "password",
        draft: "password",
        credentialsChanged: true,
      }),
    ).toMatch(/ask/);
  });
  it("stays quiet when nothing changes for this device", () => {
    expect(accessWarning({ ...base, draft: "pairing" })).toBe("");
    expect(
      accessWarning({ ...base, saved: "password", draft: "password" }),
    ).toBe("");
    // Turning pairing on keeps the device connected (it gets a session).
    expect(
      accessWarning({ ...base, saved: "password", draft: "pairing" }),
    ).toBe("");
  });
});
