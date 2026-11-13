import { describe, expect, it } from "vitest";
import {
  applyOp,
  buildSidebar,
  emptyPrefs,
  needsYou,
  relativeTime,
  resumeCommand,
  type Project,
  type Session,
} from "./tree";

const s = (
  id: string,
  ws: string,
  updated: string,
  extra: Partial<Session> = {},
): Session => ({
  id,
  agent: id.startsWith("codex") ? "codex" : "claude",
  cwd: ws,
  workspace: ws,
  title: id,
  nativeId: id.split(":")[1],
  status: "sleeping",
  updated,
  ...extra,
});

const sessions = [
  s("codex:a", "/w/app", "2026-11-10T10:00:00Z", {
    status: "running",
    title: "設計多模型 Agent 代理",
  }),
  s("codex:a1", "/w/app", "2026-11-10T11:00:00Z", {
    parentId: "codex:a",
    title: "subagent explore",
  }),
  s("claude:b", "/w/app", "2026-11-09T10:00:00Z", { status: "idle" }),
  s("claude:c", "/x/billing", "2026-11-08T10:00:00Z", {
    status: "external",
    title: "結算 方案",
  }),
  s("codex:d", "/x/billing", "2026-11-01T10:00:00Z"),
  s("claude:e", "/y/old", "2026-10-01T10:00:00Z", { status: "waiting" }),
];

