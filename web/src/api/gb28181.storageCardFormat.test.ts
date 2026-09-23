import { beforeEach, describe, expect, it, vi } from "vitest";

const request = vi.hoisted(() => vi.fn());

vi.mock("@/utils/http", () => ({ http: { request } }));
vi.mock("./utils", () => ({ baseUrlApi: (path: string) => `/api/${path}` }));

import { formatStorageCard } from "./gb28181";

/**
 * 存储卡格式化的**请求契约**（GB/T 28181-2022 A.2.3.1.13）。
 *
 * 这里钉的三条都是"错了不会报错、只会安静地做错事"的那类：
 *
 *  1. **必须走独立路由** `.../storage-cards/format`。塞进 `.../device-control` 的 action
 *     也能发出去、也能成功，但那条路由整条绑 `gb28181:device:control` ——
 *     独立权限码 `gb28181:device:format_sd` 在 casbin 层就与普通设备控制同码，
 *     等于把一个破坏性动作的授权合并进了日常操作权限里。
 *  2. **`confirmed: true` 必须带上**。服务端拿它当硬门禁（缺失直接拒），
 *     而它必须是**发起动作的一方**声明的，不能靠请求体默认值兜底。
 *  3. **`cardIndex: 0` 必须原样发出去**。0 在标准里是合法且有意义的值
 *     （A.2.3.1.13 注释「该值0时，对所有存储卡进行格式化」），
 *     任何 `if (cardIndex) {...}` / `|| 1` 兜底都会把"格式化全部卡"悄悄变成"格式化第 1 张"。
 */
describe("存储卡格式化请求契约", () => {
  beforeEach(() => request.mockReset().mockResolvedValue({ code: 0, data: null }));

  it("走独立路由，不借用 device-control", async () => {
    await formatStorageCard(12, { cardIndex: 1, idempotencyKey: "fmt-1" });

    expect(request).toHaveBeenLastCalledWith("post", "/api/gb28181/device-mgmt/channel/12/storage-cards/format", {
      data: { cardIndex: 1, idempotencyKey: "fmt-1", confirmed: true }
    });
    // 反向对照：这条路径里不该出现 device-control。
    expect(String(request.mock.calls.at(-1)?.[1])).not.toContain("device-control");
  });

  it("由 api 层统一补 confirmed，调用方不传也必须有", async () => {
    await formatStorageCard(3, { cardIndex: 2 });

    const [, , config] = request.mock.calls.at(-1) as [string, string, { data: Record<string, unknown> }];
    expect(config.data.confirmed).toBe(true);
  });

  it("cardIndex=0（格式化全部卡）原样保留，不会因为「像没传」而被兜底成 1", async () => {
    await formatStorageCard(5, { cardIndex: 0 });

    const [, , config] = request.mock.calls.at(-1) as [string, string, { data: Record<string, unknown> }];
    expect(config.data.cardIndex).toBe(0);
    expect("cardIndex" in config.data).toBe(true);
  });

  it("幂等键为可选：客户端没给时不硬塞空串", async () => {
    await formatStorageCard(7, { cardIndex: 1 });

    const [, , config] = request.mock.calls.at(-1) as [string, string, { data: Record<string, unknown> }];
    expect("idempotencyKey" in config.data).toBe(false);
  });
});
