import { describe, expect, it } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";
import { RecordMissing } from "./RecordMissing";

describe("a conversation whose record is gone", () => {
  it("says so and shows what remains", () => {
    const html = renderToStaticMarkup(
      <RecordMissing
        remains={{
          firstMessage: "an old question",
          tokensUsed: 12345,
          created: "2026-07-01T00:00:00Z",
        }}
      />,
    );
    expect(html).toContain("record is gone");
    expect(html).toContain("an old question");
    expect(html).toContain("12,345 tokens");
  });
});
