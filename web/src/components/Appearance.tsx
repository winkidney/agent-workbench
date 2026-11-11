import { Check, ChevronRight } from "lucide-react";
import { useTheme } from "../theme";
import { THEMES, type Palette } from "../themes";

const MODES = [
  { id: "system", label: "System" },
  { id: "light", label: "Light" },
  { id: "dark", label: "Dark" },
] as const;

// A small preview drawn in the theme's own colors (current mode): its
// background, text, and three of its syntax colors.
export function ThemeSwatch({ id }: { id: Palette }) {
  return (
    <span className="theme-swatch" data-palette={id} aria-hidden="true">
      Aa
      <i />
      <i />
      <i />
    </span>
  );
}

// Mode and theme controls for the header's ⋯ menu. Both apply at once and are
// remembered per browser.
export function Appearance() {
  const { theme, setTheme, palette, setPalette } = useTheme();
  const current = THEMES.find((t) => t.id === palette) ?? THEMES[0];
  return (
    <div className="appearance">
      <span className="menu-label" id="appearance-mode">
        Mode
      </span>
      <div
        className="mode-toggle"
        role="group"
        aria-labelledby="appearance-mode"
      >
        {MODES.map((m) => (
          <button
            key={m.id}
            type="button"
            aria-pressed={theme === m.id}
            onClick={() => setTheme(m.id)}
          >
            {m.label}
          </button>
        ))}
      </div>
      <details className="theme-picker">
        <summary>
          <span className="menu-label">Theme</span>
          <ThemeSwatch id={current.id} />
          <span className="theme-current">{current.name}</span>
          <ChevronRight size={14} className="theme-chevron" />
        </summary>
        <div className="theme-list" role="group" aria-label="Theme">
          {THEMES.map((t) => (
            <button
              key={t.id}
              type="button"
              aria-pressed={palette === t.id}
              title={t.description}
              onClick={() => setPalette(t.id)}
            >
              <ThemeSwatch id={t.id} />
              <span className="theme-name">{t.name}</span>
              {palette === t.id && <Check size={14} />}
            </button>
          ))}
        </div>
      </details>
    </div>
  );
}
