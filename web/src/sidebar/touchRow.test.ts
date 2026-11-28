import { describe, expect, it } from "vitest";
import styleCss from "../style.css?raw";

// On a touch screen the selected row always shows its ⋯. Floating over the
// status column (as it does on hover), it hid the bell of the row the user
// was in, so "1 needs you" pointed at nothing: there it sits after it.
describe("the selected row on a touch screen", () => {
  it("puts its actions after its status instead of over it", () => {
    const touch = styleCss.match(/@media \(hover: none\) \{([\s\S]*?)\n\}/);
    expect(touch).not.toBeNull();
    const rule = touch![1].match(
      /\.session-row\.selected \.side-actions \{([^}]*)\}/,
    );
    expect(rule).not.toBeNull();
    expect(rule![1]).toMatch(/position:\s*static/);
    expect(rule![1]).toMatch(/background:\s*none/);
  });
});
