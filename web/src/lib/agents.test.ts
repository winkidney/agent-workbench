import { describe, expect, it } from "vitest";
import {
  automaticHome,
  loginActive,
  loginText,
  sameSetup,
  setupOf,
  signInText,
  type AgentView,
  type AgentsView,
} from "./agents";

const view = (
  over: Partial<AgentView["home"]> = {},
  value = "",
): AgentView => ({
  program: { source: value ? "setting" : "path", value, candidates: [] },
  home: {
    source: "default",
    path: "/home/u/.claude",
    default: "/home/u/.claude",
    exists: true,
    signIn: { checked: true, signedIn: false },
    loginCommand: "claude auth login",
    ...over,
  },
  busy: 0,
});

describe("agents settings", () => {
  it("edits what Settings saved, and tells a change apart", () => {
    const v: AgentsView = {
      claude: view({ value: "/x/claude", source: "setting" }, "/bin/c"),
      codex: view(),
    };
    const s = setupOf(v);
    expect(s).toEqual({
      claude: { path: "/bin/c", home: "/x/claude" },
      codex: { path: "", home: "" },
    });
    expect(sameSetup(s, { ...s, codex: { path: " ", home: "" } })).toBe(true);
    expect(sameSetup(s, { ...s, codex: { path: "/c", home: "" } })).toBe(false);
  });

  it("names the automatic home and the sign-in", () => {
    expect(automaticHome(view().home)).toBe("Default: /home/u/.claude");
    expect(automaticHome(view({ env: "/env/claude" }).home)).toBe(
      "From the environment: /env/claude",
    );
    expect(signInText({ checked: true, signedIn: false })).toBe(
      "Not signed in",
    );
    expect(
      signInText({
        checked: true,
        signedIn: true,
        method: "claude.ai (max)",
        account: "me@example.com",
      }),
    ).toBe("Signed in · claude.ai (max) · me@example.com");
    expect(signInText({ checked: false, signedIn: false })).toBe("");
  });

  it("says what a sign-in waits for", () => {
    expect(loginActive({ state: "waiting" })).toBe(true);
    expect(loginActive({ state: "done" })).toBe(false);
    expect(loginText({ state: "waiting", needsCode: true })).toContain(
      "paste the code",
    );
    expect(loginText({ state: "waiting", code: "ABCD-1234" })).toContain(
      "enter the code",
    );
    expect(
      loginText({ state: "failed", message: "That code wasn't accepted." }),
    ).toBe("That code wasn't accepted.");
  });
});
