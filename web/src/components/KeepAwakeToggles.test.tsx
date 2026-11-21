import { describe, expect, it } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";
import { awakeHint, KeepAwakeToggles } from "./KeepAwakeToggles";

describe("keep-awake switches", () => {
  it("say what the computer holds", () => {
    expect(awakeHint(false, undefined, "Sleeps as usual")).toBe(
      "Sleeps as usual",
    );
    expect(awakeHint(true, { on: true, via: "logind" }, "x")).toBe(
      "On · via logind",
    );
    expect(awakeHint(true, { on: true }, "x")).toBe("On");
    expect(awakeHint(true, { on: false, error: "no session bus" }, "x")).toBe(
      "no session bus",
    );
    // Switched on, before the server answered.
    expect(awakeHint(true, undefined, "x")).toBe("Taking effect…");
    // An error from before the switch went off is not shown.
    expect(awakeHint(false, { on: false, error: "old" }, "Off")).toBe("Off");
  });

  it("are two switches, waiting for the preferences", () => {
    const html = renderToStaticMarkup(<KeepAwakeToggles />);
    expect(html.match(/role="switch"/g)).toHaveLength(2);
    expect(html).toContain("Prevent system sleep");
    expect(html).toContain("Prevent screen lock");
    expect(html).toContain("Loading…");
    expect(html).toContain("Applies to the computer Agent Workbench runs on.");
  });
});
