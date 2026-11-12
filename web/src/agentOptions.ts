export type Options = {
  model?: string;
  effort?: string;
  speed?: string;
  mode?: string;
  permissions?: string;
};
export type OptionItem = { value: string; label: string; disabled?: boolean };
export type ModelRef = {
  value?: string;
  model?: string;
  id?: string;
  resolvedModel?: string;
  isDefault?: boolean;
  displayName: string;
};

// The value a setting resolves to: the first layer that has one, in the order
// given (override, then live engine, then the CLI's configured default).
export function resolveOption(
  key: keyof Options,
  ...layers: (Options | undefined)[]
): string {
  for (const layer of layers) {
    const v = layer?.[key];
    if (v) return v;
  }
  return "";
}

export const modelValue = (m: ModelRef) => m.value ?? m.model ?? m.id ?? "";

// The catalog entry for a model id or a resolved model name. Claude reports
// the resolved name (claude-opus-5-5), which both "default" and "opus" map
// to; prefer the named entry over the alias.
export function findModel<M extends ModelRef>(
  models: M[] | undefined,
  id: string,
): M | undefined {
  if (!models?.length) return undefined;
  if (!id) return models.find((m) => m.isDefault) ?? models[0];
  return (
    models.find((m) => modelValue(m) === id) ??
    models.find((m) => m.resolvedModel === id && modelValue(m) !== "default") ??
    models.find((m) => m.resolvedModel === id)
  );
}

// Short display name for a model; the "Default" alias shows what it resolves to.
export function modelName(models: ModelRef[] | undefined, id: string): string {
  const m = findModel(models, id);
  if (!m) return id;
  const name = m.displayName.replace(/\s*\(recommended\)/i, "");
  if (/^default$/i.test(name) && m.resolvedModel) {
    const named = models?.find(
      (o) => o !== m && o.resolvedModel === m.resolvedModel,
    );
    if (named) return named.displayName;
  }
  return name;
}

const NAMES: Partial<Record<keyof Options, Record<string, string>>> = {
  permissions: {
    ":read-only": "Read only",
    ":workspace": "Workspace",
    "auto-review": "Auto-review",
    ":danger-full-access": "Full access",
    manual: "Ask",
    acceptEdits: "Accept edits",
    auto: "Auto",
    dontAsk: "Don't ask",
    bypassPermissions: "Bypass",
  },
  speed: {
    default: "Standard",
    standard: "Standard",
    priority: "Fast",
    fast: "Fast",
  },
};

// Display name for a setting value: a known short name, then the option's own
// label, then the raw value.
export function optionName(
  key: keyof Options,
  value: string,
  items: OptionItem[] = [],
): string {
  if (!value) return "";
  return (
    NAMES[key]?.[value] ?? items.find((i) => i.value === value)?.label ?? value
  );
}

// Text for the dropdown entry that clears the override.
export const defaultLabel = (name: string) =>
  name && !/^default$/i.test(name) ? `Default (${name})` : "Default";
