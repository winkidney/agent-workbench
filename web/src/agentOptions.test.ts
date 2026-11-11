import { describe, expect, it } from "vitest";
import {
  defaultLabel,
  findModel,
  modelName,
  optionName,
  resolveOption,
} from "./agentOptions";

const claude = [
  {
    value: "default",
    displayName: "Default (recommended)",
    resolvedModel: "claude-opus-5-5",
  },
  { value: "opus", displayName: "Opus 5.5", resolvedModel: "claude-opus-5-5" },
  {
    value: "sonnet",
    displayName: "Sonnet 5.5",
    resolvedModel: "claude-sonnet-5-5",
  },
];
const codex = [
  { model: "gpt-a", displayName: "GPT-A" },
  { model: "gpt-b", displayName: "GPT-B", isDefault: true },
];

describe("resolveOption", () => {
  it("prefers the override, then live values, then defaults", () => {
    const defaults = { effort: "low", speed: "default" };
    const live = { effort: "high" };
    expect(resolveOption("effort", { effort: "max" }, live, defaults)).toBe(
      "max",
    );
    expect(resolveOption("effort", { effort: "" }, live, defaults)).toBe(
      "high",
    );
    expect(resolveOption("speed", {}, live, defaults)).toBe("default");
    expect(resolveOption("mode", {}, live, defaults)).toBe("");
    expect(resolveOption("mode", undefined, undefined)).toBe("");
  });
});

describe("models", () => {
  it("matches Claude's resolved name to the named entry, not the alias", () => {
    expect(findModel(claude, "claude-opus-5-5")?.value).toBe("opus");
    expect(findModel(claude, "sonnet")?.value).toBe("sonnet");
    expect(findModel(codex, "")?.model).toBe("gpt-b");
    expect(findModel(codex, "unknown")).toBeUndefined();
  });
  it("names the default alias after what it resolves to", () => {
    expect(modelName(claude, "default")).toBe("Opus 5.5");
    expect(modelName(claude, "claude-opus-5-5")).toBe("Opus 5.5");
    expect(modelName(codex, "gpt-a")).toBe("GPT-A");
    expect(modelName(codex, "gpt-z")).toBe("gpt-z");
  });
});

describe("optionName", () => {
  it("uses short names, then option labels, then the raw value", () => {
    expect(optionName("permissions", ":workspace")).toBe("Workspace");
    expect(optionName("permissions", "manual")).toBe("Ask");
    expect(optionName("speed", "default")).toBe("Standard");
    expect(optionName("speed", "priority")).toBe("Fast");
    expect(
      optionName("mode", "default", [{ value: "default", label: "Execute" }]),
    ).toBe("Execute");
    expect(optionName("effort", "low")).toBe("low");
    expect(optionName("effort", "")).toBe("");
  });
  it("labels the clearing entry with the value it falls back to", () => {
    expect(defaultLabel("low")).toBe("Default (low)");
    expect(defaultLabel("")).toBe("Default");
    expect(defaultLabel("Default")).toBe("Default");
  });
});
