import { describe, expect, it } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";
import { SettingsTabs } from "./SettingsDialog";
import { AgentRow } from "./AboutTab";
import {
  connection,
  diagnostics,
  newer,
  uptime,
  type About,
} from "../../lib/about";

describe("about helpers", () => {
  it("reads uptimes", () => {
    const now = Date.parse("2026-11-12T12:00:00Z");
    expect(uptime("2026-11-12T11:56:00Z", now)).toBe("4m");
    expect(uptime("2026-11-12T09:55:00Z", now)).toBe("2h 5m");
    expect(uptime("2026-11-09T08:00:00Z", now)).toBe("3d 4h");
    expect(uptime(undefined, now)).toBe("");
  });

  it("compares releases, never a development build", () => {
    expect(newer("v0.3.9", "v0.3.10")).toBe(true);
    expect(newer("v0.3.10", "v0.3.9")).toBe(false);
    expect(newer("v0.3.9", "v0.3.9")).toBe(false);
    expect(newer("v0.3.9", "v1.0.0")).toBe(true);
    expect(newer("dev", "v0.3.9")).toBe(false);
    expect(newer("v0.3.9-3-gabc", "v0.3.9")).toBe(false);
  });

  it("describes the connection", () => {
    const base = {
      thisComputer: false,
      localSignin: false,
      remoteAccess: "pairing" as const,
      signedIn: 2,
      protocol: "http:",
      originRefused: false,
    };
    const lines = connection(base);
    expect(lines.map((l) => [l.text.split(":")[0], !!l.warn])).toEqual([
      ["Another device, signed in with the pairing code", false],
      ["Plain HTTP", true],
      ["Other devices", false],
    ]);
    expect(lines[2].text).toBe("Other devices: pairing code · 2 signed in");
    const local = connection({
      ...base,
      thisComputer: true,
      remoteAccess: "off",
      protocol: "http:",
      originRefused: true,
    });
    expect(local.map((l) => l.text)).toEqual([
      "This computer: no sign-in needed",
      "Plain HTTP, within this computer",
      "This address is not an allowed origin: changes made here are refused.",
      "Other devices: off",
    ]);
    expect(local[2].warn).toBe(true);
    expect(
      connection({ ...base, protocol: "https:", remoteAccess: "password" }).map(
        (l) => l.text,
      ),
    ).toEqual([
      "Another device, signed in with a password",
      "Encrypted (HTTPS)",
      "Other devices: password",
    ]);
  });

  it("writes diagnostics like the doctor command", () => {
    const about: About = {
      workbench: {
        version: "v0.3.9",
        platform: "linux/amd64",
        go: "go1.26",
        engines: "systemd",
        listen: "0.0.0.0:17880",
        dataDir: "/data",
        started: "2026-11-12T11:00:00Z",
      },
      agents: [
        {
          name: "claude",
          path: "/bin/claude",
          version: "2.1.322 (Claude Code)",
          missing: ["--include-hook-events"],
        },
        { name: "codex", error: "not found on PATH" },
      ],
      data: {
        storageBytes: 3 * 1048576,
        retentionDays: 7,
        sessions: { claude: 4, codex: 2 },
      },
    };
    expect(diagnostics(about, Date.parse("2026-11-12T12:00:00Z"))).toBe(
      [
        "agent-workbench v0.3.9 (linux/amd64, go1.26)",
        "running for: 1h 0m",
        "engines: systemd units",
        "listen: 0.0.0.0:17880",
        "data dir: /data",
        "claude: /bin/claude (2.1.322 (Claude Code)) — lacks --include-hook-events",
        "codex: not found on PATH",
        "local data: 3.0 MB, events kept 7 days",
        "sessions: claude 4, codex 2",
      ].join("\n"),
    );
  });
});

describe("settings tabs", () => {
  it("is a tablist with one selected tab, and marks tabs with unsaved changes", () => {
    const html = renderToStaticMarkup(
      <SettingsTabs tab="about" dirty={{ access: true }} onSelect={() => {}} />,
    );
    expect(html).toContain('role="tablist"');
    expect(html.match(/role="tab"/g)?.length).toBe(5);
    expect(html.match(/tab-dot/g)?.length).toBe(1);
    expect(
      renderToStaticMarkup(
        <SettingsTabs
          tab="general"
          dirty={{ agents: true, access: true }}
          onSelect={() => {}}
        />,
      ).match(/tab-dot/g)?.length,
    ).toBe(2);
    expect(html).toContain(
      'id="settings-tab-about" aria-selected="true" aria-controls="settings-panel-about" tabindex="0"',
    );
    expect(html).toContain('aria-selected="false"');
    expect(html).toContain("tab-dot");
    expect(
      renderToStaticMarkup(
        <SettingsTabs tab="general" dirty={{}} onSelect={() => {}} />,
      ),
    ).not.toContain("tab-dot");
  });

  it("says where an agent's program and home come from", () => {
    const html = renderToStaticMarkup(
      <AgentRow
        agent={{
          name: "claude",
          path: "/opt/claude",
          version: "2.1.322 (Claude Code)",
          source: "setting",
          home: "/home/u/.claude-work",
          homeSource: "setting",
        }}
      />,
    );
    expect(html).toContain("Program chosen in Settings.");
    expect(html).toContain("Home: /home/u/.claude-work (chosen in Settings)");
    const report = diagnostics({
      workbench: { version: "v1", instance: "agent-workbench-17881" },
      agents: [
        {
          name: "codex",
          path: "/bin/codex",
          version: "codex-cli 1",
          source: "setting",
          home: "/c",
          homeSource: "environment",
        },
      ],
      data: {},
    });
    expect(report).toContain("instance: agent-workbench-17881");
    expect(report).toContain(
      "codex: /bin/codex (codex-cli 1) [chosen in Settings]",
    );
    expect(report).toContain("codex home: /c (environment)");
  });

  it("shows an agent that is missing, and features a CLI lacks", () => {
    const missing = renderToStaticMarkup(
      <AgentRow agent={{ name: "codex", error: "not found on PATH" }} />,
    );
    expect(missing).toContain("Codex");
    expect(missing).toContain('class="warn"');
    expect(missing).toContain("not found on PATH");
    const claude = renderToStaticMarkup(
      <AgentRow
        agent={{
          name: "claude",
          path: "/bin/claude",
          version: "2.1.322 (Claude Code)",
          features: ["--prompt-suggestions"],
          missing: ["--include-hook-events"],
        }}
      />,
    );
    expect(claude).toContain("2.1.322 (Claude Code)");
    expect(claude).toContain("Prompt suggestions");
    expect(claude).toContain("Hook events: needs a newer Claude Code");
  });
});
