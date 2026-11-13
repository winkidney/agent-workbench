import type { KeyboardEvent } from "react";

// Shift+Enter and Alt+Enter add a line; Enter alone sends. Browsers differ on
// what Alt+Enter does in a text box, so the line is added here.
export function newlineKey(e: KeyboardEvent): boolean {
  if (e.key !== "Enter" || e.nativeEvent.isComposing) return false;
  if (e.shiftKey) return true;
  const el = e.target as HTMLTextAreaElement;
  if (!e.altKey || el.tagName !== "TEXTAREA") return false;
  e.preventDefault();
  // insertText keeps the browser's undo; without it, edit the value and tell
  // React, which reads the change from the input event.
  if (!document.execCommand?.("insertText", false, "\n")) {
    el.setRangeText("\n", el.selectionStart, el.selectionEnd, "end");
    el.dispatchEvent(new Event("input", { bubbles: true }));
  }
  return true;
}
