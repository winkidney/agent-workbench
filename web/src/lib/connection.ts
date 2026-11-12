// What the sidebar's footer says about this page's connection: where the
// page is (this computer or another device) and whether live updates from
// the workbench arrive.

// The live update stream: first connecting, open, retrying after it
// dropped, or closed for good (the server refused it).
export type Live = "connecting" | "open" | "reconnecting" | "closed";

export type FooterConnection = {
  label: string;
  tone: "ok" | "wait" | "down";
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
    case "closed":
      return {
        label: "Disconnected",
        tone: "down",
        title: `The workbench stopped sending live updates. ${title}`,
      };
  }
  return {
    label: o.here ? "Connected locally" : "Connected remotely",
    tone: "ok",
    title,
  };
}

// The stream's state after an error: EventSource retries on its own unless
// the server refused it; one that never opened is still connecting.
export function afterError(was: Live, closed: boolean): Live {
  if (closed) return "closed";
  return was === "open" ? "reconnecting" : was;
}
