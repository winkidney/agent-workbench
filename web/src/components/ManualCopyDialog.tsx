import { useRef } from "react";
import { Dialog, DialogContent, DialogTitle } from "./ui/dialog";
import { Button } from "./ui/button";

export function ManualCopyDialog({
  text,
  onClose,
}: {
  text: string | null;
  onClose: () => void;
}) {
  const field = useRef<HTMLTextAreaElement>(null);
  return (
    <Dialog open={text !== null} onOpenChange={(open) => !open && onClose()}>
      <DialogContent
        onOpenAutoFocus={(e) => {
          e.preventDefault();
          field.current?.focus();
          field.current?.select();
        }}
      >
        <DialogTitle>Copy manually</DialogTitle>
        <p>
          Automatic copying is unavailable. Select the text and copy it
          manually.
        </p>
        <textarea
          ref={field}
          className="manual-copy-text"
          aria-label="Text to copy"
          readOnly
          value={text ?? ""}
          rows={8}
        />
        <Button type="button" onClick={onClose}>
          Close
        </Button>
      </DialogContent>
    </Dialog>
  );
}
