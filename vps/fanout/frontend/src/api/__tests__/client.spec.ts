import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiError, api, apiUrl, queryString, resolveRoot } from "../client";

describe("resolveRoot", () => {
  it("keeps the management base path for the root console", () => {
    expect(resolveRoot("/")).toBe("/");
    expect(resolveRoot("/aB3/")).toBe("/aB3/");
    expect(resolveRoot("/aB3/index.html")).toBe("/aB3/");
  });

  it("removes the /next alias", () => {
    expect(resolveRoot("/next/")).toBe("/");
    expect(resolveRoot("/aB3/next/")).toBe("/aB3/");
    expect(resolveRoot("/aB3/next")).toBe("/aB3/");
  });
});

describe("queryString", () => {
  it("drops empty values and encodes the rest", () => {
    expect(queryString({ slot: 1, host: "", empty: undefined, name: "a b" })).toBe("?slot=1&name=a+b");
    expect(queryString({})).toBe("");
  });
});

describe("api", () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("returns JSON from the current base path", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(JSON.stringify({ ok: true }), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      }),
    );
    vi.stubGlobal("fetch", fetchMock);

    await expect(api<{ ok: boolean }>("api/test")).resolves.toEqual({ ok: true });
    expect(fetchMock).toHaveBeenCalledWith(
      apiUrl("api/test"),
      expect.objectContaining({ method: "GET", credentials: "same-origin" }),
    );
  });

  it("uses the backend error message and status", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(
        new Response(JSON.stringify({ error: "出口不存在" }), {
          status: 409,
          headers: { "Content-Type": "application/json" },
        }),
      ),
    );

    await expect(api("api/test")).rejects.toMatchObject({
      name: "ApiError",
      message: "出口不存在",
      status: 409,
    } satisfies Partial<ApiError>);
  });

  it("wraps network failures", async () => {
    vi.stubGlobal("fetch", vi.fn().mockRejectedValue(new TypeError("failed to fetch")));
    await expect(api("api/test")).rejects.toMatchObject({
      name: "ApiError",
      status: 0,
    } satisfies Partial<ApiError>);
  });
});
