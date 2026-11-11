import { describe, expect, it } from "vitest";
import { refusal } from "./api";

describe("refusal", () => {
  it("reloads for the server's own pages to other devices", () => {
    for (const [status, kind] of [
      [401, "sign-in"],
      [403, "off"],
      [429, "limited"],
    ] as const)
      expect(refusal(status, new Headers({ "X-Workbench-Access": kind }))).toBe(
        "reload",
      );
  });
  it("asks for the pairing code when the session ended", () => {
    expect(refusal(401, new Headers())).toBe("pair");
  });
  it("leaves other errors to the caller", () => {
    expect(refusal(403, new Headers())).toBeUndefined();
    expect(refusal(500, new Headers())).toBeUndefined();
    expect(refusal(200, new Headers())).toBeUndefined();
  });
});
