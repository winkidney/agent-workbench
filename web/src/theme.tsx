import { createContext, useContext, useEffect, useState } from "react";
import { isPalette, type Palette } from "./themes";
type Theme = "light" | "dark" | "system";
const Context = createContext({
  theme: "system" as Theme,
  setTheme: (_v: Theme) => {},
  palette: "default" as Palette,
  setPalette: (_v: Palette) => {},
});
export function ThemeProvider({ children }: { children: React.ReactNode }) {
  const [theme, set] = useState<Theme>(() => {
    const v = localStorage.getItem("workbench-theme");
    return v === "dark" || v === "light" ? v : "system";
  });
  const [palette, setPaletteState] = useState<Palette>(() => {
    const v = localStorage.getItem("workbench-palette");
    return isPalette(v) ? v : "default";
  });
  useEffect(() => {
    const media = matchMedia("(prefers-color-scheme: dark)");
    const apply = () => {
      document.documentElement.classList.toggle(
        "dark",
        theme === "dark" || (theme === "system" && media.matches),
      );
      document.documentElement.style.colorScheme =
        theme === "system" ? (media.matches ? "dark" : "light") : theme;
    };
    apply();
    media.addEventListener("change", apply);
    return () => media.removeEventListener("change", apply);
  }, [theme]);
  useEffect(() => {
    // Default is the absence of the attribute: style.css alone.
    if (palette === "default")
      document.documentElement.removeAttribute("data-palette");
    else document.documentElement.setAttribute("data-palette", palette);
  }, [palette]);
  return (
    <Context.Provider
      value={{
        theme,
        setTheme: (v) => {
          localStorage.setItem("workbench-theme", v);
          set(v);
        },
        palette,
        setPalette: (v) => {
          localStorage.setItem("workbench-palette", v);
          setPaletteState(v);
        },
      }}
    >
      {children}
    </Context.Provider>
  );
}
export const useTheme = () => useContext(Context);
