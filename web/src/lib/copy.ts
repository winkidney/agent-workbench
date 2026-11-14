// Plain HTTP on another device has no Clipboard API.
export async function copyText(value: string): Promise<boolean> {
  try {
    if (typeof navigator.clipboard?.writeText === "function") {
      await navigator.clipboard.writeText(value);
      return true;
    }
  } catch {
    // A browser may expose the API but refuse the write.
  }
  const previous = document.activeElement as HTMLElement | null;
  const field = document.createElement("textarea");
  field.value = value;
  field.readOnly = true;
  field.tabIndex = -1;
  field.style.cssText = "position:fixed;left:-10000px;top:0";
  // Stay inside a modal's focus trap when copying from Settings.
  const parent = previous?.closest('[role="dialog"]') ?? document.body;
  try {
    parent.append(field);
    field.focus({ preventScroll: true });
    field.select();
    return document.execCommand?.("copy") === true;
  } catch {
    return false;
  } finally {
    field.remove();
    previous?.focus({ preventScroll: true });
  }
}

// Existing fields remain available for manual copying if both methods fail.
export function copyField(id: string, value: string, done: () => void) {
  const select = () => {
    const el = document.getElementById(id) as
      HTMLInputElement | HTMLTextAreaElement | null;
    el?.focus();
    el?.select();
  };
  void copyText(value).then((copied) => (copied ? done() : select()));
}
