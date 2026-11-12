import { useState } from "react";
import { ExternalLink } from "lucide-react";
import { Button } from "../components/ui/button";
import { content, type Elicitation, type Field } from "./elicitation";

function Input({
  field,
  value,
  onChange,
}: {
  field: Field;
  value: unknown;
  onChange: (v: unknown) => void;
}) {
  const id = "elicit-" + field.name;
  if (field.type === "boolean")
    return (
      <label className="elicit-check">
        <input
          type="checkbox"
          checked={value === true}
          onChange={(e) => onChange(e.target.checked)}
        />
        <span>{field.title}</span>
      </label>
    );
  if (field.type === "select" || field.type === "multi") {
    const multi = field.type === "multi";
    const picked = multi
      ? Array.isArray(value)
        ? (value as string[])
        : []
      : [String(value ?? "")];
    return (
      <div className="elicit-options" role={multi ? "group" : "radiogroup"}>
        {field.options!.map((o) => {
          const on = picked.includes(o.value);
          return (
            <button
              key={o.value}
              type="button"
              className={"approval-option" + (on ? " selected" : "")}
              aria-pressed={on}
              onClick={() =>
                onChange(
                  multi
                    ? on
                      ? picked.filter((x) => x !== o.value)
                      : [...picked, o.value]
                    : on && !field.required
                      ? undefined
                      : o.value,
                )
              }
            >
              <span className="pointer">
                {multi ? (on ? "☒" : "☐") : on ? "◉" : "○"}
              </span>
              <span>{o.label}</span>
            </button>
          );
        })}
      </div>
    );
  }
  const type =
    field.type === "number" || field.type === "integer"
      ? "number"
      : field.format === "email"
        ? "email"
        : field.format === "uri"
          ? "url"
          : field.format === "date"
            ? "date"
            : field.format === "date-time"
              ? "datetime-local"
              : "text";
  return (
    <input
      id={id}
      type={type}
      step={field.type === "integer" ? 1 : undefined}
      min={field.min}
      max={field.max}
      required={field.required}
      value={value === undefined ? "" : String(value)}
      onChange={(e) => onChange(e.target.value)}
    />
  );
}

// An MCP server's request for input: its form, a page to open, or (for
// what this page cannot do) a way to decline.
export function ElicitationForm({
  elicitation: e,
  busy,
  onAccept,
  onDecline,
  onCancel,
}: {
  elicitation: Elicitation;
  busy: boolean;
  onAccept: (content: Record<string, unknown> | null) => void;
  onDecline: () => void;
  onCancel: () => void;
}) {
  const [values, setValues] = useState<Record<string, unknown>>({});
  const [opened, setOpened] = useState(false);
  const filled = content(e.fields, values);
  const actions = (accept?: React.ReactNode) => (
    <div className="approval-actions">
      {accept}
      <Button
        type="button"
        size="sm"
        variant="outline"
        disabled={busy}
        onClick={onDecline}
      >
        Decline
      </Button>
      <Button
        type="button"
        size="sm"
        variant="ghost"
        disabled={busy}
        onClick={onCancel}
      >
        Cancel
      </Button>
    </div>
  );
  return (
    <div className="elicitation">
      {e.title && <p className="elicit-title">{e.title}</p>}
      {e.message && <p className="elicit-message">{e.message}</p>}
      {e.mode === "url" && e.url && (
        <>
          <a
            className="elicit-url"
            href={e.url}
            target="_blank"
            rel="noreferrer noopener"
            onClick={() => setOpened(true)}
          >
            <ExternalLink size={13} />
            {e.url}
          </a>
          {actions(
            <Button
              type="button"
              size="sm"
              disabled={busy}
              onClick={() => onAccept(null)}
            >
              {opened ? "Done" : "Continue without opening"}
            </Button>,
          )}
        </>
      )}
      {e.mode === "form" && (
        <form
          onSubmit={(ev) => {
            ev.preventDefault();
            if (filled.complete) onAccept(filled.content);
          }}
        >
          {e.fields.map((f) => (
            <div key={f.name} className="elicit-field">
              {f.type !== "boolean" && (
                <label htmlFor={"elicit-" + f.name}>
                  {f.title}
                  {f.required && <span aria-hidden> *</span>}
                </label>
              )}
              <Input
                field={f}
                value={values[f.name] ?? f.initial}
                onChange={(v) => setValues((old) => ({ ...old, [f.name]: v }))}
              />
              {f.description && <small>{f.description}</small>}
            </div>
          ))}
          {actions(
            <Button type="submit" size="sm" disabled={busy || !filled.complete}>
              Submit
            </Button>,
          )}
        </form>
      )}
      {e.mode === "unsupported" && (
        <>
          <p className="subtle">
            This request needs the agent&apos;s own app; decline it here, or
            answer it there.
          </p>
          {actions()}
        </>
      )}
    </div>
  );
}
