// Color themes. Each has a light and a dark palette in themes.css; the mode
// (light, dark, system) is chosen separately. public/theme-init.js applies the
// stored theme before the first paint and keeps its own copy of the ids.
export const THEMES = [
  {
    id: "default",
    name: "Default",
    description: "Neutral interface; each agent keeps its own CLI's colors",
  },
  {
    id: "everforest",
    name: "Everforest",
    description: "Soft greens on warm paper, made to be easy on the eyes",
  },
  {
    id: "gruvbox",
    name: "Gruvbox",
    description: "Warm retro tones, soft contrast",
  },
  {
    id: "solarized",
    name: "Solarized",
    description: "Tuned contrast, the same accents in light and dark",
  },
  {
    id: "paper",
    name: "Paper",
    description: "Sepia for reading, low in blue light",
  },
  {
    id: "catppuccin",
    name: "Catppuccin",
    description: "Pastels: Latte and Mocha",
  },
  {
    id: "rose-pine",
    name: "Rosé Pine",
    description: "Muted rose and pine: Dawn and Moon",
  },
  {
    id: "nord",
    name: "Nord",
    description: "Arctic blue-grey",
  },
  {
    id: "tokyo-night",
    name: "Tokyo Night",
    description: "Deep blue: Day and Storm",
  },
  {
    id: "darcula",
    name: "Darcula",
    description: "JetBrains' classic dark, with IntelliJ Light",
  },
] as const;

export type Palette = (typeof THEMES)[number]["id"];

export function isPalette(v: unknown): v is Palette {
  return THEMES.some((t) => t.id === v);
}
