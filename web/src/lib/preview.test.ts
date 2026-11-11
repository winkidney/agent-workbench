import { describe, expect, it } from "vitest";
import { canonicalAddress, fileFrameSandbox, previewMode } from "./preview";

const file = {
  url: "http://p-0123abcd.agent.localhost:17880",
  fileUrl: "/__files/abc/report/index.html",
};
const service = { url: "http://p-0123abcd.agent.localhost:17880" };

describe("previewMode", () => {
  it("uses the preview origin on the workbench computer's own address", () => {
    expect(previewMode(file, "agent.localhost:17880")).toBe("origin");
    expect(previewMode(service, "AGENT.localhost:17880")).toBe("origin");
  });
  it("opens files on the workbench's address everywhere else", () => {
    for (const host of [
      "ai.example.com",
      "192.168.1.5:17880",
      "localhost:17880",
    ])
      expect(previewMode(file, host)).toBe("file");
  });
  it("cannot show dev servers away from the workbench computer", () => {
    expect(previewMode(service, "ai.example.com")).toBe("unavailable");
    expect(canonicalAddress(service)).toBe("http://agent.localhost:17880");
  });
});

describe("fileFrameSandbox", () => {
  it("sandboxes everything but PDFs, never with the workbench's origin", () => {
    expect(fileFrameSandbox("/report/index.html")).toMatch(/allow-scripts/);
    expect(fileFrameSandbox("/report/index.html")).not.toMatch(/same-origin/);
    expect(fileFrameSandbox("/doc.PDF")).toBeUndefined();
  });
});
