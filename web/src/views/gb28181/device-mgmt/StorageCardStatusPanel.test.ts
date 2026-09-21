import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { flushPromises, mount } from "@vue/test-utils";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import StorageCardStatusPanel from "./StorageCardStatusPanel.vue";

/**
 * 存储卡状态面板（从播放控制台搬到设备详情抽屉）的单测。
 *
 * ⛔ 三条口径别改：① 空列表是**合法结果**（设备可以不装卡），措辞必须是"未安装"而不是"查询失败"；
 *    ② `refresh=true` 只是"发起查询"，权威列表要等 operation 终态后**静默重读**；
 *    ③ 格式化是**无应答命令**，下发成功后靠**再查一次** SDCardStatus 才看得到进度
 *       （见 FORMAT_WATCH_ROUNDS 那一组用例）。
 */
const api = vi.hoisted(() => ({
  getChannelStorageCards: vi.fn(),
  getPtzOperation: vi.fn(),
  // 格式化由子组件 StorageCardFormatDialog 直接调这个函数，所以 mock 里必须有它，
  // 否则弹窗一点「确认格式化」就是 "formatStorageCard is not a function"。
  formatStorageCard: vi.fn()
}));

vi.mock("@/api/gb28181", () => api);

/**
 * 🔴 必须**按 visible 渲染**的 a-modal 替身。
 *
 * 全局替身（src/test/setup.ts）是无条件渲染 slot 的，那样"弹窗没打开"也照样找得到
 * 里面的确认卡，于是"点格式化才出现确认"这类断言会恒真 —— 等于没测。
 */
const modalStub = {
  props: ["visible"],
  template: "<div v-if='visible' data-testid='storage-format-modal'><slot name='title' /><slot /></div>"
};

const CHANNEL_ID = 1;

function option(value: number) {
  return { value, label: `通道 ${value}`, online: true };
}

function cardsResponse(overrides: Record<string, unknown> = {}) {
  return {
    code: 0,
    message: "",
    data: { list: [], freshness: "unknown", refreshOperationId: null, refreshError: null, ...overrides }
  };
}

function operationResponse(status: string) {
  return { code: 0, message: "", data: { operationId: "sc-op-1", status } };
}

/**
 * 格式化下发的应答形态（真实后端由 deviceControlSuccess 产出）。
 *
 * ⛔ `responseRequired: false` 是**协议事实**（9.3.1 d)：无应答命令），
 *    它不是"这个字段随便填填" —— 弹窗的文案靠它决定要不要说"设备不会回执"。
 */
function formatResponse(overrides: Record<string, unknown> = {}) {
  return {
    code: 0,
    message: "",
    data: {
      action: "format_sd",
      operationId: "fmt-op-1",
      status: "sent",
      responseRequired: false,
      targetScope: "channel",
      targetCode: "0411212755",
      ...overrides
    }
  };
}

function card(overrides: Record<string, unknown> = {}) {
  return {
    id: 1,
    deviceId: 1,
    targetCode: "0411212755",
    cardId: 1,
    hddName: "SD Card 1",
    status: "ok",
    formatProgress: null,
    capacityMb: 1024,
    freeSpaceMb: 512,
    observedAt: "2026-09-20T10:00:00Z",
    ...overrides
  };
}

async function mountWithCard(overrides: Record<string, unknown> = {}, props: Record<string, unknown> = {}) {
  api.getChannelStorageCards.mockResolvedValue(cardsResponse({ freshness: "fresh", list: [card(overrides)] }));
  const wrapper = mountPanel(props);
  await flushPromises();
  return wrapper;
}

function mountPanel(props: Record<string, unknown> = {}) {
  return mount(StorageCardStatusPanel, {
    props: {
      channelId: CHANNEL_ID,
      channelOptions: [option(CHANNEL_ID)],
      active: true,
      canView: true,
      ...props
    },
    global: { stubs: { "a-modal": modalStub } }
  });
}

