import { beforeEach, describe, expect, it } from "vitest";
import { installApiMock, mountConsole, waitFor, type ApiMock } from "@/test/harness";

describe("console settings", () => {
  let api: ApiMock;

  beforeEach(() => {
    api = installApiMock();
  });

  it("applies and persists the dark theme from the appearance menu", async () => {
    const { screen } = await mountConsole("/settings", api);

    await waitFor(() => screen.text().includes("v0.9.2"), "settings page");

    await screen.get('button[aria-label="切换外观"]').trigger("click");
    await waitFor(() => screen.findAll('[role="menuitem"]').length === 3, "appearance menu");
    const dark = screen.findAll('[role="menuitem"]').find((item) => item.text().includes("深色"));
    expect(dark).toBeDefined();

    await dark!.trigger("click");
    await waitFor(() => document.documentElement.classList.contains("dark"), "dark theme");
    expect(localStorage.getItem("vgatebox.theme")).toBe("dark");
    expect(document.documentElement.style.colorScheme).toBe("dark");
  });
});
