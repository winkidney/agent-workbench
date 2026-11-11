export type DiffLine = { op: " " | "+" | "-" | "@"; text: string };

const lines = (s: string) => (s === "" ? [] : s.replace(/\n$/, "").split("\n"));

// LCS line diff for edit previews; large inputs fall back to remove-then-add.
export function lineDiff(before: string, after: string): DiffLine[] {
  const a = lines(before);
  const b = lines(after);
  if (a.length * b.length > 250_000)
    return [
      ...a.map((text) => ({ op: "-" as const, text })),
      ...b.map((text) => ({ op: "+" as const, text })),
    ];
  const dp = Array.from(
    { length: a.length + 1 },
    () => new Uint32Array(b.length + 1),
  );
  for (let i = a.length - 1; i >= 0; i--)
    for (let j = b.length - 1; j >= 0; j--)
      dp[i][j] =
        a[i] === b[j]
          ? dp[i + 1][j + 1] + 1
          : Math.max(dp[i + 1][j], dp[i][j + 1]);
  const out: DiffLine[] = [];
  let i = 0;
  let j = 0;
  while (i < a.length && j < b.length) {
    if (a[i] === b[j]) {
      out.push({ op: " ", text: a[i] });
      i++;
      j++;
    } else if (dp[i + 1][j] >= dp[i][j + 1])
      out.push({ op: "-", text: a[i++] });
    else out.push({ op: "+", text: b[j++] });
  }
  while (i < a.length) out.push({ op: "-", text: a[i++] });
  while (j < b.length) out.push({ op: "+", text: b[j++] });
  return out;
}

// Trim unchanged runs to a few lines of context around changes.
export function withContext(diff: DiffLine[], context = 3): DiffLine[] {
  const keep = new Array(diff.length).fill(false);
  diff.forEach((l, i) => {
    if (l.op === " ") return;
    for (
      let k = Math.max(0, i - context);
      k <= Math.min(diff.length - 1, i + context);
      k++
    )
      keep[k] = true;
  });
  const out: DiffLine[] = [];
  let skipped = 0;
  diff.forEach((l, i) => {
    if (keep[i]) {
      if (skipped) out.push({ op: "@", text: `… ${skipped} unchanged lines` });
      skipped = 0;
      out.push(l);
    } else skipped++;
  });
  if (skipped && out.length)
    out.push({ op: "@", text: `… ${skipped} unchanged lines` });
  return out;
}

export function parseUnified(diff: string): DiffLine[] {
  return lines(diff)
    .filter((l) => !l.startsWith("diff --git") && !l.startsWith("index "))
    .map((l): DiffLine => {
      if (l.startsWith("+++") || l.startsWith("---"))
        return { op: "@", text: l.slice(4) };
      if (l.startsWith("@@")) return { op: "@", text: l };
      const op = l[0];
      if (op === "+" || op === "-" || op === " ")
        return { op, text: l.slice(1) };
      return { op: " ", text: l };
    });
}

export function diffStats(diff: DiffLine[]) {
  let add = 0;
  let del = 0;
  for (const l of diff) {
    if (l.op === "+") add++;
    else if (l.op === "-") del++;
  }
  return { add, del };
}
