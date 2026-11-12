// The Agents settings (internal/workbench/agents_api.go): which program runs
// each agent and where it keeps its data, and signing a CLI in from here.

export type AgentId = "claude" | "codex";
export const AGENT_IDS: AgentId[] = ["claude", "codex"];

export type SignIn = {
  checked: boolean;
  signedIn: boolean;
  method?: string;
  account?: string;
  error?: string;
};

export type LoginState = {
  state:
    | "none"
    | "starting"
    | "waiting"
    | "verifying"
    | "done"
    | "failed"
    | "cancelled"
    | "expired";
  url?: string;
  code?: string;
  needsCode?: boolean;
  message?: string;
  started?: string;
};

export type AgentView = {
  program: {
    value?: string;
    source: "setting" | "path";
    path?: string;
    version?: string;
    error?: string;
    candidates: string[];
  };
  home: {
    value?: string;
    source: "setting" | "environment" | "default";
    path: string;
    env?: string;
    default: string;
    exists: boolean;
    error?: string;
    signIn: SignIn;
    loginCommand: string;
  };
  busy: number;
  login?: LoginState;
};

export type AgentsView = Record<AgentId, AgentView>;
export type AgentSetup = { path?: string; home?: string };
export type AgentsSetup = Record<AgentId, AgentSetup>;

// What Settings saved, as the form edits it.
export function setupOf(view: AgentsView): AgentsSetup {
  return {
    claude: {
      path: view.claude.program.value ?? "",
      home: view.claude.home.value ?? "",
    },
    codex: {
      path: view.codex.program.value ?? "",
      home: view.codex.home.value ?? "",
    },
  };
}

const trim = (s?: string) => (s ?? "").trim();

export function sameSetup(a: AgentsSetup, b: AgentsSetup): boolean {
  return AGENT_IDS.every(
    (id) =>
      trim(a[id].path) === trim(b[id].path) &&
      trim(a[id].home) === trim(b[id].home),
  );
}

// The automatic home, in words.
export function automaticHome(home: AgentView["home"]): string {
  return home.env
    ? `From the environment: ${home.env}`
    : `Default: ${home.default}`;
}

// What the CLI says of its login.
export function signInText(s: SignIn | undefined): string {
  if (!s || (!s.checked && !s.error)) return "";
  if (s.error) return s.error;
  if (!s.signedIn) return "Not signed in";
  return ["Signed in", s.method, s.account].filter(Boolean).join(" · ");
}

export const LOGIN_METHODS = [
  { id: "claudeai", label: "Claude subscription" },
  { id: "console", label: "Anthropic Console (API billing)" },
  { id: "sso", label: "Single sign-on" },
];

// Whether a login still needs the page.
export function loginActive(l?: LoginState): boolean {
  return (
    !!l &&
    (l.state === "starting" || l.state === "waiting" || l.state === "verifying")
  );
}

export function loginText(l: LoginState): string {
  switch (l.state) {
    case "starting":
      return "Starting the sign-in…";
    case "waiting":
      return l.needsCode
        ? "Open the sign-in page, sign in, then paste the code it shows."
        : "Open the link on any device and enter the code.";
    case "verifying":
      return "Checking the code…";
    case "done":
      return "Signed in.";
    case "cancelled":
      return "Sign-in cancelled.";
    case "expired":
      return l.message || "The sign-in expired.";
    case "failed":
      return l.message || "The sign-in failed.";
  }
  return "";
}