describe("sidebar tree", () => {
  it("groups by project, nests subsessions and sorts by activity", () => {
    const m = buildSidebar(sessions, emptyPrefs(), {
      workspaces: ["/z/empty"],
    });
    expect(m.projects.map((p) => p.name)).toEqual([
      "app",
      "billing",
      "old",
      "empty",
    ]);
    const app = m.projects[0];
    expect(app.nodes.map((n) => n.session.id)).toEqual(["codex:a", "claude:b"]);
    expect(app.nodes[0].children.map((n) => n.session.id)).toEqual([
      "codex:a1",
    ]);
    // The subsession's activity counts for the project.
    expect(app.latest).toBe("2026-11-10T11:00:00Z");
  });

  it("lists active sessions busy first, then idle, then active elsewhere", () => {
    const m = buildSidebar(sessions, emptyPrefs());
    expect(m.active.map((n) => n.session.id)).toEqual([
      "codex:a",
      "claude:e",
      "claude:b",
      "claude:c",
    ]);
  });

  it("keeps subagents under their parent in the active list", () => {
    const list = [
      s("codex:p", "/w/app", "2026-11-10T09:00:00Z", { status: "running" }),
      s("codex:k1", "/w/app", "2026-11-10T09:05:00Z", {
        status: "external_running",
        parentId: "codex:p",
        title: "ui_architecture",
      }),
      s("codex:k2", "/w/app", "2026-11-10T09:06:00Z", {
        status: "external",
        parentId: "codex:p",
        title: "state_contracts",
      }),
      // Done and asleep: not listed as at work.
      s("codex:k3", "/w/app", "2026-11-10T09:01:00Z", {
        parentId: "codex:p",
        title: "save_domains",
      }),
      // A parent asleep while its subagent still works.
      s("claude:q", "/w/app", "2026-11-09T09:00:00Z"),
      s("claude:q:agent-a1", "/w/app", "2026-11-09T09:10:00Z", {
        status: "running",
        parentId: "claude:q",
      }),
      // A subagent whose parent is not listed stands alone.
      s("codex:orphan", "/w/app", "2026-11-08T09:00:00Z", {
        status: "idle",
        parentId: "codex:gone",
      }),
    ];
    const tree = (m: ReturnType<typeof buildSidebar>) =>
      m.active.map((n) => [n.session.id, n.children.map((c) => c.session.id)]);
    // Only subagents at work hang under it; idle ones stay in the project.
    expect(tree(buildSidebar(list, emptyPrefs()))).toEqual([
      ["codex:p", ["codex:k1"]],
      ["claude:q", ["claude:q:agent-a1"]],
      ["codex:orphan", []],
    ]);
    // Searching for a subagent shows it under its parent.
    expect(
      tree(buildSidebar(list, emptyPrefs(), { query: "architecture" })),
    ).toEqual([["codex:p", ["codex:k1"]]]);
    // A subagent that needs you counts for its family, which comes first.
    const asking = list.map((x) =>
      x.id === "claude:q:agent-a1" ? { ...x, attention: "approval" } : x,
    );
    const m = buildSidebar(asking, emptyPrefs());
    expect(m.active[0].session.id).toBe("claude:q");
    expect(m.active.filter(needsYou).map((n) => n.session.id)).toEqual([
      "claude:q",
    ]);
    // A pinned parent keeps its subsessions.
    const pinned = buildSidebar(
      list,
      applyOp(emptyPrefs(), "pinSession", "codex:p"),
    );
    expect(pinned.pinned[0].children.map((c) => c.session.id)).toEqual(
      expect.arrayContaining(["codex:k1", "codex:k2", "codex:k3"]),
    );
    // ...and is listed there alone, not again under Active.
    expect(tree(pinned)).toEqual([
      ["claude:q", ["claude:q:agent-a1"]],
      ["codex:orphan", []],
    ]);
    // A pinned subagent leaves its active parent's family.
    expect(
      tree(
        buildSidebar(
          list,
          applyOp(emptyPrefs(), "pinSession", "claude:q:agent-a1"),
        ),
      ),
    ).toEqual([
      ["codex:p", ["codex:k1"]],
      ["codex:orphan", []],
    ]);
  });

  it("shows a session both pinned and active only under Pinned", () => {
    const m = buildSidebar(
      sessions,
      applyOp(emptyPrefs(), "pinSession", "codex:a"),
    );
    expect(m.pinned.map((n) => n.session.id)).toEqual(["codex:a"]);
    expect(m.active.map((n) => n.session.id)).toEqual([
      "claude:e",
      "claude:b",
      "claude:c",
    ]);
    // Unpinned, it is active again.
    const back = buildSidebar(
      sessions,
      applyOp(
        applyOp(emptyPrefs(), "pinSession", "codex:a"),
        "unpinSession",
        "codex:a",
      ),
    );
    expect(back.active[0].session.id).toBe("codex:a");
  });

  it("keeps pin order and puts pinned projects first", () => {
    let p = emptyPrefs();
    p = applyOp(p, "pinSession", "codex:d");
    p = applyOp(p, "pinSession", "claude:b");
    p = applyOp(p, "pinWorkspace", "/y/old");
    const m = buildSidebar(sessions, p);
    expect(m.pinned.map((n) => n.session.id)).toEqual(["codex:d", "claude:b"]);
    expect(m.projects[0]).toMatchObject({ name: "old", pinned: true });
  });

  it("keeps archived sessions out of every list unless asked for", () => {
    const list = sessions.map((x) =>
      x.id === "codex:d" || x.id === "codex:a" ? { ...x, archived: true } : x,
    );
    const p = applyOp(emptyPrefs(), "pinSession", "codex:d");
    const m = buildSidebar(list, p);
    expect(m.archivedCount).toBe(2);
    const ids = (ps: Project[]): string[] =>
      ps.flatMap((x) => [
        ...x.nodes.flatMap((n) => [
          n.session.id,
          ...n.children.map((c) => c.session.id),
        ]),
        ...ids(x.children),
      ]);
    // Its subsessions go with it.
    expect(ids(m.projects)).not.toContain("codex:a1");
    expect(ids(m.projects)).not.toContain("codex:d");
    expect(m.pinned).toEqual([]);
    expect(m.active.map((n) => n.session.id)).not.toContain("codex:a");
    expect(ids(buildSidebar(list, p, { query: "billing" }).projects)).toEqual([
      "claude:c",
    ]);
    const shown = buildSidebar(list, p, { showArchived: true });
    expect(shown.pinned.map((n) => [n.session.id, n.archived])).toEqual([
      ["codex:d", true],
    ]);
    expect(ids(shown.projects)).toEqual(
      expect.arrayContaining(["codex:a", "codex:a1", "codex:d"]),
    );
  });

  it("hides sessions everywhere unless show hidden is on", () => {
    let p = applyOp(emptyPrefs(), "pinSession", "claude:c");
    p = applyOp(p, "hide", "claude:c");
    expect(p.pinnedSessions).toEqual([]);
    const m = buildSidebar(sessions, p);
    expect(m.hiddenCount).toBe(1);
    expect(m.active.some((n) => n.session.id === "claude:c")).toBe(false);
    expect(
      m.projects
        .find((x) => x.name === "billing")!
        .nodes.map((n) => n.session.id),
    ).toEqual(["codex:d"]);
    const shown = buildSidebar(sessions, p, { showHidden: true });
    expect(
      shown.projects.find((x) => x.name === "billing")!.nodes[0],
    ).toMatchObject({ hidden: true });
    expect(applyOp(p, "unhide", "claude:c").hidden).toEqual([]);
  });

  it("applies display aliases to titles and search", () => {
    const p = { ...emptyPrefs(), aliases: { "codex:d": "Firmware fees" } };
    const m = buildSidebar(sessions, p, { query: "firmware" });
    expect(m.projects.map((x) => x.name)).toEqual(["billing"]);
    expect(m.projects[0].nodes[0].title).toBe("Firmware fees");
  });

  it("searches with multiple keywords across title, project and agent", () => {
    const m = buildSidebar(sessions, emptyPrefs(), { query: "billing 方案" });
    expect(m.searching).toBe(true);
    expect(
      m.projects.map((x) => [x.name, x.nodes.map((n) => n.session.id)]),
    ).toEqual([["billing", ["claude:c"]]]);
    expect(m.active.map((n) => n.session.id)).toEqual(["claude:c"]);
    // A matching subsession keeps its parent visible.
    const sub = buildSidebar(sessions, emptyPrefs(), { query: "explore" });
    expect(sub.projects[0].nodes[0].session.id).toBe("codex:a");
    expect(sub.projects[0].nodes[0].children.map((n) => n.session.id)).toEqual([
      "codex:a1",
    ]);
    // Empty projects disappear while searching unless the project name matches.
    expect(
      buildSidebar(sessions, emptyPrefs(), {
        query: "empty",
        workspaces: ["/z/empty"],
      }).projects.map((x) => x.name),
    ).toEqual(["empty"]);
  });

  it("formats relative times and resume commands", () => {
    const now = Date.parse("2026-11-10T12:00:00Z");
    expect(relativeTime("2026-11-10T11:59:40Z", now)).toBe("now");
    expect(relativeTime("2026-11-10T11:15:00Z", now)).toBe("45m");
    expect(relativeTime("2026-11-10T09:00:00Z", now)).toBe("3h");
    expect(relativeTime("2026-11-07T12:00:00Z", now)).toBe("3d");
    expect(resumeCommand(s("codex:t", "/w/it's", ""))).toBe(
      `cd '/w/it'\\''s' && codex resume t`,
    );
    expect(resumeCommand(s("claude:u", "/w", ""))).toBe(
      `cd '/w' && claude --resume u`,
    );
  });
});

