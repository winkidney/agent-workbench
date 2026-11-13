// The file being edited with changes not saved, if any (one editor at a
// time): only its name, never its text. What would close the editor asks
// first; so does leaving the page.

let unsaved: string | null = null;

const onUnload = (e: BeforeUnloadEvent) => {
  e.preventDefault();
  // Older browsers want a return value to ask.
  e.returnValue = "";
};

export function markUnsaved(name: string | null) {
  if (!!name === !!unsaved) {
    unsaved = name;
    return;
  }
  unsaved = name;
  if (name) window.addEventListener("beforeunload", onUnload);
  else window.removeEventListener("beforeunload", onUnload);
}

// Whether what would close the editor may go ahead: nothing unsaved, or the
// changes are to be discarded.
export function mayLeave(): boolean {
  if (!unsaved) return true;
  if (!confirm(`Discard your unsaved changes to ${unsaved}?`)) return false;
  markUnsaved(null);
  return true;
}
