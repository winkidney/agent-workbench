import * as React from "react";
import { cn } from "../../lib/utils";
export function Input({
  className,
  type,
  ...props
}: React.ComponentProps<"input">) {
  return (
    <input
      type={type}
      data-slot="input"
      className={cn(
        "flex h-10 w-full rounded-md border border-border bg-transparent px-3 py-2 text-sm disabled:opacity-50",
        className,
      )}
      {...props}
    />
  );
}