describe("launch directory tree", () => {
  const W = "/w/example.com";
  const all = [
    s("codex:root1", W, "2026-11-10T09:00:00Z", {
      related: ["api-ctl", "api-server"],
      title: "更新同步 api-server、api-ctl",
    }),
    s("codex:root2", W, "2026-11-10T08:00:00Z"),
    s("codex:k1", W + "/api-server", "2026-11-10T11:00:00Z"),
    s("codex:k2", W + "/api-server/api-server", "2026-11-09T11:00:00Z"),
    s("codex:c1", W + "/api-ctl", "2026-11-01T11:00:00Z"),
    s("claude:a", W, "2026-11-10T07:00:00Z", { title: "Lighting demo" }),
    s("claude:a:agent-1", W, "2026-11-10T07:30:00Z", {
      parentId: "claude:a",
      title: "Port holy category",
    }),
    s("codex:exec", W + "/game-demo/build/attempt-01", "2026-11-10T10:00:00Z", {
      origin: "automation",
    }),
    s("codex:tmp", "/tmp/x", "2026-11-10T06:00:00Z", { origin: "temporary" }),
    s(
      "codex:chat",
      "/home/u/Documents/Codex/2026-10-09/chat",
      "2026-11-02T06:00:00Z",
      { origin: "chat" },
    ),
    s("codex:tools", "/w/tools", "2026-10-01T06:00:00Z"),
  ];
  const m = buildSidebar(all, emptyPrefs());
  const site = m.projects.find((p) => p.path === W)!;

  it("nests subprojects under the launch-directory parent and rolls up", () => {
    expect(m.projects.map((p) => p.name)).toEqual(["example.com", "tools"]);
    expect(site.nodes.map((n) => n.session.id)).toEqual([
      "codex:root1",
      "codex:root2",
      "claude:a",
    ]);
    expect(site.children.map((c) => c.name)).toEqual(["api-server", "api-ctl"]);
    const api = site.children[0];
    expect(api.children.map((c) => c.name)).toEqual(["api-server"]);
    expect(api.total).toBe(2);
    expect(site.total).toBe(6);
    // Recency rolls up from the newest descendant.
    expect(site.latest).toBe("2026-11-10T11:00:00Z");
  });

  it("keeps Claude subagents as children of their session", () => {
    const claude = site.nodes.find((n) => n.session.id === "claude:a")!;
    expect(claude.children.map((c) => c.title)).toEqual(["Port holy category"]);
  });

  it("separates automation, temporary and chat sessions", () => {
    expect(m.other.automated.map((n) => n.session.id)).toEqual(["codex:exec"]);
    expect(m.other.temporary.map((n) => n.session.id)).toEqual(["codex:tmp"]);
    expect(m.other.chats.map((n) => n.session.id)).toEqual(["codex:chat"]);
    expect(JSON.stringify(m.projects)).not.toContain("codex:exec");
  });

  it("links cross-project sessions into each related subproject", () => {
    const names = (p: { related: { session: Session }[] }) =>
      p.related.map((n) => n.session.id);
    expect(names(site.children.find((c) => c.name === "api-ctl")!)).toEqual([
      "codex:root1",
    ]);
    expect(names(site.children.find((c) => c.name === "api-server")!)).toEqual([
      "codex:root1",
    ]);
    // The session itself stays where it was launched.
    expect(site.nodes[0].session.id).toBe("codex:root1");
  });

  it("does not create folders that would only hold related sessions", () => {
    const only = buildSidebar(
      [
        s("codex:r", W, "2026-11-10T09:00:00Z", {
          related: ["docker_api-server"],
        }),
      ],
      emptyPrefs(),
      { workspaces: [W + "/registered"] },
    );
    expect(only.projects[0].children.map((c) => c.name)).toEqual([
      "registered",
    ]);
    expect(only.projects[0].nodes.map((n) => n.session.id)).toEqual([
      "codex:r",
    ]);
    // A registered folder whose only content is related sessions is dropped too,
    // unless the user pinned it.
    const withRelated = (prefs = emptyPrefs()) =>
      buildSidebar(
        [s("codex:r", W, "2026-11-10T09:00:00Z", { related: ["registered"] })],
        prefs,
        { workspaces: [W + "/registered"] },
      ).projects[0].children.map((c) => c.name);
    expect(withRelated()).toEqual([]);
    expect(
      withRelated(applyOp(emptyPrefs(), "pinWorkspace", W + "/registered")),
    ).toEqual(["registered"]);
  });

  it("finds sessions in nested subprojects and by related project name", () => {
    const deep = buildSidebar(all, emptyPrefs(), { query: "k2" });
    expect(deep.projects.map((p) => p.name)).toEqual(["example.com"]);
    expect(deep.projects[0].nodes).toEqual([]);
    expect(
      deep.projects[0].children[0].children[0].nodes.map((n) => n.session.id),
    ).toEqual(["codex:k2"]);
    const rel = buildSidebar(all, emptyPrefs(), { query: "api-ctl" });
    expect(rel.projects[0].nodes.map((n) => n.session.id)).toEqual([
      "codex:root1",
    ]);
  });

  it("sorts pinned child projects first among siblings", () => {
    const p = applyOp(emptyPrefs(), "pinWorkspace", W + "/api-ctl");
    expect(
      buildSidebar(all, p).projects[0].children.map((c) => c.name),
    ).toEqual(["api-ctl", "api-server"]);
  });
});

