import { describe, expect, it } from "vitest";
import { initialTarget } from "../plan";

describe("initialTarget", () => {
  it("uses the suggested default when no target was saved", () => {
    expect(initialTarget({ target: 0, target_configured: false, suggested_target: 16 })).toBe(16);
  });

  it("preserves a saved zero", () => {
    expect(initialTarget({ target: 0, target_configured: true, suggested_target: 16 })).toBe(0);
  });

  it("preserves an existing target", () => {
    expect(initialTarget({ target: 24, target_configured: true, suggested_target: 16 })).toBe(24);
  });
});
