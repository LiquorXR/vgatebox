import { flushPromises, mount } from "@vue/test-utils";
import { beforeEach, describe, expect, it, vi } from "vitest";

const { copyText, toast } = vi.hoisted(() => ({
  copyText: vi.fn(),
  toast: vi.fn(),
}));

vi.mock("@/lib/clipboard", () => ({ copyText }));
vi.mock("@/lib/toast", () => ({
  toast,
  toastError: vi.fn(),
}));

import CopyButton from "../CopyButton.vue";

describe("CopyButton", () => {
  beforeEach(() => {
    copyText.mockReset();
    toast.mockReset();
  });

  it("copies the provided text and shows feedback", async () => {
    copyText.mockResolvedValue(undefined);
    const wrapper = mount(CopyButton, { props: { text: "socks5://example", label: "复制" } });
    await wrapper.get("button").trigger("click");
    await flushPromises();
    expect(copyText).toHaveBeenCalledWith("socks5://example");
    expect(wrapper.text()).toContain("已复制");
  });

  it("is disabled without text", () => {
    const wrapper = mount(CopyButton, { props: { text: "" } });
    expect(wrapper.get("button").attributes("disabled")).toBeDefined();
  });
});
