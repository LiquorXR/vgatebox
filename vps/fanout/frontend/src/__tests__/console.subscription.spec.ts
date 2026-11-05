import { beforeEach, describe, expect, it } from "vitest";
import { installApiMock, mountConsole, waitFor, type ApiMock } from "@/test/harness";

describe("console subscription", () => {
  let api: ApiMock;

  beforeEach(() => {
    api = installApiMock();
  });

  it("switches the format and updates the QR code URL", async () => {
    const { screen } = await mountConsole("/subscription", api);

    await waitFor(() => screen.findAll("h1").some((item) => item.text() === "订阅"), "subscription heading");
    const url = screen.get('textarea[name="subscription-url"]');
    expect((url.element as HTMLTextAreaElement).value).toContain("token=abcdef1234567890");

    await screen.get('select[name="sub-format"]').setValue("clash");
    await waitFor(
      () => (url.element as HTMLTextAreaElement).value.includes("target=clash"),
      "clash subscription URL",
    );

    const qr = screen.get('img[alt="订阅二维码"]');
    expect(qr.attributes("src")).toContain("api/console/qr");
    expect(qr.attributes("src")).not.toContain("token=");
  });
});
