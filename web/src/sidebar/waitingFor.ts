import { needsYou } from "../attention";
import type { RunningRun } from "../routines/waiting";
import type { Node } from "./tree";

// What waits for the user at an assistant (its sidebar row): its sessions
// (and their subsessions) that need the user, its routine runs waiting
// for approval, and its routines' reports not yet seen, as one count with a
// hover text that says what they are. The row shows the same bell for all.
export function waitingFor(
  nodes: Node[],
  runs: number,
  reports = 0,
): { count: number; title: string } {
  const kinds = new Map<string, number>();
  const visit = (n: Node) => {
    if (needsYou(n.session))
      kinds.set(
        n.session.attention!,
        (kinds.get(n.session.attention!) ?? 0) + 1,
      );
    n.children.forEach(visit);
  };
  nodes.forEach(visit);
  const parts = ["question", "approval", "plan"]
    .filter((k) => kinds.get(k))
    .map((k) => `${kinds.get(k)} ${k}${kinds.get(k)! > 1 ? "s" : ""}`);
  if (runs) parts.push(`${runs} routine run${runs > 1 ? "s" : ""}`);
  if (reports) parts.push(`${reports} report${reports > 1 ? "s" : ""}`);
  const count = [...kinds.values()].reduce((a, b) => a + b, 0) + runs + reports;
  return { count, title: count ? `Waiting for you: ${parts.join(", ")}` : "" };
}

// The assistants' rows as the sidebar lists them, and what the Assistants
// heading counts: exactly what their bells add up to, so its "needs you"
// always points at a row. A turned-off assistant with something waiting is
// listed even while the others that are off are folded away.
export function assistantRows<
  A extends { dir: string; path: string; enabled?: boolean },
>(
  assistants: A[],
  nodes: Node[],
  runs: Record<
    string,
    { count: number; reports?: unknown[]; running?: RunningRun[] } | undefined
  >,
  showOff: boolean,
) {
  const rows = assistants.map((assistant) => {
    const own = nodes.filter((n) => n.session.cwd === assistant.path);
    const count = runs[assistant.dir]?.count ?? 0;
    const reports = runs[assistant.dir]?.reports?.length ?? 0;
    return {
      assistant,
      nodes: own,
      runs: count,
      reports,
      wait: waitingFor(own, count, reports),
      // Its routine runs under way: the row shows them as running.
      running: runs[assistant.dir]?.running ?? [],
    };
  });
  const listed = rows.filter(
    (r) => r.assistant.enabled !== false || showOff || r.wait.count > 0,
  );
  return {
    listed,
    waiting: listed.reduce((n, r) => n + r.wait.count, 0),
    // Turned off and folded away.
    folded: rows.length - listed.length,
  };
}
