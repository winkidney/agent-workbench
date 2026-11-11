import * as React from "react";
import { cn } from "../../lib/utils";
export function Textarea({
  className,
  ...props
}: React.ComponentProps<"textarea">) {
  return (
    <textarea
      data-slot="textarea"
      className={cn(
        "flex min-h-20 w-full rounded-md border border-border bg-transparent px-3 py-2 text-sm disabled:opacity-50",
        className,
      )}
      {...props}
    />
  );
}
