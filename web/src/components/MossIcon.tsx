import { Pixels, PixelSvg } from "./pixel";

// An assistant (Moss and the others alike): a box camera in 2:1 pixel isometry, its
// red-coated lens on the front-left face and the agent it runs on shown on the
// screen of its right face (Claude in orange on black; Codex as a light
// screen with its prompt). Two drawings: 16×16 for the sidebar, ink within
// the middle 14×14 like the other icons there, and 32×32 for large sizes
// (the assistant card). Sizes are whole multiples of the grid.
export const MOSS_CLAUDE_16 = [
  "................",
  "..........K.....",
  "........KKTKK...",
  "......KKTTTTTKK.",
  "....KKTTTTTTTQK.",
  "..KKTTTTTTTQQNK.",
  ".KFTTTTTTQQCCNK.",
  ".KFffTTQQCCNCCK.",
  ".KllFfQNCNCCCNK.",
  ".KLLlfQNCCCNCNK.",
  ".KRrLlQCCNCNNKK.",
  ".KrRLlQNCNNKK...",
  ".KKrLlQNNKK.....",
  "...KKfQKK.......",
  ".....KK.........",
  "................",
];
export const MOSS_CODEX_16 = [
  "................",
  "..........K.....",
  "........KKTKK...",
  "......KKTTTTTKK.",
  "....KKTTTTTTTQK.",
  "..KKTTTTTTTQQxK.",
  ".KFTTTTTTQQxxxK.",
  ".KFffTTQQxxxxxK.",
  ".KllFfQOxxxxxxK.",
  ".KLLlfQxOxxOOxK.",
  ".KRrLlQOxxOxxKK.",
  ".KrRLlQxxxxKK...",
  ".KKrLlQxxKK.....",
  "...KKfQKK.......",
  ".....KK.........",
  "................",
];
export const MOSS_CLAUDE_32 = [
  "...................K............",
  ".................KKTKK..........",
  "...............KKTTTTTKK........",
  ".............KKTTTTTTTTTKK......",
  "...........KKTTTTTTTTTTTTTKK....",
  ".........KKTTTTTTTTTTTTTTQQK....",
  ".......KKTTTTTTTTTTTTTTQQMMK....",
  ".....KKTTTTTTTTTTTTTTQQMMNNK....",
  "....KFTTTTTTTTTTTTTQQMMNNNNK....",
  "....KFffTTTTTTTTTQQMMNNCCNNK....",
  "....KFFFffTTTTTQQMMNNCCCCNNK....",
  "....KFFFFFffTQQMMNNCCCCCCNNK....",
  "....KFFFFFFfQMMNNCCCCNNCCCCK....",
  "....KFFllFFfQNNCCCCCCNNCCCCK....",
  "....KLLLLlFfQNNCCNNCCCCCCNNK....",
  "....KLrrLLlfQNNCCNNCCCCCCNNK....",
  "....KLrRHrllQNNCCCCCCNNCCNNK....",
  "....KLrRRrLlQNNCCCCCCNNNNNNK....",
  "....KLrRRrLlQCCCCNNCCNNNNMMK....",
  "....KFLrrrLlQCCCCNNNNNNMMssK....",
  "....KFFLLLLlQNNCCNNNNMMssssK....",
  "....KFFfFFFfQNNNNNNMMssssVsK....",
  "....KFFfffFfQNNNNMMssssVVssK....",
  "....KFFfffffQNNMMssssVVssKK.....",
  "....KFFfffffQMMssssVVssKK.......",
  "....KFFFffffQssssssssKK.........",
  "....KKFFFFffQssssssKK...........",
  "......KKFFFfQssssKK.............",
  "........KKFfQssKK...............",
  "..........KKQKK.................",
  "............K...................",
  "................................",
];
export const MOSS_CODEX_32 = [
  "...................K............",
  ".................KKTKK..........",
  "...............KKTTTTTKK........",
  ".............KKTTTTTTTTTKK......",
  "...........KKTTTTTTTTTTTTTKK....",
  ".........KKTTTTTTTTTTTTTTQQK....",
  ".......KKTTTTTTTTTTTTTTQQMMK....",
  ".....KKTTTTTTTTTTTTTTQQMMxxK....",
  "....KFTTTTTTTTTTTTTQQMMxxxxK....",
  "....KFffTTTTTTTTTQQMMxxxxxxK....",
  "....KFFFffTTTTTQQMMxxxxxxxxK....",
  "....KFFFFFffTQQMMxxxxxxxxxxK....",
  "....KFFFFFFfQMMxxxxxxxxxxxxK....",
  "....KFFllFFfQxxxxxxxxxxxxxxK....",
  "....KLLLLlFfQOOxxxxxxxxxxxxK....",
  "....KLrrLLlfQOOOOxxxxxxOOxxK....",
  "....KLrRHrllQxxOOxxxxOOOOxxK....",
  "....KLrRRrLlQxxxxxxOOOOxxxxK....",
  "....KLrRRrLlQOOxxxxOOxxxxMMK....",
  "....KFLrrrLlQOOxxxxxxxxMMssK....",
  "....KFFLLLLlQxxxxxxxxMMssssK....",
  "....KFFfFFFfQxxxxxxMMssssVsK....",
  "....KFFfffFfQxxxxMMssssVVssK....",
  "....KFFfffffQxxMMssssVVssKK.....",
  "....KFFfffffQMMssssVVssKK.......",
  "....KFFFffffQssssssssKK.........",
  "....KKFFFFffQssssssKK...........",
  "......KKFFFfQssssKK.............",
  "........KKFfQssKK...............",
  "..........KKQKK.................",
  "............K...................",
  "................................",
];
export const MOSS_COLORS = {
  C: "var(--agent-claude)", // Claude
  F: "#aeb1bf", // front
  H: "#ffffff", // glint
  K: "#15171c", // outline
  L: "#6c7080", // lens barrel, lit
  M: "#9095a6", // screen frame
  N: "#0c0d10", // Claude screen
  O: "#3f4857", // Codex prompt
  Q: "#6b7186", // right, lit
  R: "#c8323a", // lens: dark glass with a red coating
  T: "#c4c7d2", // top
  V: "#2e3240", // vents
  f: "#8e92a2", // front, shaded
  l: "#545867", // lens barrel
  r: "#5a1418", // lens rim
  s: "#3c4050", // right, shaded
  x: "#dfe4ec", // Codex screen
};

export function MossIcon({
  agent,
  size = 16,
  title,
  decorative,
}: {
  agent: string;
  // 16, or a multiple of 32 for the detailed drawing.
  size?: number;
  title?: string;
  decorative?: boolean;
}) {
  const codex = agent === "codex";
  const big = size >= 32;
  const grid = big
    ? codex
      ? MOSS_CODEX_32
      : MOSS_CLAUDE_32
    : codex
      ? MOSS_CODEX_16
      : MOSS_CLAUDE_16;
  const n = big ? 32 : 16;
  return (
    <PixelSvg
      w={n}
      h={n}
      scale={size / n}
      label={
        decorative
          ? undefined
          : (title ?? `Moss · ${codex ? "Codex" : "Claude"}`)
      }
      className={"moss-icon " + (codex ? "codex" : "claude")}
    >
      <Pixels grid={grid} palette={MOSS_COLORS} />
    </PixelSvg>
  );
}
