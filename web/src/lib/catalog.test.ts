import { describe, expect, it } from "vitest";
import { agentsOf, catalogName } from "./assistants";

describe("catalogs", () => {
  it("names a catalog as people say it", () => {
    expect(
      catalogName(
        "https://github.com/winkidney/agent-workbench-assistants.git",
      ),
    ).toBe("winkidney/agent-workbench-assistants");
    expect(catalogName("file:///tmp/cat")).toBe("/tmp/cat");
  });
  it("offers the agents its author made it for, or both", () => {
    expect(agentsOf({})).toEqual(["claude", "codex"]);
    expect(agentsOf({ agents: [] })).toEqual(["claude", "codex"]);
    expect(agentsOf({ agents: ["codex"] })).toEqual(["codex"]);
  });
});
