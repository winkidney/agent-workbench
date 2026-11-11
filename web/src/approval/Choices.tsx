// A numbered list of choices, as the CLIs show them.
export function Choices({
  options,
  selected,
  onChoose,
}: {
  options: { label: string; hint?: string }[];
  selected: number;
  onChoose: (i: number) => void;
}) {
  return (
    <div className="approval-options" role="listbox" aria-label="Choices">
      {options.map((o, i) => (
        <button
          key={o.label}
          type="button"
          role="option"
          aria-selected={i === selected}
          className={"approval-option" + (i === selected ? " selected" : "")}
          onClick={() => onChoose(i)}
        >
          <span className="pointer">{i === selected ? "›" : " "}</span>
          <span className="num">{i + 1}.</span>
          <span>{o.label}</span>
          {o.hint && <span className="hint">{o.hint}</span>}
        </button>
      ))}
    </div>
  );
}
