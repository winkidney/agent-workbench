// Copies, or selects the field where the clipboard is unavailable (plain
// HTTP on another device is not a secure context).
export function copyField(id: string, value: string, done: () => void) {
  const select = () => {
    const el = document.getElementById(id) as
      HTMLInputElement | HTMLTextAreaElement | null;
    el?.focus();
    el?.select();
  };
  if (!navigator.clipboard) return select();
  navigator.clipboard.writeText(value).then(done, select);
}