describe("StorageCardStatusPanel", () => {
  beforeEach(() => {
    api.getChannelStorageCards.mockReset();
    api.getPtzOperation.mockReset();
    api.formatStorageCard.mockReset();
    api.getChannelStorageCards.mockResolvedValue(cardsResponse());
    api.formatStorageCard.mockResolvedValue(formatResponse());
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it("打开这一页只读缓存，容量在展示层换算成 GB", async () => {
    api.getChannelStorageCards.mockResolvedValue(
      cardsResponse({
        list: [
          {
            id: 1,
            deviceId: 1,
            targetCode: "0411212755",
            cardId: 1,
            hddName: "SD Card 1",
            status: "ok",
            formatProgress: null,
            capacityMb: 32768,
            freeSpaceMb: 24576,
            observedAt: "2026-09-17T10:00:00Z"
          }
        ],
        freshness: "fresh"
      })
    );
    const wrapper = mountPanel();
    await flushPromises();

    expect(api.getChannelStorageCards).toHaveBeenLastCalledWith(CHANNEL_ID, false);
    const card = wrapper.get("[data-testid='storage-card-status']").text();
    expect(card).toContain("SD Card 1");
    expect(card).toContain("正常");
    // 32768 MB → 32.0 GB、24576 MB → 24.0 GB；已用 8192 MB = 25%。
    expect(card).toContain("已用 8.0 GB（25%）");
    expect(card).toContain("可用 24.0 GB（75%）");
    expect(card).toContain("总容量 32.0 GB");
    expect(wrapper.get("[data-testid='storage-card-1']").attributes("data-status")).toBe("ok");
    wrapper.unmount();
  });

  it("已用/可用是两段语义：实心段宽度=已用占比，图例数字与百分比互补", async () => {
    const wrapper = await mountWithCard({ capacityMb: 128 * 1024, freeSpaceMb: Math.round(93.4 * 1024) });

    // 128.0 GB 容量、93.4 GB 可用 ⇒ 已用 34.6 GB = 27%，可用 73%（不是 74%）。
    expect(wrapper.get("[data-testid='storage-bar-used']").attributes("style")).toContain("width: 27%");
    const bar = wrapper.get("[data-testid='storage-bar']");
    expect(bar.attributes("role")).toBe("progressbar");
    expect(bar.attributes("aria-valuenow")).toBe("27");
    expect(bar.attributes("aria-valuetext")).toBe("已用 27%，可用 73%");
    expect(wrapper.get("[data-testid='storage-legend-used']").text()).toContain("已用 34.6 GB（27%）");
    expect(wrapper.get("[data-testid='storage-legend-free']").text()).toContain("可用 93.4 GB（73%）");
    expect(wrapper.text()).toContain("总容量 128.0 GB");
    // 容量充裕 ⇒ 正常档
    expect(wrapper.get("[data-testid='storage-card-1']").attributes("data-level")).toBe("ok");
    wrapper.unmount();
  });

  it("两个百分比互补：不出现各自四舍五入后相加 101%", async () => {
    // 1024 里用掉 550 ⇒ 已用 27.5%（Math.round → 28%）；可用独立算会是 72.5% → 73%，合计 101%。
    const wrapper = await mountWithCard({ capacityMb: 2000, freeSpaceMb: 1450 });

    expect(wrapper.get("[data-testid='storage-bar']").attributes("aria-valuetext")).toBe("已用 28%，可用 72%");
    expect(wrapper.get("[data-testid='storage-legend-used']").text()).toContain("（28%）");
    expect(wrapper.get("[data-testid='storage-legend-free']").text()).toContain("（72%）");
    wrapper.unmount();
  });

  it("可用大于容量的脏数据按夹紧处理，不出现负的已用空间", async () => {
    const wrapper = await mountWithCard({ capacityMb: 1024, freeSpaceMb: 4096 });

    expect(wrapper.get("[data-testid='storage-bar-used']").attributes("style")).toContain("width: 0%");
    expect(wrapper.get("[data-testid='storage-legend-used']").text()).toContain("已用 0 MB（0%）");
    expect(wrapper.get("[data-testid='storage-legend-free']").text()).toContain("可用 1.0 GB（100%）");
    wrapper.unmount();
  });

  it("设备没上报容量时不画进度条（0% 空条会被读成「整块都空着」）", async () => {
    const wrapper = await mountWithCard({ capacityMb: 0, freeSpaceMb: 0 });

    expect(wrapper.find("[data-testid='storage-bar']").exists()).toBe(false);
    expect(wrapper.get("[data-testid='storage-capacity-unknown']").text()).toContain("设备未上报容量");
    wrapper.unmount();
  });

  it("已用达到 90% 时升为危险档并提示剩余空间不足", async () => {
    const wrapper = await mountWithCard({ capacityMb: 1024, freeSpaceMb: 51 }); // 已用 973 MB = 95%

    expect(wrapper.get("[data-testid='storage-card-1']").attributes("data-level")).toBe("danger");
    expect(wrapper.get("[data-testid='storage-bar-used']").attributes("style")).toContain("width: 95%");
    expect(wrapper.get("[data-testid='storage-space-alert']").text()).toBe("剩余空间不足");
    wrapper.unmount();
  });

  it("卡状态异常优先压过容量阈值（卡有毛病比用得满更该被看见）", async () => {
    // 用量只有 12%，但设备把卡报成 error ⇒ 仍按危险档着色。
    const wrapper = await mountWithCard({ status: "error", capacityMb: 1024, freeSpaceMb: 900 });

    expect(wrapper.get("[data-testid='storage-card-1']").attributes("data-level")).toBe("danger");
    wrapper.unmount();
  });

  it("设备无存储卡时展示空态而不是错误", async () => {
    api.getChannelStorageCards.mockResolvedValue(cardsResponse({ freshness: "fresh" }));
    const wrapper = mountPanel();
    await flushPromises();

    expect(wrapper.text()).toContain("设备未安装存储卡");
    expect(wrapper.find("[data-testid='storage-error']").exists()).toBe(false);
    wrapper.unmount();
  });

  it("发起查询后轮询 operation 到终态，再静默重读一次列表", async () => {
    vi.useFakeTimers();
    api.getChannelStorageCards
      .mockResolvedValueOnce(cardsResponse({ freshness: "fresh" }))
      .mockResolvedValueOnce(cardsResponse({ freshness: "unknown", refreshOperationId: "sc-op-1" }))
      .mockResolvedValueOnce(
        cardsResponse({
          freshness: "fresh",
          list: [
            {
              id: 2,
              deviceId: 1,
              targetCode: "0411212755",
              cardId: 1,
              hddName: "SD Card 1",
              status: "formatting",
              formatProgress: 40,
              capacityMb: 1024,
              freeSpaceMb: 512,
              observedAt: "2026-09-20T10:00:00Z"
            }
          ]
        })
      );
    api.getPtzOperation.mockResolvedValue(operationResponse("accepted"));

    const wrapper = mountPanel();
    await flushPromises();
    api.getChannelStorageCards.mockClear();

    await wrapper.get("[data-testid='storage-refresh']").trigger("click");
    await flushPromises();
    expect(api.getChannelStorageCards).toHaveBeenLastCalledWith(CHANNEL_ID, true);
    expect(wrapper.text()).toContain("正在查询存储卡");

    await vi.advanceTimersByTimeAsync(300);
    await flushPromises();
    expect(api.getPtzOperation).toHaveBeenCalledWith(CHANNEL_ID, "sc-op-1");
    // 终态后的重读不带 refresh —— 只是把设备刚报上来的事实读回来。
    expect(api.getChannelStorageCards).toHaveBeenLastCalledWith(CHANNEL_ID, false);

    const text = wrapper.get("[data-testid='storage-card-status']").text();
    expect(text).toContain("格式化中");
    // ⛔ 进度必须**画成条**，不能只是列表里的一句话 —— 用户上一轮报的「看不到进度百分比」
    //    根因就是进度只以文字形式存在，所以这里直接钉填充段的宽度，不靠 `text()` 子串。
    expect(wrapper.get("[data-testid='storage-format-bar-fill']").attributes("style")).toContain("width: 40%");
    expect(text).toContain("正在格式化 40%");
    expect(text).not.toContain("正在查询存储卡");
    wrapper.unmount();
  });

  it("没有通道时给出空态，查询按钮不可用", async () => {
    const wrapper = mountPanel({ channelId: null, channelOptions: [] });
    await flushPromises();

    expect(wrapper.text()).toContain("该设备下暂无通道，无法查询存储卡状态");
    expect(api.getChannelStorageCards).not.toHaveBeenCalled();
    expect(wrapper.get("[data-testid='storage-refresh']").attributes("disabled")).toBeDefined();
    wrapper.unmount();
  });
});

/**
 * 存储卡格式化（GB/T 28181-2022 A.2.3.1.13）的前端入口与**进度跟踪**。
 *
 * 这一组的核心不是"按钮能不能点"，而是这条协议的形状在界面上有没有被说对：
 *   - 它**无应答**（9.3.1 d) / 表 1 序号 12 的应答章节是「（无）」）⇒ 下发成功只能叫
 *     「已下发」，界面上不允许出现"格式化已完成"；
 *   - 进度的唯一出口是**再查一次 SDCardStatus**（A.2.6.16 的 Status=formatting +
 *     FormatProgress 只在被查询时上报，2022 全文里没有对应 NOTIFY）⇒ 下发之后必须
 *     由面板主动跟一段查询，且**有限次**（每轮都是一次真实 SIP MESSAGE）。
 */
describe("存储卡格式化入口与进度跟踪", () => {
  beforeEach(() => {
    api.getChannelStorageCards.mockReset();
    api.getPtzOperation.mockReset();
    api.formatStorageCard.mockReset();
    api.getChannelStorageCards.mockResolvedValue(cardsResponse());
    api.formatStorageCard.mockResolvedValue(formatResponse());
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  /** 打开弹窗 → 确认。两条断言之外，测试自己关心「确认之后发生了什么」。 */
  async function confirmFormat(wrapper: ReturnType<typeof mountPanel>) {
    await wrapper.get("[data-testid='storage-format-1']").trigger("click");
    await wrapper.get("[data-testid='storage-format-confirm']").trigger("click");
    await flushPromises();
  }

  it("没有格式化权限时按钮仍在但不可用（禁用而非隐藏）", async () => {
    // canFormat 缺省 false —— 与"只给 ptz:view、不给 format_sd"的账号一致。
    const wrapper = await mountWithCard({});
    const button = wrapper.get("[data-testid='storage-format-1']");
    expect(button.attributes("disabled")).toBeDefined();
    expect(button.attributes("title")).toContain("没有存储卡格式化权限");

    // 点不动，也不该弹任何东西。
    await button.trigger("click");
    expect(wrapper.find("[data-testid='storage-format-modal']").exists()).toBe(false);
    expect(api.formatStorageCard).not.toHaveBeenCalled();
    wrapper.unmount();
  });

  it("有权限时点按钮弹出二次确认，取消后不产生任何下发", async () => {
    const wrapper = await mountWithCard({}, { canFormat: true });
    expect(wrapper.find("[data-testid='storage-format-modal']").exists()).toBe(false);

    await wrapper.get("[data-testid='storage-format-1']").trigger("click");
    // 目标卡信息在弹窗顶部那张卡里（不在确认区里），所以按弹窗体断言。
    const body = wrapper.get("[data-testid='storage-format-body']");
    expect(body.text()).toContain("SD Card 1");
    expect(body.text()).toContain("卡编号 1");
    // 破坏性必须说满：清空 + 不可恢复。
    expect(body.text()).toContain("全部录像会被清空且无法恢复");
    // 协议事实也必须在**点之前**讲清楚，而不是事后补一句。
    expect(body.text()).toContain("无应答命令");
    expect(body.text()).toContain("9.3.1 d)");

    await wrapper.get("[data-testid='storage-format-cancel']").trigger("click");
    expect(wrapper.find("[data-testid='storage-format-modal']").exists()).toBe(false);
    expect(api.formatStorageCard).not.toHaveBeenCalled();
    wrapper.unmount();
  });

  it("确认后下发该卡编号，并把结果如实说成「已下发」而不是「已完成」", async () => {
    const wrapper = await mountWithCard({}, { canFormat: true });
    await confirmFormat(wrapper);

    expect(api.formatStorageCard).toHaveBeenCalledTimes(1);
    const [channelId, payload] = api.formatStorageCard.mock.calls[0];
    expect(channelId).toBe(CHANNEL_ID);
    expect(payload).toMatchObject({ cardIndex: 1 });

    const result = wrapper.get("[data-testid='storage-format-result']");
    // ⛔ 状态行只能是「已下发」。这个动作已经不可撤销了，界面若声称"已完成"，
    //    用户下一眼会看到卡还是满的 —— 而录像已经没了。
    expect(result.get("strong").text()).toBe("命令已下发");
    expect(result.text()).toContain("设备不回应答");
    expect(result.text()).toContain("fmt-op-1");
    wrapper.unmount();
  });

  it("下发成功立刻查一次设备并进入进度跟踪（无应答命令没有「等回执」这一步）", async () => {
    vi.useFakeTimers();
    const wrapper = await mountWithCard({}, { canFormat: true });
    api.getChannelStorageCards.mockClear();
    api.getChannelStorageCards.mockResolvedValue(
      cardsResponse({
        freshness: "unknown",
        refreshOperationId: "sc-op-1",
        list: [card({ status: "formatting", formatProgress: 8, freeSpaceMb: 0 })]
      })
    );
    api.getPtzOperation.mockResolvedValue(operationResponse("accepted"));

    await confirmFormat(wrapper);

    // 第一跳：立刻向设备查一次 —— 用户点完按钮最想看的就是"动了没有"。
    expect(api.getChannelStorageCards).toHaveBeenCalledWith(CHANNEL_ID, true);
    expect(wrapper.get("[data-testid='storage-freshness']").text()).toContain("正在跟踪格式化进度");

    await vi.advanceTimersByTimeAsync(300);
    await flushPromises();
    const text = wrapper.get("[data-testid='storage-card-status']").text();
    expect(text).toContain("格式化中");
    // 同上：钉宽度，不靠子串（`正在格式化 8%` 也含 `格式化 8%`，光看子串这条会恒真）。
    expect(wrapper.get("[data-testid='storage-format-bar-fill']").attributes("style")).toContain("width: 8%");
    wrapper.unmount();
  });

  it("跟踪期间卡回到非格式化态就立刻收工，不再向设备发查询", async () => {
    vi.useFakeTimers();
    const wrapper = await mountWithCard({}, { canFormat: true });
    api.getChannelStorageCards.mockClear();
    api.getChannelStorageCards
      .mockResolvedValueOnce(
        cardsResponse({
          freshness: "unknown",
          refreshOperationId: "sc-op-1",
          list: [card({ status: "formatting", formatProgress: 99, freeSpaceMb: 0 })]
        })
      )
      .mockResolvedValue(cardsResponse({ freshness: "fresh", list: [card({ status: "ok", freeSpaceMb: 1024 })] }));
    api.getPtzOperation.mockResolvedValue(operationResponse("accepted"));

    await confirmFormat(wrapper);
    await vi.advanceTimersByTimeAsync(300);
    await flushPromises();

    // 卡已经回到 ok（空间也回来了）⇒ 观察窗结束，状态行回到常规措辞。
    expect(wrapper.get("[data-testid='storage-freshness']").text()).not.toContain("正在跟踪");
    expect(wrapper.get("[data-testid='storage-card-1']").attributes("data-status")).toBe("ok");

    const settleCalls = api.getChannelStorageCards.mock.calls.length;
    await vi.advanceTimersByTimeAsync(120_000);
    expect(api.getChannelStorageCards.mock.calls.length).toBe(settleCalls);
    wrapper.unmount();
  });

  it("卡一直停在格式化态时跟踪也有上限（观察窗，不是无限轮询）", async () => {
    vi.useFakeTimers();
    const wrapper = await mountWithCard({}, { canFormat: true });
    api.getChannelStorageCards.mockClear();
    api.getChannelStorageCards.mockResolvedValue(
      cardsResponse({
        freshness: "unknown",
        refreshOperationId: "sc-op-1",
        list: [card({ status: "formatting", formatProgress: 5, freeSpaceMb: 0 })]
      })
    );
    api.getPtzOperation.mockResolvedValue(operationResponse("accepted"));

    await confirmFormat(wrapper);
    expect(wrapper.get("[data-testid='storage-freshness']").text()).toContain("正在跟踪格式化进度");

    // 观察窗跑完（8 轮 × 6s，留足余量）
    await vi.advanceTimersByTimeAsync(120_000);
    const stopped = api.getChannelStorageCards.mock.calls.length;
    expect(wrapper.get("[data-testid='storage-freshness']").text()).not.toContain("正在跟踪");

    // 再等十分钟必须一条新查询都没有 —— 否则就是拿设备的 CPU 换一个不动的进度条。
    await vi.advanceTimersByTimeAsync(600_000);
    expect(api.getChannelStorageCards.mock.calls.length).toBe(stopped);
    wrapper.unmount();
  });

  it("切走这一页就停止跟踪，不对着看不见的面板继续查设备", async () => {
    vi.useFakeTimers();
    const wrapper = await mountWithCard({}, { canFormat: true });
    api.getChannelStorageCards.mockClear();
    api.getChannelStorageCards.mockResolvedValue(
      cardsResponse({
        freshness: "unknown",
        refreshOperationId: "sc-op-1",
        list: [card({ status: "formatting", formatProgress: 5, freeSpaceMb: 0 })]
      })
    );
    api.getPtzOperation.mockResolvedValue(operationResponse("accepted"));

    await confirmFormat(wrapper);
    expect(wrapper.get("[data-testid='storage-freshness']").text()).toContain("正在跟踪格式化进度");

    await wrapper.setProps({ active: false });
    await flushPromises();
    const stopped = api.getChannelStorageCards.mock.calls.length;
    await vi.advanceTimersByTimeAsync(120_000);
    expect(api.getChannelStorageCards.mock.calls.length).toBe(stopped);
    wrapper.unmount();
  });

  it("手动「向设备查询」会打断自动跟踪（两条路同时发查询只会打架）", async () => {
    vi.useFakeTimers();
    const wrapper = await mountWithCard({}, { canFormat: true });
    api.getChannelStorageCards.mockResolvedValue(
      cardsResponse({
        freshness: "unknown",
        refreshOperationId: "sc-op-1",
        list: [card({ status: "formatting", formatProgress: 5, freeSpaceMb: 0 })]
      })
    );
    api.getPtzOperation.mockResolvedValue(operationResponse("accepted"));

    await confirmFormat(wrapper);
    expect(wrapper.get("[data-testid='storage-freshness']").text()).toContain("正在跟踪格式化进度");

    await wrapper.get("[data-testid='storage-refresh']").trigger("click");
    await flushPromises();
    expect(wrapper.get("[data-testid='storage-freshness']").text()).not.toContain("正在跟踪");
    expect(wrapper.text()).toContain("优先读平台缓存");
    wrapper.unmount();
  });

  it("卡正在格式化中时入口禁用（别让用户对同一张卡再下一次）", async () => {
    const wrapper = await mountWithCard({ status: "formatting", formatProgress: 30 }, { canFormat: true });

    const button = wrapper.get("[data-testid='storage-format-1']");
    expect(button.attributes("disabled")).toBeDefined();
    expect(button.attributes("title")).toContain("正在格式化中");
    wrapper.unmount();
  });

  it("通道离线时弹窗里直接说明「发不到设备上」并禁用确认", async () => {
    const wrapper = await mountWithCard(
      {},
      { canFormat: true, channelOptions: [{ value: CHANNEL_ID, label: "通道 1", online: false }] }
    );

    await wrapper.get("[data-testid='storage-format-1']").trigger("click");
    expect(wrapper.get("[data-testid='storage-format-blocked']").text()).toContain("通道离线");
    expect(wrapper.get("[data-testid='storage-format-confirm']").attributes("disabled")).toBeDefined();

    await wrapper.get("[data-testid='storage-format-confirm']").trigger("click");
    await flushPromises();
    expect(api.formatStorageCard).not.toHaveBeenCalled();
    wrapper.unmount();
  });

  it("下发失败（HTTP 报错）留在确认态并显示原因，不改口说成「结果未知」", async () => {
    const wrapper = await mountWithCard({}, { canFormat: true });
    api.formatStorageCard.mockResolvedValue({ code: 500, message: "无存储卡格式化权限", data: null });

    await confirmFormat(wrapper);
    expect(wrapper.get("[data-testid='storage-format-error']").text()).toContain("无存储卡格式化权限");
    expect(wrapper.find("[data-testid='storage-format-result']").exists()).toBe(false);
    expect(wrapper.find("[data-testid='storage-format-confirmation']").exists()).toBe(true);
    wrapper.unmount();
  });

  it("请求本身没回来时按「结果未知」呈现，并明确劝阻重复提交", async () => {
    const wrapper = await mountWithCard({}, { canFormat: true });
    api.formatStorageCard.mockRejectedValue(new Error("Network Error"));
    api.getChannelStorageCards.mockResolvedValue(cardsResponse({ freshness: "fresh", list: [card({})] }));

    await confirmFormat(wrapper);
    const result = wrapper.get("[data-testid='storage-format-result']");
    expect(result.text()).toContain("结果未知");
    expect(result.text()).toContain("暂不要重复提交");
    // 结果未知也算"已经发过了"：不能再给一个可点的确认按钮。
    expect(wrapper.find("[data-testid='storage-format-confirmation']").exists()).toBe(false);
    wrapper.unmount();
  });

  // ===== 进度就画在弹窗里（2026-09-20 真机实测后的回归） =====
  //
  // 起因：真机（海康 37010301021320000002）实测发现**设备报进度、后端也返回进度**
  // （formatting 0→12→…→38→ok），但进度只画在卡片行的**脚注小字**里，而用户操作所在的
  // 弹窗提交后只有「命令已下发」⇒ 用户报「看不到进度百分比」。这组用例钉住新行为。

  it("提交后**在弹窗里**就显示格式化进度，不用用户自己去找", async () => {
    const wrapper = await mountWithCard({}, { canFormat: true });
    // 提交后父组件会立刻查一次设备，这一次设备报"正在格式化 26%"。
    api.getChannelStorageCards.mockResolvedValue(
      cardsResponse({
        freshness: "fresh",
        list: [card({ status: "formatting", formatProgress: 26, freeSpaceMb: 0 })]
      })
    );

    await confirmFormat(wrapper);

    const track = wrapper.get("[data-testid='storage-format-track']");
    expect(track.get("[data-testid='storage-format-track-title']").text()).toBe("设备正在格式化该卡");
    expect(track.get("[data-testid='storage-format-track-percent']").text()).toBe("26%");
    // 进度条本体 + 无障碍数值都跟着走。
    expect(wrapper.get("[data-testid='storage-format-track-bar']").attributes("aria-valuenow")).toBe("26");
    expect(wrapper.get("[data-testid='storage-format-track-fill']").attributes("style")).toContain("26%");
    // ⛔ 必须让用户知道这个百分比**是问出来的**，不是平台在推 —— 它天生是离散的。
    expect(track.text()).toContain("A.2.6.16");
    wrapper.unmount();
  });

  it("设备报回非格式化态后，进度区改口说「设备已报告格式化结束」", async () => {
    const wrapper = await mountWithCard({}, { canFormat: true });
    api.getChannelStorageCards.mockResolvedValue(
      cardsResponse({
        freshness: "fresh",
        list: [card({ status: "formatting", formatProgress: 26, freeSpaceMb: 0 })]
      })
    );
    await confirmFormat(wrapper);
    expect(wrapper.get("[data-testid='storage-format-track-title']").text()).toBe("设备正在格式化该卡");

    // 设备做完了：回到 ok、空间放出来。
    api.getChannelStorageCards.mockResolvedValue(
      cardsResponse({ freshness: "fresh", list: [card({ status: "ok", formatProgress: null, freeSpaceMb: 1024 })] })
    );
    await wrapper.get("[data-testid='storage-refresh']").trigger("click");
    await flushPromises();

    const track = wrapper.get("[data-testid='storage-format-track']");
    expect(track.get("[data-testid='storage-format-track-title']").text()).toBe("设备已报告格式化结束");
    expect(track.text()).toContain("1.0 GB");
    // 结束后不该还挂着一根进度条。
    expect(wrapper.find("[data-testid='storage-format-track-bar']").exists()).toBe(false);
    wrapper.unmount();
  });

  it("已下发但设备还没报 formatting 时说「等待设备报告」，绝不说成已完成", async () => {
    const wrapper = await mountWithCard({}, { canFormat: true });
    // 设备还没开始（或压根不报）—— 列表里仍是 ok。
    api.getChannelStorageCards.mockResolvedValue(
      cardsResponse({ freshness: "fresh", list: [card({ status: "ok", formatProgress: null })] })
    );
    await confirmFormat(wrapper);

    const track = wrapper.get("[data-testid='storage-format-track']");
    expect(track.get("[data-testid='storage-format-track-title']").text()).toBe("命令已下发，等待设备报告");
    // ⛔⛔ 没见过 formatting 就不许出现"已完成"：提交那一刻父组件手里的卡还是上一次查询的
    //      旧事实（几乎是 ok），拿它判完成 = 把一条刚发出的命令当场报成做完了。
    expect(track.text()).not.toContain("已完成");
    wrapper.unmount();
  });

  it("没提交之前不出现进度区（否则它就是一块永远转圈的装饰）", async () => {
    const wrapper = await mountWithCard({}, { canFormat: true });
    await wrapper.get("[data-testid='storage-format-1']").trigger("click");
    expect(wrapper.find("[data-testid='storage-format-track']").exists()).toBe(false);
    wrapper.unmount();
  });

  /*
   * 卡片行：格式化期间**整段换成进度条**（2026-09-20 真机实测后的修正）。
   *
   * 起因是用户报「看不到进度百分比」。三层取证（SIP 报文 / 接口 / 前端）显示设备与后端
   * 都在正常报进度 —— 前端也画了，但只是列表里的一句小字；而**同一行那条容量条在格式化期间
   * 会显示 100% 满**（真机实测格式化全程 `FreeSpace=0`，文件系统正在重建），
   * 它比任何文字都抢眼，用户会把它读成「格式化卡住了 / 盘还是满的」。
   *
   * 所以这里钉两条：① 那条不成立的容量读数必须**消失**；② 进度要**画成条**。
   */

  it("格式化中：容量条整段让位给进度条（那条 100% 满的读数必须消失）", async () => {
    const wrapper = await mountWithCard({ status: "formatting", formatProgress: 40, freeSpaceMb: 0 });

    expect(wrapper.get("[data-testid='storage-format-bar-fill']").attributes("style")).toContain("width: 40%");
    expect(wrapper.get("[data-testid='storage-legend-format']").text()).toContain("正在格式化 40%");
    // ⛔ freeSpaceMb=0 ⇒ 已用 = 100%。这条读数在格式化期间不成立，不许画出来。
    expect(wrapper.find("[data-testid='storage-bar']").exists()).toBe(false);
    expect(wrapper.find("[data-testid='storage-legend-used']").exists()).toBe(false);
    expect(wrapper.get("[data-testid='storage-card-status']").text()).not.toContain("已用 100%");
    // 总容量照旧要说 —— 用户需要知道格完能拿回多少。
    expect(wrapper.get("[data-testid='storage-card-status']").text()).toContain("总容量");
    wrapper.unmount();
  });

  it("格式化中但设备没报进度：用不确定态脉冲，而不是画一条 0% 的空条", async () => {
    const wrapper = await mountWithCard({ status: "formatting", formatProgress: null, freeSpaceMb: 0 });

    const bar = wrapper.get("[data-testid='storage-format-bar']");
    expect(bar.classes()).toContain("is-indeterminate");
    // ⛔ 不确定态**不给宽度**：给了就是把「不知道」画成「进度 0%，明显没动」——伪造读数。
    expect(wrapper.get("[data-testid='storage-format-bar-fill']").attributes("style")).toBeUndefined();
    expect(bar.attributes("aria-valuenow")).toBeUndefined();
    expect(bar.attributes("aria-valuetext")).toBe("正在格式化，设备未上报进度");
    wrapper.unmount();
  });

  it("格式化结束回到 ok：容量条恢复、进度条消失（收工后不留残影）", async () => {
    vi.useFakeTimers();
    api.getChannelStorageCards
      .mockResolvedValueOnce(
        cardsResponse({
          freshness: "fresh",
          list: [card({ status: "formatting", formatProgress: 99, freeSpaceMb: 0 })]
        })
      )
      .mockResolvedValue(
        cardsResponse({ freshness: "fresh", list: [card({ status: "ok", formatProgress: null, freeSpaceMb: 1024 })] })
      );
    api.getPtzOperation.mockResolvedValue(operationResponse("accepted"));

    const wrapper = mountPanel();
    await flushPromises();
    expect(wrapper.find("[data-testid='storage-format-bar']").exists()).toBe(true);

    await wrapper.get("[data-testid='storage-refresh']").trigger("click");
    await flushPromises();
    await vi.advanceTimersByTimeAsync(300);
    await flushPromises();

    expect(wrapper.find("[data-testid='storage-format-bar']").exists()).toBe(false);
    expect(wrapper.find("[data-testid='storage-bar']").exists()).toBe(true);
    expect(wrapper.get("[data-testid='storage-legend-used']").text()).toContain("已用 0 MB（0%）");
    wrapper.unmount();
  });

  it("非格式化态的卡不受影响：照旧只画容量条（别把进度条画到所有卡上）", async () => {
    const wrapper = await mountWithCard({ status: "ok", formatProgress: null, freeSpaceMb: 256 });

    expect(wrapper.find("[data-testid='storage-format-bar']").exists()).toBe(false);
    expect(wrapper.find("[data-testid='storage-bar']").exists()).toBe(true);
    wrapper.unmount();
  });
});

/**
 * 进度条底轨的**源码级视觉契约**。
 *
 * 起因（2026-09-20）：底轨原来写的是 `background: var(--uvp-border)`，而 `--uvp-border`
 * 这个 token 全仓从未定义过 ⇒ `background` 在计算值阶段失效并回退 `transparent`
 * ⇒ 底轨跟卡片底色一样白 ⇒ 整条只剩一段青色，"还剩多少空间"完全看不出来。
 *
 * ⛔ 这个故障**编译、类型检查、普通渲染单测都抓不到**（happy-dom 不解析真实 CSS 计算值），
 *    所以只能钉源码。全仓「不引用未定义 token」的门禁在 src/style/model/uvp-tokens.test.ts。
 */
describe("存储卡进度条底轨的视觉契约（源码级）", () => {
  const PANEL = resolve(process.cwd(), "src/views/gb28181/device-mgmt/StorageCardStatusPanel.vue");
  const TOKENS = resolve(process.cwd(), "src/style/var/uvp-ui-tokens.scss");
  const panelSource = readFileSync(PANEL, "utf8");

  it("底轨用的是有定义的 --uvp-meter-track，而不是看不见的透明色", () => {
    expect(panelSource).not.toMatch(/var\(\s*--uvp-border(?:-subtle)?\s*\)/);
    expect(panelSource).toMatch(/\.storage-item-bar\s*\{[^}]*background:\s*var\(--uvp-meter-track\)/);
  });

  it("底轨 token 亮色与暗色各定义一次、且取值不同", () => {
    // 底轨是大面积色块：只定义亮色的话，暗色下会弱到整段读不出来。
    const source = readFileSync(TOKENS, "utf8");
    const values = [...source.matchAll(/--uvp-meter-track:\s*([^;]+);/g)].map(match => match[1].trim());
    expect(values.length).toBe(2);
    expect(new Set(values).size).toBe(2);
  });

  it("格式化进度条不复用容量条的填充 class（否则会被 data-level 改色成「空间告警」）", () => {
    /*
     * 格式化期间 `usageLevel(card)` 恰好是 `warning`（卡状态优先于容量阈值）。
     * 如果填充段复用了 `.storage-bar-used`，那三条 [data-level] 规则就会把它改成告警黄 ——
     * 一条**动作进度**于是长得和一条**容量告警**一模一样，正好是要避免的那种误读。
     * ⛔ 这条和上面那条同源：happy-dom 不解析真实 CSS 计算值，只能钉源码。
     */
    expect(panelSource).toMatch(/class="storage-bar-format"/);
    expect(panelSource).toMatch(/\.storage-bar-format\s*\{[^}]*background:\s*var\(--uvp-warning\)/);
    // 不确定态必须是"不给宽度"（宽度由 CSS 的 100% + 脉冲动画承担）。
    expect(panelSource).toMatch(/\.storage-item-bar-format\.is-indeterminate\s+\.storage-bar-format\s*\{[^}]*width:\s*100%/);
  });
});
