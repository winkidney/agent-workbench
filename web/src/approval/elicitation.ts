import { list, record, text } from "../transcript/model";

// MCP elicitation: an MCP server asks the user for input through the agent,
// as a small form, a page to open, or something this page cannot do.
// Codex sends it as mcpServer/elicitation/request, Claude as an
// "elicitation" control request; the fields follow MCP's flat form schema.

export type Field = {
  name: string;
  title: string;
  description?: string;
  type: "text" | "number" | "integer" | "boolean" | "select" | "multi";
  format?: string;
  options?: { value: string; label: string }[];
  required: boolean;
  initial?: string | number | boolean | string[];
  min?: number;
  max?: number;
};

export type Elicitation = {
  server: string;
  message: string;
  title?: string;
  mode: "form" | "url" | "unsupported";
  url?: string;
  fields: Field[];
};

function options(schema: Record<string, unknown>) {
  const titled = list(schema.oneOf ?? schema.anyOf).map(record);
  if (titled.length)
    return titled.map((o) => ({
      value: text(o.const),
      label: text(o.title) || text(o.const),
    }));
  const names = list(schema.enumNames).map(text);
  return list(schema.enum)
    .map(text)
    .map((v, i) => ({ value: v, label: names[i] || v }));
}

export function fields(
  schema: Record<string, unknown>,
  order?: string[],
): Field[] {
  const props = record(schema.properties);
  const required = new Set(list(schema.required).map(text));
  const names = order?.length ? order : Object.keys(props);
  return names
    .filter((n) => n in props)
    .map((name) => {
      const p = record(props[name]);
      const base = {
        name,
        title: text(p.title) || name,
        description: text(p.description) || undefined,
        required: required.has(name),
      };
      const type = text(p.type);
      if (type === "boolean")
        return {
          ...base,
          type: "boolean" as const,
          initial: typeof p.default === "boolean" ? p.default : false,
        };
      if (type === "number" || type === "integer")
        return {
          ...base,
          type: type as "number" | "integer",
          initial: typeof p.default === "number" ? p.default : undefined,
          min: typeof p.minimum === "number" ? p.minimum : undefined,
          max: typeof p.maximum === "number" ? p.maximum : undefined,
        };
      if (type === "array")
        return {
          ...base,
          type: "multi" as const,
          options: options(record(p.items)),
          initial: list(p.default).map(text),
        };
      const opts = options(p);
      if (opts.length)
        return {
          ...base,
          type: "select" as const,
          options: opts,
          initial: text(p.default) || undefined,
        };
      return {
        ...base,
        type: "text" as const,
        format: text(p.format) || undefined,
        initial: text(p.default) || undefined,
      };
    });
}

// The request, from either agent.
export function parseElicitation(
  agent: string,
  request: unknown,
): Elicitation | undefined {
  const v = record(request);
  if (agent === "claude") {
    const r = record(v.request);
    if (r.subtype !== "elicitation") return undefined;
    const mode = text(r.mode) || "form";
    return {
      server: text(r.display_name) || text(r.mcp_server_name),
      message: text(r.message),
      title: text(r.title) || undefined,
      mode: mode === "url" ? "url" : mode === "form" ? "form" : "unsupported",
      url: text(r.url) || undefined,
      fields:
        mode === "form"
          ? fields(record(r.requested_schema), list(r.property_order).map(text))
          : [],
    };
  }
  if (text(v.method) !== "mcpServer/elicitation/request") return undefined;
  const p = record(v.params);
  const mode = text(p.mode);
  const form =
    mode === "form" || mode === "openai/form" || mode === "openaiForm";
  const schema = record(p.requestedSchema);
  return {
    server: text(p.serverName),
    message: text(p.message) || text(p.description),
    title: text(p.title) || undefined,
    mode:
      mode === "url"
        ? "url"
        : form && Object.keys(record(schema.properties)).length
          ? "form"
          : "unsupported",
    url: text(p.url) || undefined,
    fields: form ? fields(schema) : [],
  };
}

// What the form sends back, in each field's type; undefined values are left
// out, and a missing required field makes it incomplete.
export function content(
  form: Field[],
  values: Record<string, unknown>,
): { content: Record<string, unknown>; complete: boolean } {
  const out: Record<string, unknown> = {};
  let complete = true;
  for (const f of form) {
    const v = values[f.name] ?? f.initial;
    let value: unknown;
    if (f.type === "boolean") value = v === true;
    else if (f.type === "number" || f.type === "integer") {
      const n = typeof v === "number" ? v : Number(String(v ?? "").trim());
      if (String(v ?? "").trim() !== "" && Number.isFinite(n))
        value = f.type === "integer" ? Math.trunc(n) : n;
    } else if (f.type === "multi") {
      const picked = Array.isArray(v) ? v.map(String) : [];
      if (picked.length) value = picked;
    } else if (typeof v === "string" && v.trim() !== "") value = v;
    if (value === undefined) {
      if (f.required) complete = false;
    } else out[f.name] = value;
  }
  return { content: out, complete };
}
