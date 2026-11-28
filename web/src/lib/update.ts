import { needsYou } from "../attention";
import type { UpdateStatus } from "./about";

// Install and restart (ADR 0028): what the About tab says before and after.

type SessionLike = {
  id: string;
  title?: string;
  nativeId?: string;
  status: string;
  attention?: string;
};

// The sessions a restart touches: working, or waiting for the user.
export function busySessions<S extends SessionLike>(sessions: S[]): S[] {
  return sessions.filter(
    (s) => needsYou(s) || ["running", "starting", "waiting"].includes(s.status),
  );
}

// What a restart does to them: engines that are units of their own keep
// running; child processes stop with the workbench.
export function restartEffect(engines?: string): string {
  return engines === "systemd"
    ? "They keep running: their agents run apart from the workbench, and the page reconnects in a few seconds."
    : "Restarting stops their current turns. Their conversations are kept, and you can continue them afterwards.";
}

const UNDER_WAY: Record<string, string> = {
  starting: "starting",
  downloading: "downloading",
  verifying: "checking the download",
  installing: "installing and restarting",
};

export const updateUnderWay = (s?: UpdateStatus) => !!s && s.state in UNDER_WAY;

const bare = (v?: string) => (v ?? "").replace(/^v/, "").trim();

// The last update in words, or null when there is nothing to say (an
// update that found this was already the newest).
export function lastUpdate(
  s: UpdateStatus | undefined,
  current: string,
): { text: string; warn?: boolean } | null {
  if (!s) return null;
  const log = s.log ? ` Its log: ${s.log}` : "";
  switch (s.state) {
    case "done":
      return s.from &&
        bare(s.from) !== bare(s.to) &&
        bare(s.to) === bare(current)
        ? { text: `Updated from ${s.from} to ${s.to}.` }
        : null;
    case "failed":
      return {
        text: `The update to ${s.to} failed: ${s.error || "unknown error"}.${log}`,
        warn: true,
      };
    case "rolled-back":
      return {
        text: `${s.to} did not start (${s.error || "unknown error"}), so ${s.from} was put back.${log}`,
        warn: true,
      };
  }
  return { text: `Updating to ${s.to}: ${UNDER_WAY[s.state]}…` };
}
