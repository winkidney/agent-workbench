import type { RemoteAccess } from "./access";

// What the About tab shows (internal/workbench/about.go).
export type AboutAgent = {
  name: string;
  path?: string;
  version?: string;
  error?: string;
  features?: string[];
  missing?: string[];
  // Where the program and the home come from (Settings → Agents).
  source?: "setting" | "path";
  home?: string;
  homeSource?: "setting" | "environment" | "default";
};
export type About = {
  workbench: {
    version: string;
    build?: string;
    go?: string;
    platform?: string;
    dataDir?: string;
    instance?: string;
    listen?: string;
    engines?: string;
    started?: string;
    autostart?: string;
    repo?: string;
    license?: string;
  };
  agents: AboutAgent[];
  data: {
    storageBytes?: number;
    retentionDays?: number;
    sessions?: Record<string, number>;
  };
};

export const AGENT_NAMES: Record<string, string> = {
  claude: "Claude Code",
  codex: "Codex",
};

// How long the workbench has run: "3d 4h", "2h 5m", "4m".
export function uptime(started: string | undefined, now = Date.now()): string {
  if (!started) return "";
  const ms = now - Date.parse(started);
  if (!Number.isFinite(ms) || ms < 0) return "";
  const m = Math.floor(ms / 60000);
  const h = Math.floor(m / 60);
  const d = Math.floor(h / 24);
  if (d > 0) return `${d}d ${h % 24}h`;
  if (h > 0) return `${h}h ${m % 60}m`;
  return `${Math.max(m, 0)}m`;
}

export function bytes(n: number | undefined): string {
  if (n === undefined) return "";
  return n >= 1048576
    ? (n / 1048576).toFixed(1) + " MB"
    : Math.ceil(n / 1024) + " KB";
}

const parts = (v: string) => {
  const m = /^v?(\d+)\.(\d+)\.(\d+)/.exec(v.trim());
  return m ? m.slice(1).map(Number) : null;
};

// Whether latest is a newer release than current; never for builds that
// are not releases ("dev", a commit).
export function newer(current: string, latest: string): boolean {
  const a = parts(current);
  const b = parts(latest);
  if (!a || !b) return false;
  for (let i = 0; i < 3; i++) if (a[i] !== b[i]) return b[i] > a[i];
  return false;
}

export const ENGINES: Record<string, string> = {
  systemd: "systemd units",
  direct: "child processes",
};

// The connection this page has, one line each, for the Access tab's card.
export type ConnectionLine = { text: string; warn?: boolean };
export function connection(o: {
  thisComputer: boolean;
  localSignin: boolean;
  remoteAccess: RemoteAccess;
  signedIn?: number;
  protocol: string;
  originRefused: boolean;
}): ConnectionLine[] {
  const method =
    o.remoteAccess === "password" ? "a password" : "the pairing code";
  const lines: ConnectionLine[] = [
    {
      text: o.thisComputer
        ? o.localSignin
          ? `This computer, signed in with ${method} (sign in on this computer is on)`
          : "This computer: no sign-in needed"
        : `Another device, signed in with ${method}`,
    },
  ];
  if (o.protocol === "https:") lines.push({ text: "Encrypted (HTTPS)" });
  else if (o.thisComputer)
    lines.push({ text: "Plain HTTP, within this computer" });
  else
    lines.push({
      text: "Plain HTTP: what you send crosses the network unencrypted. Use HTTPS or a VPN on networks you don't trust.",
      warn: true,
    });
  if (o.originRefused)
    lines.push({
      text: "This address is not an allowed origin: changes made here are refused.",
      warn: true,
    });
  lines.push({
    text:
      o.remoteAccess === "off"
        ? "Other devices: off"
        : o.remoteAccess === "pairing"
          ? `Other devices: pairing code · ${o.signedIn ?? 0} signed in`
          : "Other devices: password",
  });
  return lines;
}

// A plain-text summary for bug reports, as `agent-workbench doctor` prints.
export function diagnostics(a: About, now = Date.now()): string {
  const w = a.workbench;
  const lines = [
    `agent-workbench ${w.version} (${w.platform ?? ""}, ${w.go ?? ""})`,
    w.build && `web build: ${w.build}`,
    w.started && `running for: ${uptime(w.started, now)}`,
    w.engines && `engines: ${ENGINES[w.engines] ?? w.engines}`,
    w.instance && `instance: ${w.instance}`,
    w.listen && `listen: ${w.listen}`,
    w.dataDir && `data dir: ${w.dataDir}`,
    w.autostart && `autostart: ${w.autostart}`,
    ...a.agents.flatMap((g) => [
      g.path
        ? `${g.name}: ${g.path} (${g.version || g.error || "unknown version"})` +
          (g.source === "setting" ? " [chosen in Settings]" : "") +
          (g.missing?.length ? ` — lacks ${g.missing.join(", ")}` : "")
        : `${g.name}: ${g.error ?? "not found"}`,
      g.home && `${g.name} home: ${g.home} (${g.homeSource ?? "default"})`,
    ]),
    a.data.storageBytes !== undefined &&
      `local data: ${bytes(a.data.storageBytes)}, events kept ${a.data.retentionDays ?? 7} days`,
    a.data.sessions &&
      `sessions: ${Object.entries(a.data.sessions)
        .map(([k, n]) => `${k} ${n}`)
        .join(", ")}`,
  ];
  return lines.filter(Boolean).join("\n");
}
