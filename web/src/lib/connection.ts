// What the sidebar's footer says about this page's connection: where the
// page is (this computer or another device) and whether live updates from
// the workbench arrive.

// The live update stream: first connecting, open, or retrying after it
// dropped. It is never left closed: the page always connects again
// (ADR 0001).
export type Live = "connecting" | "open" | "reconnecting";

export type FooterConnection = {
  label: string;
  tone: "ok" | "wait";
  // Hover text: how the page is connected.
  title: string;
};

export function footerConnection(o: {
  here: boolean;
  // Uses the workbench without signing in (this computer, sign-in off there).
  open: boolean;
  method?: string;
  protocol: string;
  live: Live;
}): FooterConnection {
  const how = o.open
    ? "no sign-in needed"
    : o.method === "password"
      ? "signed in with a password"
      : o.method === "pairing"
        ? "signed in with the pairing code"
        : "signed in";
  const transport =
    o.protocol === "https:"
      ? "encrypted (HTTPS)"
      : o.here
        ? "plain HTTP within this computer"
        : "plain HTTP across the network";
  const title = `${o.here ? "This computer" : "Another device"}, ${how}; ${transport}`;
  switch (o.live) {
    case "connecting":
      return { label: "Connecting…", tone: "wait", title };
    case "reconnecting":
      return {
        label: "Reconnecting…",
        tone: "wait",
        title: `Live updates stopped; trying again. ${title}`,
      };
  }
  return {
    label: o.here ? "Connected locally" : "Connected remotely",
    tone: "ok",
    title,
  };
}

// The stream's state after an error: retrying (the browser does, or the
// page makes a new one when the browser gave up); one that never opened is
// still connecting.
export function afterError(was: Live): Live {
  return was === "open" ? "reconnecting" : was;
}

// How long to wait before making the stream again after the browser gave up
// on it: longer each time, then every 30 seconds, for as long as it takes.
const DELAYS = [1000, 2000, 5000, 10000, 20000, 30000];
export const reconnectDelay = (attempt: number) =>
  DELAYS[Math.min(Math.max(0, attempt), DELAYS.length - 1)];

// The workbench sends something at least every 15 seconds (a ping when
// quiet). An open stream quiet for longer has stalled; a page that wakes
// (shown again, back online) renews one quiet for a while, or not open.
export const STALL_MS = 45000;
export const WAKE_MS = 20000;
export function shouldRenew(o: {
  now: number;
  last: number;
  live: Live;
  waking: boolean;
}): boolean {
  if (o.live !== "open") return o.waking;
  return o.now - o.last > (o.waking ? WAKE_MS : STALL_MS);
}
