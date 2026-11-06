import { describe, expect, it } from "vitest";
import { apiPayloads } from "@/test/fixtures";
import contract from "../../../../testdata/api/payloads.json";

// P2-3 载荷契约的前端侧：testdata/api/payloads.json 的 required 键必须
// 全在 test/fixtures.ts 的 apiPayloads 里。后端侧
// api_payload_contract_test.go 断言同一份表全在真实 handler 应答里。
// 两边都红，说明有人改了字段名却只改了一边。
// 带 query 的变体（如 nodes 分页形）不断言夹具：夹具只保留 base 形状，
// 分页切片由 harness 逻辑与 nodes_pagination_test.go 覆盖。

interface PayloadEntry {
  path: string;
  query?: string;
  fixture?: boolean;
  required: string[];
}

const endpoints = (contract as { endpoints: PayloadEntry[] }).endpoints;

describe("payload contract", () => {
  for (const entry of endpoints) {
    // fixture=false 的小端点只在后端 enforced（无单元测试 mock 它们）；
    // 带 query 的变体不断言夹具（夹具只保留 base 形状，分页切片由 harness
    // 逻辑与 nodes_pagination_test.go 覆盖）。
    if (entry.fixture === false || entry.query) continue;
    const label = entry.path;
    it(`${label} required keys exist in fixtures`, () => {
      const payload = apiPayloads[entry.path] as Record<string, unknown> | undefined;
      expect(payload, `missing fixture for ${label}`).toBeDefined();
      for (const key of entry.required) {
        expect(payload, `${label} fixture lacks key ${key}`).toHaveProperty(key);
      }
    });
  }

  it("contract file is sorted and well-formed", () => {
    expect(contract.schema_version).toBe(1);
    const keys = endpoints.map((e) => [e.path, e.query ?? ""]);
    const sorted = [...keys].sort((a, b) => (a[0] < b[0] ? -1 : a[0] > b[0] ? 1 : a[1] < b[1] ? -1 : 1));
    expect(keys).toEqual(sorted);
    for (const entry of endpoints) {
      expect(entry.required.length).toBeGreaterThan(0);
    }
  });
});
