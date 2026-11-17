import { describe, expect, it } from "vitest";
import { fenced, languageOf } from "./TextView";

describe("a text file shown in the panel", () => {
  it("is one fence longer than any backticks in it", () => {
    expect(fenced("a\n", "go")).toBe("```go\na\n```");
    expect(fenced("x ``` y\n````", "")).toBe("`````\nx ``` y\n````\n`````");
  });
  it("is highlighted by what it is", () => {
    expect(languageOf("src/main.go")).toBe("go");
    expect(languageOf("events.jsonl")).toBe("json");
    expect(languageOf("Makefile")).toBe("makefile");
    expect(languageOf("run.log")).toBe("");
    expect(languageOf("LICENSE")).toBe("");
  });
});
