import { beforeEach, describe, expect, it, vi } from "vitest";

const request = vi.hoisted(() => vi.fn());

vi.mock("@/utils/http", () => ({ http: { request } }));
vi.mock("./utils", () => ({ baseUrlApi: (path: string) => `/api/${path}` }));

import { getChannelTargetTrack, setChannelTargetTrack } from "./gb28181";

/**
 * 目标跟踪的**请求契约**（GB/T 28181-2022 A.2.3.1.14）。
 *
 * 这里钉的都是"错了不会报错、只会安静地做错事"的那类：
 *
 *  1. **必须走独立路由** `.../target-track`。塞进 `.../device-control` 的 action 也能发出去，
 *     但那条路由整条绑 `gb28181:device:control`，而且它没有"读回平台已下发意图"这条出口
 *     —— 目标跟踪是**无应答命令**，看不到意图就等于界面上什么都留不下。
 *  2. **读接口必须是 GET、且不带任何参数**。它读的是平台自己的表（不产生 SIP 报文），
 *     带上 `refresh` 之类的参数会让人以为它能去问设备 —— 而标准里根本没有这条查询命令。
 *  3. **`area` 只能由调用方给**。`Stop` 带框是自相矛盾的指令（服务端两侧都会拒），
 *     API 层绝不能替任何 mode 补一个默认框：默认框 = 一个平台上没人画过的目标。
 */
describe("目标跟踪请求契约", () => {
  beforeEach(() => request.mockReset().mockResolvedValue({ code: 0, data: null }));

  it("读接口走独立路由，且不发任何参数（不产生 SIP 报文）", async () => {
    await getChannelTargetTrack(12);

    expect(request).toHaveBeenLastCalledWith(
      "get",
      "/api/gb28181/device-mgmt/channel/12/target-track",
      undefined,
      // 静默配置：本接口是面板的后台重读，失败时不该弹全局错误提示。
      expect.objectContaining({ showErrorMessage: false })
    );
    expect(String(request.mock.calls.at(-1)?.[1])).not.toContain("device-control");
  });

  it("写接口走独立路由，mode 原样交给服务端", async () => {
    await setChannelTargetTrack(12, { mode: "Auto", idempotencyKey: "tt-1" });

    expect(request).toHaveBeenLastCalledWith("post", "/api/gb28181/device-mgmt/channel/12/target-track", {
      data: { mode: "Auto", idempotencyKey: "tt-1" }
    });
  });

  it("Stop 绝不替调用方补 area —— 带框的停止是自相矛盾的指令", async () => {
    await setChannelTargetTrack(5, { mode: "Stop" });

    const [, , config] = request.mock.calls.at(-1) as [string, string, { data: Record<string, unknown> }];
    expect("area" in config.data).toBe(false);
    expect(config.data.mode).toBe("Stop");
  });

  it("Manual 的六项框选坐标原样透传，一个都不许被「归一化」", async () => {
    const area = { length: 1280, width: 720, midPointX: 640, midPointY: 360, lengthX: 200, lengthY: 120 };
    await setChannelTargetTrack(7, { mode: "Manual", deviceId2: "34020000001310000009", area });

    const [, , config] = request.mock.calls.at(-1) as [string, string, { data: Record<string, unknown> }];
    expect(config.data.area).toEqual(area);
    expect(config.data.deviceId2).toBe("34020000001310000009");
  });

  it("mode 原样发标准拼写（大写），不在 api 层做大小写归一", async () => {
    // 大小写归一在服务端 `ParseTargetTrackMode` 一处做；两层各归一一次，
    // 排障时就分不清"到底哪一层改的"了。
    await setChannelTargetTrack(7, {
      mode: "Manual",
      area: { length: 1, width: 1, midPointX: 0, midPointY: 0, lengthX: 1, lengthY: 1 }
    });

    const [, , config] = request.mock.calls.at(-1) as [string, string, { data: Record<string, unknown> }];
    expect(config.data.mode).toBe("Manual");
  });

  it("deviceId2 未指定时不硬塞空串（空串会被当成「指定了空的全景通道」）", async () => {
    await setChannelTargetTrack(7, { mode: "Auto" });

    const [, , config] = request.mock.calls.at(-1) as [string, string, { data: Record<string, unknown> }];
    expect("deviceId2" in config.data).toBe(false);
  });
});
