import { createContext, useContext, useEffect, useState } from "react";
type Theme = "light" | "dark" | "system";
const Context = createContext({
  theme: "system" as Theme,
  setTheme: (_v: Theme) => {},
});
export function ThemeProvider({ children }: { children: React.ReactNode }) {
  const [theme, set] = useState<Theme>(() => {
    const v = localStorage.getItem("workbench-theme");
    return v === "dark" || v === "light" ? v : "system";
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
  return (
    <Context.Provider
      value={{
        theme,
        setTheme: (v) => {
          localStorage.setItem("workbench-theme", v);
          set(v);
        },
      }}
    >
      {children}
    </Context.Provider>
  );
}
export const useTheme = () => useContext(Context);
