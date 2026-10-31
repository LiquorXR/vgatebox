import { flushPromises, mount } from "@vue/test-utils";
import { afterEach, describe, expect, it, vi } from "vitest";
import LoginPage from "../LoginPage.vue";

describe("LoginPage", () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("posts the password and shows the server error", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(JSON.stringify({ error: "口令不对" }), {
        status: 401,
        headers: { "Content-Type": "application/json" },
      }),
    );
    vi.stubGlobal("fetch", fetchMock);

    const wrapper = mount(LoginPage);
    await wrapper.get('input[name="password"]').setValue("wrong");
    await wrapper.get("form").trigger("submit");
    await flushPromises();

    expect(fetchMock).toHaveBeenCalledWith(
      "login",
      expect.objectContaining({ method: "POST", credentials: "same-origin" }),
    );
    expect(wrapper.get('[role="alert"]').text()).toContain("口令不对");
  });

  it("does not submit an empty password", async () => {
    const fetchMock = vi.fn();
    vi.stubGlobal("fetch", fetchMock);
    const wrapper = mount(LoginPage);
    await wrapper.get("form").trigger("submit");
    await flushPromises();
    expect(fetchMock).not.toHaveBeenCalled();
  });
});
