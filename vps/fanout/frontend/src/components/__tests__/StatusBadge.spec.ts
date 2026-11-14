import { mount } from "@vue/test-utils";
import { describe, expect, it } from "vitest";
import StatusBadge from "../StatusBadge.vue";

describe("StatusBadge", () => {
  it("renders the label and tone", () => {
    const wrapper = mount(StatusBadge, { props: { label: "已就绪", tone: "ok" } });
    expect(wrapper.text()).toContain("已就绪");
    expect(wrapper.classes()).toContain("bg-ok-soft");
    expect(wrapper.classes()).toContain("w-fit");
  });

  it("pulses the dot for running states", () => {
    const wrapper = mount(StatusBadge, { props: { label: "连接中", tone: "info", pulse: true } });
    expect(wrapper.find(".animate-pulse").exists()).toBe(true);
  });
});
