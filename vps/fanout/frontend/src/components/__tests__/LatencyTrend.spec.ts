import { mount } from "@vue/test-utils";
import { describe, expect, it } from "vitest";
import LatencyTrend from "@/components/LatencyTrend.vue";

describe("LatencyTrend", () => {
  it("shows rising and falling arrows with hints", () => {
    const rising = mount(LatencyTrend, { props: { trend: "rising" } });
    expect(rising.html()).toContain("延迟上升中");
    const falling = mount(LatencyTrend, { props: { trend: "falling" } });
    expect(falling.html()).toContain("延迟下降中");
  });

  it("renders nothing without a trend", () => {
    expect(mount(LatencyTrend, { props: {} }).html()).toBe("<!--v-if-->");
    expect(mount(LatencyTrend, { props: { trend: "steady" } }).html()).toBe("<!--v-if-->");
  });
});
