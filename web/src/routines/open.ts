// Asks App.tsx to open Settings → Routines with one routine open on its runs:
// a compact heartbeat outside that tab (an assistant's card or settings)
// leads there, as it opens the routine inside it.
export const OPEN_ROUTINE_EVENT = "workbench:open-routine";
export const openRoutine = (id: string) =>
  window.dispatchEvent(new CustomEvent(OPEN_ROUTINE_EVENT, { detail: id }));
