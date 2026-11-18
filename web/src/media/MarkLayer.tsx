import { FONT, arrowHead, halo, penPath, type Mark } from "./markup";
import type { Size, View } from "./zoom";

// The marks over the image, drawn in its own pixels and moved with it.
export function MarkLayer({
  marks,
  natural,
  view,
}: {
  marks: Mark[];
  natural: Size;
  view: View;
}) {
  return (
    <svg
      className="viewer-markup"
      width={natural.w}
      height={natural.h}
      viewBox={`0 0 ${natural.w} ${natural.h}`}
      style={{
        transform: `translate(${view.x}px, ${view.y}px) scale(${view.s})`,
      }}
      aria-hidden="true"
    >
      {marks.map((m, i) => (
        <MarkShape key={i} mark={m} />
      ))}
    </svg>
  );
}

function MarkShape({ mark: m }: { mark: Mark }) {
  if (m.kind === "rect")
    return (
      <rect
        data-mark="rect"
        x={m.x}
        y={m.y}
        width={m.w}
        height={m.h}
        fill="none"
        stroke={m.color}
        strokeWidth={m.width}
        strokeLinejoin="round"
      />
    );
  if (m.kind === "arrow") {
    const { points: p, lineEnd } = arrowHead(m.x1, m.y1, m.x2, m.y2, m.width);
    return (
      <g data-mark="arrow">
        <line
          x1={m.x1}
          y1={m.y1}
          x2={lineEnd.x}
          y2={lineEnd.y}
          stroke={m.color}
          strokeWidth={m.width}
          strokeLinecap="round"
        />
        <polygon points={p.join(" ")} fill={m.color} />
      </g>
    );
  }
  if (m.kind === "pen")
    return (
      <path
        data-mark="pen"
        d={penPath(m.points)}
        fill="none"
        stroke={m.color}
        strokeWidth={m.width}
        strokeLinecap="round"
        strokeLinejoin="round"
      />
    );
  return (
    <text
      data-mark="text"
      x={m.x}
      y={m.y}
      fill={m.color}
      stroke={halo(m.color)}
      strokeWidth={Math.max(2, m.size / 6)}
      strokeLinejoin="round"
      paintOrder="stroke"
      dominantBaseline="text-before-edge"
      style={{ font: FONT.replace("{size}", String(m.size)) }}
    >
      {m.text}
    </text>
  );
}
