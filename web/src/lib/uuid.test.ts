import { afterEach, describe, expect, it, vi } from "vitest";
import { randomUUID } from "./uuid";

const getRandomValues = crypto.getRandomValues.bind(crypto);

afterEach(() => vi.unstubAllGlobals());

describe("request UUIDs", () => {
  it("uses the browser's native method with its Crypto receiver", () => {
    const native = vi.fn(function (this: unknown) {
      expect(this).toBe(globalThis.crypto);
      return "8af310fb-423c-4bb4-b253-b541283d4028";
    });
    vi.stubGlobal("crypto", { randomUUID: native });
    expect(randomUUID()).toBe("8af310fb-423c-4bb4-b253-b541283d4028");
    expect(native).toHaveBeenCalledOnce();
  });

  it("creates v4 UUIDs on HTTP where only getRandomValues is available", () => {
    vi.stubGlobal("crypto", {
      getRandomValues,
    });
    const ids = Array.from({ length: 100 }, () => randomUUID());
    for (const id of ids)
      expect(id).toMatch(
        /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/,
      );
    expect(new Set(ids).size).toBe(ids.length);
  });

  it("preserves random bytes while setting the UUID version and variant", () => {
    vi.stubGlobal("crypto", {
      getRandomValues(this: unknown, bytes: Uint8Array) {
        expect(this).toBe(globalThis.crypto);
        bytes.set(Array.from({ length: 16 }, (_, i) => i));
        return bytes;
      },
    });
    expect(randomUUID()).toBe("00010203-0405-4607-8809-0a0b0c0d0e0f");
  });
});