describe("implicit folders", () => {
  it("groups sibling run directories under their shared prefix", () => {
    const W = "/w/example.com";
    const runs = ["lamp", "snow", "timber"].map((n, i) =>
      s(
        "codex:" + n,
        `${W}/game-demo/build/s21-imagegen/${n}/attempt-01`,
        `2026-11-0${i + 1}T00:00:00Z`,
      ),
    );
    const m = buildSidebar(
      [
        s("codex:root", W, "2026-11-10T00:00:00Z"),
        s("codex:solo", `${W}/tools/x`, "2026-11-01T00:00:00Z"),
        ...runs,
      ],
      emptyPrefs(),
    );
    const site = m.projects[0];
    expect(site.children.map((c) => c.name)).toEqual([
      "game-demo/build/s21-imagegen",
      "tools/x",
    ]);
    const group = site.children[0];
    expect(group.total).toBe(3);
    expect(group.children.map((c) => c.name)).toEqual([
      "timber/attempt-01",
      "snow/attempt-01",
      "lamp/attempt-01",
    ]);
  });
});

describe("unified ordering", () => {
  const W = "/w/app";
  const list = [
    s("codex:old", W, "2026-11-01T00:00:00Z"),
    s("codex:new", W, "2026-11-10T00:00:00Z"),
    s("codex:mid", W, "2026-11-05T00:00:00Z"),
    s("codex:busy-parent", W, "2026-10-01T00:00:00Z"),
    s("codex:kid", W, "2026-11-09T00:00:00Z", {
      parentId: "codex:busy-parent",
    }),
    s("codex:f1", W + "/folder-a", "2026-11-06T00:00:00Z"),
    s("codex:f2", W + "/folder-b", "2026-10-15T00:00:00Z"),
  ];
  const labels = (p: Project) =>
    p.entries.map((e) =>
      e.kind === "project" ? "dir:" + e.project.name : e.node.session.id,
    );

  it("interleaves folders and sessions by most recent activity", () => {
    const app = buildSidebar(list, emptyPrefs()).projects[0];
    expect(labels(app)).toEqual([
      "codex:new",
      "codex:busy-parent",
      "dir:folder-a",
      "codex:mid",
      "codex:old",
      "dir:folder-b",
    ]);
    expect(app.latest).toBe("2026-11-10T00:00:00Z");
  });

  it("keeps pinned folders first", () => {
    const p = applyOp(emptyPrefs(), "pinWorkspace", W + "/folder-b");
    expect(labels(buildSidebar(list, p).projects[0])[0]).toBe("dir:folder-b");
  });

  it("keeps the Related list on folders that have their own sessions", () => {
    const m = buildSidebar(
      [
        ...list,
        s("codex:x", W, "2026-11-08T00:00:00Z", { related: ["folder-a"] }),
      ],
      emptyPrefs(),
    );
    const a = m.projects[0].children.find((c) => c.name === "folder-a")!;
    expect(a.related.map((n) => n.session.id)).toEqual(["codex:x"]);
  });
});
