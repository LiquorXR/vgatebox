import { beforeEach, describe, expect, it, vi } from "vitest";

describe("theme", () => {
  beforeEach(() => {
    localStorage.clear();
    document.documentElement.className = "";
    vi.resetModules();
  });

  it("persists and applies a dark choice", async () => {
    const theme = await import("../theme");
    theme.setTheme("dark");
    expect(localStorage.getItem("vgatebox.theme")).toBe("dark");
    expect(document.documentElement.classList.contains("dark")).toBe(true);
    expect(document.documentElement.style.colorScheme).toBe("dark");
  });

  it("applies a light choice over the system preference", async () => {
    const theme = await import("../theme");
    theme.setTheme("light");
    expect(document.documentElement.classList.contains("dark")).toBe(false);
    expect(document.documentElement.style.colorScheme).toBe("light");
  });
});
