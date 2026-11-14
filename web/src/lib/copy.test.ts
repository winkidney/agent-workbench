import { afterEach, describe, expect, it, vi } from "vitest";
import { copyField, copyText } from "./copy";

afterEach(() => vi.unstubAllGlobals());

function page(exec: (() => boolean) | undefined = () => true) {
  const field = {
    value: "",
    readOnly: false,
    tabIndex: 0,
    style: { cssText: "" },
    focus: vi.fn(),
    select: vi.fn(),
    remove: vi.fn(),
  };
  const parent = { append: vi.fn() };
  const modal = { append: vi.fn() };
  const previous = { focus: vi.fn(), closest: vi.fn(() => modal) };
  const target = { focus: vi.fn(), select: vi.fn() };
  vi.stubGlobal("navigator", {});
  vi.stubGlobal("document", {
    activeElement: previous,
    body: parent,
    createElement: () => field,
    execCommand: exec,
    getElementById: () => target,
  });
  return { field, parent, modal, previous, target };
}

describe("copying text", () => {
  it("prefers the native API", async () => {
    const p = page();
    const writeText = vi.fn().mockResolvedValue(undefined);
    vi.stubGlobal("navigator", { clipboard: { writeText } });
    expect(await copyText("中文\nline 2")).toBe(true);
    expect(writeText).toHaveBeenCalledWith("中文\nline 2");
    expect(p.modal.append).not.toHaveBeenCalled();
  });
  it("copies synchronously on HTTP, stays inside a modal and restores focus", async () => {
    const exec = vi.fn(() => true);
    const p = page(exec);
    const result = copyText("中文\nline 2");
    expect(exec).toHaveBeenCalledWith("copy");
    expect(await result).toBe(true);
    expect(p.field.value).toBe("中文\nline 2");
    expect(p.modal.append).toHaveBeenCalledWith(p.field);
    expect(p.field.select).toHaveBeenCalledOnce();
    expect(p.field.remove).toHaveBeenCalledOnce();
    expect(p.previous.focus).toHaveBeenCalledWith({ preventScroll: true });
  });
  it("falls back after native rejection, or a synchronous exception", async () => {
    page();
    for (const writeText of [
      () => Promise.reject(new Error("denied")),
      () => {
        throw new Error("denied");
      },
    ]) {
      vi.stubGlobal("navigator", { clipboard: { writeText } });
      expect(await copyText("text")).toBe(true);
    }
  });
  it("uses the body outside a modal", async () => {
    const p = page();
    p.previous.closest.mockReturnValue(null as never);
    expect(await copyText("text")).toBe(true);
    expect(p.parent.append).toHaveBeenCalledWith(p.field);
  });
  it("reports failure and cleans up when legacy copying fails, throws or is missing", async () => {
    for (const exec of [
      () => false,
      () => {
        throw new Error("denied");
      },
      undefined,
    ]) {
      const p = page(exec);
      if (!exec)
        vi.stubGlobal("document", { ...document, execCommand: undefined });
      expect(await copyText("text")).toBe(false);
      expect(p.field.remove).toHaveBeenCalledOnce();
      expect(p.previous.focus).toHaveBeenCalledOnce();
    }
  });
  it("selects the existing field on failure without claiming success", async () => {
    const p = page(() => false);
    const done = vi.fn();
    copyField("code", "text", done);
    await vi.waitFor(() => expect(p.target.select).toHaveBeenCalledOnce());
    expect(done).not.toHaveBeenCalled();
    page();
    copyField("code", "text", done);
    await vi.waitFor(() => expect(done).toHaveBeenCalledOnce());
  });
});
