import type { KeyboardEvent } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { newlineKey } from "./newline";

function press(
  keys: { shift?: boolean; alt?: boolean; composing?: boolean },
  execCommand: () => boolean = () => false,
) {
  vi.stubGlobal("document", { execCommand });
  const box = {
    tagName: "TEXTAREA",
    value: "ab",
    selectionStart: 1,
    selectionEnd: 1,
    setRangeText(text: string, start: number, end: number) {
      this.value = this.value.slice(0, start) + text + this.value.slice(end);
    },
    dispatchEvent: vi.fn(),
  };
  const e = {
    key: "Enter",
    shiftKey: !!keys.shift,
    altKey: !!keys.alt,
    nativeEvent: { isComposing: !!keys.composing },
    target: box,
    preventDefault: vi.fn(),
  };
  return { box, e, newline: newlineKey(e as unknown as KeyboardEvent) };
}

afterEach(() => vi.unstubAllGlobals());

describe("newlineKey", () => {
  it("leaves Enter alone to send", () => {
    const { box, e, newline } = press({});
    expect(newline).toBe(false);
    expect(e.preventDefault).not.toHaveBeenCalled();
    expect(box.value).toBe("ab");
  });

  it("lets the browser add Shift+Enter's line", () => {
    const { box, e, newline } = press({ shift: true });
    expect(newline).toBe(true);
    expect(e.preventDefault).not.toHaveBeenCalled();
    expect(box.value).toBe("ab");
  });

  it("adds a line for Alt+Enter at the caret", () => {
    const { box, e, newline } = press({ alt: true });
    expect(newline).toBe(true);
    expect(e.preventDefault).toHaveBeenCalled();
    expect(box.value).toBe("a\nb");
    expect(box.dispatchEvent).toHaveBeenCalledOnce();
  });

  it("uses the browser's own insert when it has one, for undo", () => {
    const exec = vi.fn(() => true);
    const { box, newline } = press({ alt: true }, exec);
    expect(newline).toBe(true);
    expect(exec).toHaveBeenCalledWith("insertText", false, "\n");
    expect(box.value).toBe("ab");
  });

  it("does nothing while an input method composes", () => {
    const { box, e, newline } = press({ alt: true, composing: true });
    expect(newline).toBe(false);
    expect(e.preventDefault).not.toHaveBeenCalled();
    expect(box.value).toBe("ab");
  });
});
