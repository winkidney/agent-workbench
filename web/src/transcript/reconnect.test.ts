import { describe, expect, it } from "vitest";
import { fromHistory } from "./reducer";
import { reconnecting } from "./codex";

const error = (willRetry: boolean, message: string, details?: string) => ({
  seq: 7,
  session: "s",
  kind: "native",
  time: "",
  data: {
    method: "error",
    params: {
      threadId: "t",
      turnId: "u",
      willRetry,
      error: { message, additionalDetails: details },
    },
  },
});

describe("Codex reconnecting", () => {
  it("is a passing notice, not a failure, while Codex will retry", () => {
    const s = fromHistory(
      [
        error(
          true,
          "Reconnecting... 1/5",
          "Selected model is at capacity. Please try a different model.",
        ),
      ],
      "codex",
      false,
    );
    const b = [...s.blocks.values()][0];
    expect(b).toMatchObject({
      kind: "notice",
      tone: "warn",
      text: "Reconnecting to the model (1/5) · Selected model is at capacity",
    });
  });

  it("stays an error when Codex gives up", () => {
    const s = fromHistory(
      [error(false, "stream disconnected before completion")],
      "codex",
      false,
    );
    expect([...s.blocks.values()][0]).toMatchObject({
      kind: "error",
      text: "stream disconnected before completion",
    });
  });

  it("says the attempt and the reason in words", () => {
    expect(reconnecting({}, "Reconnecting... 2/5")).toBe(
      "Reconnecting to the model (2/5)",
    );
    expect(reconnecting({ additionalDetails: "Overloaded." }, "x")).toBe(
      "Reconnecting to the model · Overloaded",
    );
  });
});
