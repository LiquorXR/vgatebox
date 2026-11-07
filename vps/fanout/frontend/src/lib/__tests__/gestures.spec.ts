import { describe, expect, it } from "vitest";
import { shouldDismissDrawer } from "../gestures";

describe("swipe-to-close", () => {
  it("dismisses on long drags", () => {
    expect(shouldDismissDrawer(130, 500)).toBe(true);
    expect(shouldDismissDrawer(10, 500)).toBe(false);
  });

  it("dismisses on fast flicks", () => {
    expect(shouldDismissDrawer(30, 40)).toBe(true);
    expect(shouldDismissDrawer(30, 500)).toBe(false);
  });
});
