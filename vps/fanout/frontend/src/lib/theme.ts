import { computed, ref } from "vue";

export type ThemeChoice = "light" | "dark" | "auto";

const storageKey = "vgatebox.theme";
const media = typeof window !== "undefined" ? window.matchMedia?.("(prefers-color-scheme: dark)") : null;

function readChoice(): ThemeChoice {
  try {
    const value = localStorage.getItem(storageKey);
    if (value === "light" || value === "dark" || value === "auto") return value;
  } catch {
    // Storage can be unavailable; fall back to the OS preference.
  }
  return "auto";
}

export const themeChoice = ref<ThemeChoice>(readChoice());

const systemDark = ref(Boolean(media?.matches));

if (media) {
  const listener = (event: MediaQueryListEvent) => {
    systemDark.value = event.matches;
    if (themeChoice.value === "auto") applyTheme();
  };
  if (typeof media.addEventListener === "function") media.addEventListener("change", listener);
  else media.addListener?.(listener);
}

export const isDark = computed(() => themeChoice.value === "dark" || (themeChoice.value === "auto" && systemDark.value));

export function applyTheme(): void {
  if (typeof document === "undefined") return;
  const dark = isDark.value;
  document.documentElement.classList.toggle("dark", dark);
  document.documentElement.style.colorScheme = dark ? "dark" : "light";
}

export function setTheme(choice: ThemeChoice): void {
  themeChoice.value = choice;
  try {
    localStorage.setItem(storageKey, choice);
  } catch {
    // Ignore storage failures.
  }
  applyTheme();
}

export function toggleTheme(): void {
  setTheme(isDark.value ? "light" : "dark");
}
