import { AgentIcon } from "./AgentIcon";
import { MOSS_CLAUDE_16, MOSS_CLAUDE_32, MOSS_COLORS } from "./MossIcon";
import { ICON_COLORS, ICON_DRAWINGS } from "./assistantIcons";
import { Pixels, PixelSvg } from "./pixel";

// An assistant's icon (ADR 0030): a built-in drawing or the image in its
// folder, square, with the agent it runs on as a badge in the bottom-right
// corner (the agent's mark from 48 pixels up; a dot in its colour below).

export const DEFAULT_ICON = "camera";

// The built-in icons, in the order Settings offers them.
export const ICONS: { name: string; label: string }[] = [
  { name: DEFAULT_ICON, label: "Camera" },
  ...Object.entries(ICON_DRAWINGS).map(([name, d]) => ({
    name,
    label: d.label,
  })),
];

export const AGENT_NAME: Record<string, string> = {
  claude: "Claude",
  codex: "Codex",
};

// The image an assistant's folder holds, loaded again when it changes.
export const iconURL = (dir: string, version?: number) =>
  `/api/assistants/${encodeURIComponent(dir)}/icon?v=${version ?? 0}`;

// The camera with a blank screen: the badge alone names the agent, so the
// drawing is the same whichever it runs on.
const CAMERA_COLORS = { ...MOSS_COLORS, C: "#262b36", N: "#262b36" };

function Drawing({ icon, size }: { icon: string; size: number }) {
  const n = size >= 32 ? 32 : 16;
  const drawing = ICON_DRAWINGS[icon];
  const grid = drawing
    ? n === 32
      ? drawing.g32
      : drawing.g16
    : n === 32
      ? MOSS_CLAUDE_32
      : MOSS_CLAUDE_16;
  return (
    <PixelSvg w={n} h={n} scale={size / n}>
      <Pixels grid={grid} palette={drawing ? ICON_COLORS : CAMERA_COLORS} />
    </PixelSvg>
  );
}

export function AssistantIcon({
  icon,
  src,
  agent,
  size = 16,
  name,
  decorative,
  badge: showBadge = true,
}: {
  // A built-in drawing's name; another or none is the camera.
  icon?: string;
  // The uploaded image, in place of a drawing.
  src?: string;
  agent: string;
  // 16, or a multiple of 32.
  size?: number;
  name?: string;
  decorative?: boolean;
  // Off where only the drawings are compared (Settings' choice).
  badge?: boolean;
}) {
  const label = `${name ?? "Assistant"} · ${AGENT_NAME[agent] ?? agent}`;
  const badge = size >= 48 ? "mark" : "dot";
  return (
    <span
      className={`assistant-icon assistant-icon-${badge}`}
      style={{ width: size, height: size }}
      role={decorative ? undefined : "img"}
      aria-label={decorative ? undefined : label}
      aria-hidden={decorative ? true : undefined}
      title={decorative ? undefined : label}
    >
      {src ? (
        <img src={src} alt="" width={size} height={size} draggable={false} />
      ) : (
        <Drawing icon={icon ?? DEFAULT_ICON} size={size} />
      )}
      {showBadge && (agent === "claude" || agent === "codex") && (
        <span className={`assistant-badge ${agent}`} aria-hidden>
          {badge === "mark" && <AgentIcon agent={agent} size={16} decorative />}
        </span>
      )}
    </span>
  );
}

// An assistant's icon from what the server says of it.
export function iconOf(a: {
  dir: string;
  icon?: string;
  iconVersion?: number;
}): { icon?: string; src?: string } {
  return a.icon === "file"
    ? { src: iconURL(a.dir, a.iconVersion) }
    : { icon: a.icon };
}
