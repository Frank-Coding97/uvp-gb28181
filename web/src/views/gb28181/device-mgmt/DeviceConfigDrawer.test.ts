import { flushPromises, mount } from "@vue/test-utils";
import { beforeEach, describe, expect, it, vi } from "vitest";
import DeviceConfigDrawer from "./DeviceConfigDrawer.vue";
import { CONFIG_GROUPS } from "./deviceConfigGroups";

const api = vi.hoisted(() => ({
  getChannelVideoParams: vi.fn(),
  applyChannelVideoParams: vi.fn(),
  getChannelDeviceConfigs: vi.fn(),
  applyChannelDeviceConfigs: vi.fn()
}));

vi.mock("@/api/gb28181", () => api);

const SelectStub = {
  props: { modelValue: { type: [String, Number], default: "" } },
  emits: ["change"],
  template: '<select :value="String(modelValue)" @change="$emit(\'change\', $event)"><slot /></select>'
};

function videoParamRow(overrides: Record<string, unknown> = {}) {
  return {
    id: 1,
    deviceId: 5147,
    targetCode: "34020000001320000010",
    streamNumber: 0,
    videoFormat: "2",
    resolution: "5",
    frameRate: "25",
    bitRateType: "1",
    videoBitRate: "2000",
    observedAt: "2026-09-18T08:53:53Z",
    sourceSn: 771,
    ...overrides
  };
}

/** 默认应答里 refreshOperationId 恒为 null —— 免得用例被后台轮询定时器缠住。 */
function readOk(list: Array<Record<string, unknown>> = [videoParamRow()]) {
  return {
    code: 0,
    message: "ok",
    data: {
      list,
      freshness: "fresh",
      streamNumberList: "0",
      registeredVersion: "2022",
      observedAt: "2026-09-18T08:53:53Z",
      reconcile: {
        state: "read_ok",
        operationId: "1304",
        status: "accepted",
        responseHasData: true,
        derivedFromApply: false
      },
      refreshOperationId: null,
      refreshError: null
    }
  };
}

/** 配置家族读应答。⛔ payload 的键名是**标准元素名小驼峰**（协议词汇表），不是表单键。 */
function deviceConfigOk(entries: Array<Record<string, unknown>> = [], absentTypes: string[] = []) {
  return {
    code: 0,
    message: "ok",
    data: {
      list: entries,
      absentTypes,
      requestedTypes: ["OSDConfig"],
      targetCode: "34020000001320000010",
      registeredVersion: "2022",
      freshness: "fresh",
      observedAt: "2026-09-19T02:10:00Z",
      reconcile: {
        state: "read_ok",
        operationId: "1402",
        status: "accepted",
        responseHasData: true,
        derivedFromApply: false
      },
      refreshOperationId: null,
      refreshError: null
    }
  };
}

/**
 * 一条配置快照。
 *
 * ⛔ `payload` 是**那一块本身**（库表按 `(device,target,configType)` 一行一块），
 *    不是包在 `osdConfig` 键下的容器 —— 写成容器形状会让用例和真实接口对不上。
 */
function osdEntry(overrides: Record<string, unknown> = {}) {
  return {
    configType: "OSDConfig",
    observedAt: "2026-09-19T02:10:00Z",
    sourceOperationId: "1402",
    payload: {
      length: 1920,
      width: 1080,
      timeX: 10,
      timeY: 20,
      timeEnable: 1,
      timeType: 1,
      textEnable: 0,
      items: [{ text: "厂区东门", x: 5, y: 6 }],
      ...overrides
    }
  };
}

/** 画面处理的块之一：`FrameMirror` 与 `PictureMask` 在标准里是**两条独立配置**。 */
function frameMirrorEntry(overrides: Record<string, unknown> = {}) {
  return {
    configType: "FrameMirror",
    observedAt: "2026-09-19T02:10:00Z",
    sourceOperationId: "1402",
    payload: { value: 0, ...overrides }
  };
}

/** ⛔ regions 按 `seq` 归位（1~4），不是数组下标。 */
function pictureMaskEntry(overrides: Record<string, unknown> = {}) {
  return {
    configType: "PictureMask",
    observedAt: "2026-09-19T02:10:00Z",
    sourceOperationId: "1402",
    payload: { on: 0, regions: [], ...overrides }
  };
}

function mountDrawer(props: Record<string, unknown> = {}) {
  return mount(DeviceConfigDrawer, {
    global: {
      stubs: {
        "a-select": SelectStub
      }
    },
    props: {
      visible: true,
      deviceName: "UVP-Sim",
      deviceCode: "37010301021180000007",
      channelName: "前置摄像头",
      online: true,
      effectiveVersion: "2022",
      channelId: 3539,
      // 默认按「播放控制台里那份」挂：那边外面有画布，OSD 面板才长成完整形态。
      // ⛔ 不写这条的话测的是「宿主没有画面」的形态 ——
      //    没有画布 ⇒ 没有「调整位置」、精确数值默认展开。两种形态各有一条用例钉着。
      osdCanvasLinked: true,
      ...props
    }
  });
}

describe("DeviceConfigDrawer 设备配置中心", () => {
  beforeEach(() => {
    api.getChannelVideoParams.mockReset();
    api.applyChannelVideoParams.mockReset();
    api.getChannelDeviceConfigs.mockReset();
    api.applyChannelDeviceConfigs.mockReset();
    api.getChannelVideoParams.mockResolvedValue(readOk());
    api.getChannelDeviceConfigs.mockResolvedValue(deviceConfigOk());
  });

  it("渲染三栏骨架：标题栏 + 预览 + 全部分组导航 + 参数区", async () => {
    const wrapper = mountDrawer();
    await flushPromises();

    expect(wrapper.find(".dcg-window").exists()).toBe(true);
    expect(wrapper.find("[data-testid='dcg-title-target']").text()).toBe("前置摄像头");
    expect(wrapper.find(".dcg-screen").exists()).toBe(true);
    expect(wrapper.findAll(".dcg-nav-item")).toHaveLength(CONFIG_GROUPS.length);
    expect(wrapper.find(".dcg-params").exists()).toBe(true);
    // 默认落在唯一已接入的分组上
    expect(wrapper.find("[data-testid='dcg-group-title']").text()).toBe("视频参数属性");
    expect(wrapper.find("[data-testid='dcg-group-state']").text()).toBe("已接入");
  });

  it("visible=false 时不渲染窗口", () => {
    const wrapper = mountDrawer({ visible: false });
    expect(wrapper.find(".dcg-window").exists()).toBe(false);
  });

  it("嵌入侧栏时分组导航用短名，标准全名留在 title 上", async () => {
    const wrapper = mountDrawer({ embedded: true, groupKeys: ["video-param", "osd"] });
    await flushPromises();

    const videoNav = wrapper.get("[data-testid='dcg-nav-video-param']");
    // ⛔ 430px 侧栏里两组等分、每格约 105px：「视频参数属性」会被省略号截成「视频参数…」，
    //    等于没写。所以导航这一行用 `navLabel`；
    //    ⛔ 但标准名**一个字都没改** —— 它挂在 title 上，非嵌入形态照旧显示 `label` + `std`。
    expect(videoNav.find(".dcg-nav-label").text()).toBe("视频编码");
    expect(videoNav.attributes("title")).toBe("视频参数属性");
    expect(wrapper.get("[data-testid='dcg-nav-osd']").find(".dcg-nav-label").text()).toBe("图像叠加");
    expect(wrapper.get("[data-testid='dcg-nav-osd']").attributes("title")).toBe("图像叠加 OSD");
  });

  it("分组可由宿主受控：外部改值界面跟着走，内部点击只发意图", async () => {
    const wrapper = mountDrawer({
      embedded: true,
      groupKeys: ["video-param", "osd"],
      activeGroupKey: "video-param"
    });
    await flushPromises();
    expect(wrapper.find("[data-testid='dcg-stream-0']").exists()).toBe(true);

    // 宿主改分组 → 界面跟着走，不需要再点导航。
    await wrapper.setProps({ activeGroupKey: "osd" });
    await flushPromises();
    expect(wrapper.find("[data-testid='dcg-osd-blocks']").exists()).toBe(true);
    expect(wrapper.find("[data-testid='dcg-stream-0']").exists()).toBe(false);

    // 点导航 → 只发意图，不自己落状态（受控组件的本分）。宿主 PlayConsoleLinked 就是靠这个
    // 信号决定"切离图像叠加就退出 OSD 编辑模式"的。
    await wrapper.get("[data-testid='dcg-nav-video-param']").trigger("click");
    expect(wrapper.emitted("update:activeGroupKey")?.at(-1)).toEqual(["video-param"]);
    // ⛔ 没收到宿主回写之前界面**不动** —— 这正是"真源在宿主"的可观测证据。
    //    抽屉自己偷偷切过去的话，宿主那份状态就与界面脱钩了。
    expect(wrapper.find("[data-testid='dcg-osd-blocks']").exists()).toBe(true);
  });

  it("宿主给的非法分组 key 回落第一组，不让导航一个高亮都没有", async () => {
    // 设备详情抽屉只挂 record / alarm，而宿主那份状态可能停在别的组。
    // ⛔ 直接把非法 key 交给渲染，`dcg-nav` 会一个 `is-active` 都不带 —— 看着像"导航坏了"。
    const wrapper = mountDrawer({ embedded: true, groupKeys: ["video-param", "osd"], activeGroupKey: "record-plan" });
    await flushPromises();
    expect(wrapper.get("[data-testid='dcg-nav-video-param']").classes()).toContain("is-active");
    expect(wrapper.find("[data-testid='dcg-stream-0']").exists()).toBe(true);
  });

  it("码流可由宿主受控：底栏选子码流，侧栏显示的行跟着切", async () => {
    // ⛔ 底栏「参数对照」的码流与侧栏「配置文件」下拉必须是**同一路**：
    //    各持一份状态就会出现"对照卡说子码流、侧栏在改主码流"，而两边都不报错。
    api.getChannelVideoParams.mockResolvedValue(
      readOk([videoParamRow({ id: 1, streamNumber: 0 }), videoParamRow({ id: 2, streamNumber: 1, resolution: "4" })])
    );
    const wrapper = mountDrawer({ embedded: true, groupKeys: ["video-param", "osd"], streamProfile: "0" });
    await flushPromises();
    expect(wrapper.find("[data-testid='dcg-stream-0']").exists()).toBe(true);
    expect(wrapper.find("[data-testid='dcg-stream-1']").exists()).toBe(false);
    // 配置文件只是标题 + 下拉框的布局容器，不能用 label 包裹整个行宽，
    // 否则点击下拉框右侧空白也会触发原生 label 激活内部 select。
    expect(wrapper.get(".dcg-params-profile").element.tagName).toBe("DIV");

    await wrapper.setProps({ streamProfile: "1" });
    await flushPromises();
    expect(wrapper.find("[data-testid='dcg-stream-1']").exists()).toBe(true);
    expect(wrapper.find("[data-testid='dcg-stream-0']").exists()).toBe(false);

    // 侧栏自己改码流 → 同样只发意图，由宿主回写（回写后两条路仍指向同一路）。
    await wrapper.get("[aria-label='配置文件']").setValue("0");
    await flushPromises();
    expect(wrapper.emitted("update:streamProfile")?.at(-1)).toEqual(["0"]);
  });

  it("打开时按通道读一次平台缓存事实，且不带 refresh", async () => {
    mountDrawer();
    await flushPromises();
    expect(api.getChannelVideoParams).toHaveBeenCalledTimes(1);
    expect(api.getChannelVideoParams).toHaveBeenCalledWith(3539, false);
  });

  it("打开时把配置家族**一次问全**（六组的 ConfigType 并集）", async () => {
    mountDrawer();
    await flushPromises();

    expect(api.getChannelDeviceConfigs).toHaveBeenCalledTimes(1);
    const [channelId, options] = api.getChannelDeviceConfigs.mock.calls[0]!;
    expect(channelId).toBe(3539);
    expect(options.refresh).toBe(false);
    // ⛔ 一次问全而不是"读哪组问哪组"：A.2.4.7 本来就允许一次查多个类型；
    //    拆开会让"切分组"这个纯界面动作产生 SIP 报文。
    const expected = Array.from(new Set(CONFIG_GROUPS.flatMap(group => group.configTypes)));
    expect(options.configTypes).toEqual(expected);
    // 视频参数属性走独立通道，不该混进家族的查询清单
    expect(options.configTypes).not.toContain("VideoParamAttribute");
  });

  it("点「读取」才发 refresh=true 去设备拉取", async () => {
    const wrapper = mountDrawer();
    await flushPromises();
    api.getChannelVideoParams.mockClear();

    await wrapper.find("[data-testid='dcg-read']").trigger("click");
    await flushPromises();

    expect(api.getChannelVideoParams).toHaveBeenCalledWith(3539, true);
  });

  it("回读值渲染成码流行，并按码值回填控件", async () => {
    const wrapper = mountDrawer();
    await flushPromises();

    const row = wrapper.find("[data-testid='dcg-stream-0']");
    expect(row.exists()).toBe(true);
    expect(row.text()).toContain("主码流");
    // ⛔ 控件绑定的必须是**码值**（"2" 而不是 "H.264"），人读串只出现在行尾提示
    expect((wrapper.find("[data-testid='dcg-format-0']").element as HTMLSelectElement).value).toBe("2");
    expect((wrapper.find("[data-testid='dcg-resolution-0']").element as HTMLSelectElement).value).toBe("5");
    expect(row.text()).toContain("H.264");
    expect(row.text()).toContain("720P");
    // 回读成功态
    expect(wrapper.find("[data-testid='dcg-reconcile']").text()).toContain("回读成功");
  });

  it("嵌入播放控制台时按国标字段族呈现，并保留读取与下发动作", async () => {
    const wrapper = mountDrawer({ embedded: true });
    await flushPromises();

    expect(wrapper.find("[data-testid='dcg-standard-badge']").exists()).toBe(false);
    expect(wrapper.find(".dcg-params-summary").exists()).toBe(false);
    expect(wrapper.find(".dcg-params-std").exists()).toBe(false);
    expect(wrapper.find(".dcg-params-state").exists()).toBe(false);
    expect(wrapper.find("[data-testid='dcg-embedded-actions'] [data-testid='dcg-read']").exists()).toBe(true);
    expect(wrapper.find("[data-testid='dcg-embedded-actions'] [data-testid='dcg-apply']").exists()).toBe(true);

    const stream = wrapper.find("[data-testid='dcg-stream-0']");
    expect(stream.findAll("[data-testid='dcg-field-group']").map(group => group.attributes("data-group"))).toEqual([
      "encoding",
      "picture",
      "bitrate"
    ]);
    expect(stream.find("[data-group='encoding']").text()).toContain("编码");
    expect(stream.find("[data-group='picture']").text()).toContain("画面");
    expect(stream.find("[data-group='bitrate']").text()).toContain("CBR 时必填");
  });

  it("嵌入配置把复杂字段和唯一操作条挂到底部目标", async () => {
    api.getChannelDeviceConfigs.mockResolvedValue(deviceConfigOk([osdEntry()]));
    const target = document.createElement("div");
    document.body.appendChild(target);

    const wrapper = mountDrawer({
      embedded: true,
      configOnly: true,
      groupKeys: ["osd"],
      detailTarget: target
    });
    await flushPromises();

    expect(wrapper.find("[data-testid='dcg-field-items']").exists()).toBe(false);
    expect(target.querySelector("[data-testid='dcg-detail-fields']")).not.toBeNull();
    expect(target.querySelector("[data-testid='dcg-detail-field-items']")).not.toBeNull();
    expect(target.querySelector("[data-testid='dcg-embedded-actions'] [data-testid='dcg-read']")).not.toBeNull();
    expect(wrapper.findAll("[data-testid='dcg-embedded-actions']")).toHaveLength(0);

    wrapper.unmount();
    target.remove();
  });

  it("滑杆的 ± 微调改草稿，并点亮「下发」按钮", async () => {
    const wrapper = mountDrawer();
    await flushPromises();

    expect(wrapper.find("[data-testid='dcg-apply']").attributes("disabled")).toBeDefined();

    const slider = wrapper.find("[data-testid='dcg-frame-rate-0']");
    const steps = slider.findAll(".cfg-slider-step");
    expect(steps).toHaveLength(2);
    await steps[1]?.trigger("click");
    await flushPromises();

    // 25 → 26：微调按钮按 step 走，是精调的唯一手段
    expect((slider.find("input.cfg-slider-input").element as HTMLInputElement).value).toBe("26");
    expect(wrapper.find("[data-testid='dcg-apply']").attributes("disabled")).toBeUndefined();
    expect(wrapper.find("[data-testid='dcg-stream-0']").text()).toContain("已改");
  });

  it("VBR 时码率格禁用，并说明该元素不发", async () => {
    api.getChannelVideoParams.mockResolvedValue(readOk([videoParamRow({ bitRateType: "2", videoBitRate: null })]));
    const wrapper = mountDrawer();
    await flushPromises();

    expect(wrapper.find("[data-testid='dcg-bit-rate-0'] input.cfg-slider-input").attributes("disabled")).toBeDefined();
    // 徽标列统一两字，完整口径在 title 里
    const badge = wrapper.find("[data-testid='dcg-stream-0'] .dcg-row-hint[data-source='不发']");
    expect(badge.exists()).toBe(true);
    expect(badge.text()).toBe("不发");
    expect(badge.attributes("title")).toContain("VBR");
  });

  it("来源徽标按格判定：设备给了值标「设备」，没给标「缺省」", async () => {
    api.getChannelVideoParams.mockResolvedValue(
      readOk([videoParamRow({ resolution: "", frameRate: "25", bitRateType: "1", videoBitRate: "2000" })])
    );
    const wrapper = mountDrawer();
    await flushPromises();

    const row = wrapper.find("[data-testid='dcg-stream-0']");
    // 设备没上报分辨率 → 缺省
    expect(row.find(".dcg-row-hint[data-source='缺省']").exists()).toBe(true);
    expect(row.find(".dcg-row-hint[data-source='缺省']").attributes("title")).toContain("未上报");
    // 其余四格有值 → 设备
    expect(row.findAll(".dcg-row-hint[data-source='设备']")).toHaveLength(4);
  });

  it("配置家族分组已接入：切过去后控件可编辑、并显示回读值", async () => {
    api.getChannelDeviceConfigs.mockResolvedValue(deviceConfigOk([osdEntry()]));
    const wrapper = mountDrawer();
    await flushPromises();

    await wrapper.find("[data-testid='dcg-nav-osd']").trigger("click");
    await flushPromises();

    expect(wrapper.find("[data-testid='dcg-group-title']").text()).toBe("图像叠加 OSD");
    expect(wrapper.find("[data-testid='dcg-group-state']").text()).toBe("已接入");

    // ⛔ 控件必须**可编辑**：让人以为能下发是上一版的失败模式，反过来
    //    "接上了后端却还禁用"同样骗人 —— 用户会以为功能没做。
    expect(wrapper.find("[data-testid='osd-time-switch']").attributes("disabled")).toBeUndefined();
    const fmt = wrapper.get("[data-testid='osd-fmt-select']");
    expect(fmt.attributes("disabled")).toBeUndefined();

    // 回读值按协议键折进表单：timeType=1 → 下拉框选中第二项
    expect((fmt.element as HTMLSelectElement).value).toBe("1");
    // ⛔ 三选一**不再摊成三条竖排单选**（老板 2026-09-20：占掉半块面板的高度）。
    //    判据 = 选中项身上没有 `input[type=radio]` 了，只剩一个 `<select>`。
    expect(fmt.find("input").exists()).toBe(false);
    expect(fmt.findAll("option")).toHaveLength(3);
    // 格式选项的文案**不是模式串**：渲染的是当前时刻的实例（用户看的才是他会在画面上看到的东西）
    expect(fmt.get("[data-testid='osd-fmt-0']").text()).toMatch(/^\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}$/);
    expect(fmt.get("[data-testid='osd-fmt-1']").text()).toMatch(/^\d{4}年\d{2}月\d{2}日\d{2}:\d{2}:\d{2}$/);
    expect(fmt.get("[data-testid='osd-fmt-follow']").text()).toContain("跟设备走");
    // ⛔ 「窗口长度 / 窗口宽度」2026-09-20 起**不再是可编辑滑杆**：真机实测设备拒收平台改写
    //    （写 2560×1440 也回 200 OK、回读仍是 704×576），它们真正的身份是遮挡坐标的基准。
    //    现在只作为**只读事实**出现，值照旧取自回读。
    expect(wrapper.find("[data-testid='dcg-field-length']").exists()).toBe(false);
    expect(wrapper.find("[data-testid='dcg-field-timeX']").exists()).toBe(false);
    expect(wrapper.find("[data-testid='osd-canvas-value']").text()).toBe("1920 × 1080");
    expect(wrapper.find("[data-testid='osd-canvas-tag']").text()).toBe("只读");
    // 开关按 0/1 折成布尔
    expect(wrapper.find("[data-testid='osd-time-switch']").classes()).toContain("is-on");
    expect(wrapper.find("[data-testid='osd-text-switch']").classes()).not.toContain("is-on");
    // OSD 自由文本行（编号与画布锚点同源）
    expect((wrapper.find("[data-testid='osd-text-0']").element as HTMLInputElement).value).toBe("厂区东门");
    // 位置从两条滑杆改「在画面上拖 + 数值折叠」：默认收起，摘要里直接给出坐标
    expect(wrapper.find("[data-testid='osd-pos-exact']").exists()).toBe(false);
    expect(wrapper.find("[data-testid='osd-time-pos']").text()).toBe("X 10 · Y 20");
  });

  it("没有可拖的画布时不渲染「调整位置」（宿主没有画面时，那会是个点了没用的按钮）", async () => {
    api.getChannelDeviceConfigs.mockResolvedValue(deviceConfigOk([osdEntry()]));
    // `osd-canvas-linked` 关掉，正是「宿主没有画面」（设备详情那种用法）的情形。
    const wrapper = mountDrawer({ osdCanvasLinked: false });
    await flushPromises();
    await wrapper.find("[data-testid='dcg-nav-osd']").trigger("click");
    await flushPromises();

    expect(wrapper.find("[data-testid='osd-edit-toggle']").exists()).toBe(false);
    // 也没有"去定位"（同样要画布）——
    expect(wrapper.find("[data-testid='osd-locate-0']").exists()).toBe(false);
    // ⛔ 没有画布时**没有"在画面上拖"这条路**，精确数值必须默认展开，
    //    否则用户连改坐标的入口都没有。
    expect(wrapper.find("[data-testid='osd-pos-exact']").exists()).toBe(true);
    expect(wrapper.find("[data-testid='osd-pos-toggle']").exists()).toBe(false);
    expect(wrapper.find("[data-testid='osd-time-x']").exists()).toBe(true);
  });

  it("有画布时「调整位置」按钮在**时间戳面板里**，点它只发意图、不自己落状态", async () => {
    api.getChannelDeviceConfigs.mockResolvedValue(deviceConfigOk([osdEntry()]));
    const wrapper = mountDrawer({ osdCanvasLinked: true });
    await flushPromises();
    await wrapper.find("[data-testid='dcg-nav-osd']").trigger("click");
    await flushPromises();

    // ⛔ 按钮长在「时间戳」卡片内部，跟它控制的那个坐标读数同一行（老板 2026-09-20 指定）。
    const card = wrapper.get("[data-testid='osd-block-time']");
    const toggle = card.get("[data-testid='osd-edit-toggle']");
    expect(toggle.text()).toContain("调整位置");
    expect(toggle.attributes("data-active")).toBe("0");

    await toggle.trigger("click");
    await flushPromises();
    // 状态在宿主（画面侧）手里：这里只把意图放出来，按钮外观不自己翻 ——
    // 否则"抽屉以为开着、画面以为关着"，一边有锚点一边没有。
    expect(wrapper.emitted("toggleOsdEdit")).toHaveLength(1);
    expect(toggle.attributes("data-active")).toBe("0");

    // 宿主把状态传回来之后，按钮才显示成「完成调整」
    await wrapper.setProps({ osdEditing: true });
    expect(wrapper.get("[data-testid='osd-edit-toggle']").text()).toContain("完成调整");
    expect(wrapper.get("[data-testid='osd-edit-toggle']").attributes("data-active")).toBe("1");
  });

  it("改一格后下发：POST 出去的是**协议块**（键名 = 标准元素名小驼峰）", async () => {
    api.getChannelDeviceConfigs.mockResolvedValue(deviceConfigOk([osdEntry()]));
    api.applyChannelDeviceConfigs.mockResolvedValue({
      code: 0,
      message: "ok",
      data: { action: "apply-device-config", configTypes: ["OSDConfig"], reconcilePending: true }
    });
    const wrapper = mountDrawer();
    await flushPromises();

    await wrapper.find("[data-testid='dcg-nav-osd']").trigger("click");
    await flushPromises();

    // 刚回读完不应算"已改"：基准就是回读值
    expect(wrapper.find("[data-testid='dcg-apply']").attributes("disabled")).toBeDefined();

    await wrapper.find("[data-testid='osd-text-switch']").trigger("click");
    await flushPromises();
    expect(wrapper.find("[data-testid='dcg-apply']").attributes("disabled")).toBeUndefined();

    await wrapper.find("[data-testid='dcg-apply']").trigger("click");
    await flushPromises();

    const [channelId, blocks, key] = api.applyChannelDeviceConfigs.mock.calls[0]!;
    expect(channelId).toBe(3539);
    expect(String(key)).toContain("device-config-");
    // ⛔ 只发本组的块，且**键名必须是协议词汇表**（osdConfig），不是表单键
    expect(Object.keys(blocks as object)).toEqual(["osdConfig"]);
    const osdConfig = (blocks as { osdConfig: Record<string, unknown> }).osdConfig;
    expect(osdConfig.textEnable).toBe(1);
    expect(osdConfig.timeEnable).toBe(1);
    // 回读到的 timeType=1 必须**原样带出去**，不能被前端"补默认值"改掉
    expect(osdConfig.timeType).toBe(1);
    expect(osdConfig.items).toEqual([{ text: "厂区东门", x: 5, y: 6 }]);
    // 计数不独立存：SumNum 由后端按数组长度现算，前端不得自带
    expect(osdConfig.SumNum).toBeUndefined();
  });

  it("设备没返回某类型时给出说明，而不是把这一组说成操作失败", async () => {
    api.getChannelDeviceConfigs.mockResolvedValue(deviceConfigOk([], ["OSDConfig"]));
    const wrapper = mountDrawer();
    await flushPromises();

    await wrapper.find("[data-testid='dcg-nav-osd']").trigger("click");
    await flushPromises();

    const notice = wrapper.find("[data-testid='dcg-absent-notice']");
    expect(notice.exists()).toBe(true);
    expect(notice.text()).toContain("OSDConfig");
    // 回读本身是成功的（设备认这条通道），只是这一组没有内容
    expect(wrapper.find("[data-testid='dcg-reconcile']").text()).toContain("回读成功");
  });

  it("画面处理一组覆盖两个 ConfigType：下发时两块一起发", async () => {
    // ⛔ 前置必须是**真的回读到过**：没有设备事实时字段不可编辑（见下一条用例），
    //    拿空白模板改值下发就是"盲写设备"。
    // ⭐ 回读值刻意取「已启用 + 有一个区域」的真机形态，再**关掉总闸**下发 ——
    //    这正是"移除画面遮挡"的操作路径。带着空区域去开遮挡是设备不执行的组合，
    //    已由 payload 层的用例拒发，这里不再复现。
    api.getChannelDeviceConfigs.mockResolvedValue(
      deviceConfigOk([
        frameMirrorEntry(),
        pictureMaskEntry({ on: 1, regions: [{ seq: 1, left: 144, top: 295, right: 704, bottom: 576 }] })
      ])
    );
    api.applyChannelDeviceConfigs.mockResolvedValue({
      code: 0,
      message: "ok",
      data: { action: "apply-device-config", reconcilePending: true }
    });
    const wrapper = mountDrawer();
    await flushPromises();

    await wrapper.find("[data-testid='dcg-nav-picture']").trigger("click");
    await flushPromises();

    // 回读是「启用」⇒ 点一下就是「停用」（移除遮挡）。
    await wrapper.find("[data-testid='dcg-field-maskOn'] .dcg-switch").trigger("click");
    await flushPromises();
    await wrapper.find("[data-testid='dcg-apply']").trigger("click");
    await flushPromises();

    const blocks = api.applyChannelDeviceConfigs.mock.calls[0]![1] as Record<string, unknown>;
    expect(Object.keys(blocks).sort()).toEqual(["frameMirror", "pictureMask"]);
    expect((blocks.frameMirror as { value: number }).value).toBe(0);
    expect((blocks.pictureMask as { on: number }).on).toBe(0);
    // 关闭时**照常带上区域**：关闭态的区域不参与对账（后端忽略），但发出去能让
    // 设备的意图完整，不必依赖"设备替我保留"这种没写进标准的行为。
    expect((blocks.pictureMask as { regions: unknown[] }).regions).toEqual([
      { seq: 1, left: 144, top: 295, right: 704, bottom: 576 }
    ]);
  });

  it("清掉最后一个遮挡区域时**顺手关掉总闸**（否则会拼出设备不执行的组合）", async () => {
    // ⛔ 2026-09-19 现场：操作员想移除遮挡，于是清空了区域 —— 但总闸还开着，
    //    下发的是 `{on:1,regions:[]}`。设备保留原有区域、遮挡照旧生效，
    //    对账又报 `PictureMask.regions=[]...`，看起来像"平台没生效"。
    //    与 setPictureRegion 的"画了区域顺手开总闸"对称：最后一个区域没了就顺手关掉。
    api.getChannelDeviceConfigs.mockResolvedValue(
      deviceConfigOk([
        frameMirrorEntry(),
        pictureMaskEntry({ on: 1, regions: [{ seq: 1, left: 144, top: 295, right: 704, bottom: 576 }] })
      ])
    );
    api.applyChannelDeviceConfigs.mockResolvedValue({
      code: 0,
      message: "ok",
      data: { action: "apply-device-config", reconcilePending: true }
    });
    const wrapper = mountDrawer();
    await flushPromises();

    await wrapper.find("[data-testid='dcg-nav-picture']").trigger("click");
    await flushPromises();
    expect(wrapper.find("[data-testid='dcg-field-maskOn'] .dcg-switch").classes()).toContain("is-on");

    // 播放控制台画布上的「删掉这个框」走的正是这个 expose 入口。
    (wrapper.vm as unknown as { clearPictureRegion(seq: number): void }).clearPictureRegion(1);
    await flushPromises();

    expect(wrapper.find("[data-testid='dcg-field-maskOn'] .dcg-switch").classes()).not.toContain("is-on");

    await wrapper.find("[data-testid='dcg-apply']").trigger("click");
    await flushPromises();

    const blocks = api.applyChannelDeviceConfigs.mock.calls[0]![1] as Record<string, unknown>;
    expect((blocks.pictureMask as { on: number }).on).toBe(0);
    expect((blocks.pictureMask as { regions: unknown[] }).regions).toEqual([]);
  });

  it("启用遮挡却没画区域：**照发**，并把「看不到遮挡」交给画面卡片", async () => {
    // ⛔ 这里的期望在 2026-09-19 被真机实测推翻过一次：原来是"本地拒发"，
    //    理由是"设备收到 `On=1` 又拿不到区域列表 ⇒ 保留原有区域"。
    //    但实测（海康 IPC）表明设备**接受**这个请求（回读 `On=1`、不凭空创建区域），
    //    而协议里根本没有独立的"启用信令"——`On` 只是 `PictureMask` 里的一个字段。
    //    ⇒ 拦住它只会让操作员看到"点了什么都不发生"，归因不到"是本地拦的"。
    // ⭐ 放行后仍**必须说出来**：本次没带区域 ⇒ 画面上不会有可见遮挡。
    //    这条提示走的是**独立于 error** 的通道 —— 它不是失败，不该染成危险色，
    //    也不该被 `clearPictureError`（用户一还原草稿就清）顺手抹掉。
    api.getChannelDeviceConfigs.mockResolvedValue(deviceConfigOk([frameMirrorEntry(), pictureMaskEntry({ on: 0, regions: [] })]));
    api.applyChannelDeviceConfigs.mockResolvedValue({
      code: 0,
      message: "ok",
      data: { action: "apply-device-config", configTypes: ["PictureMask"], reconcilePending: false }
    });
    const wrapper = mountDrawer();
    await flushPromises();

    await wrapper.find("[data-testid='dcg-nav-picture']").trigger("click");
    await flushPromises();

    // 打开总闸，但一块区域都不画。
    await wrapper.find("[data-testid='dcg-field-maskOn'] .dcg-switch").trigger("click");
    await flushPromises();
    await wrapper.find("[data-testid='dcg-apply']").trigger("click");
    await flushPromises();

    // 照发：请求带 `On=1` 出去，不再是"一个请求都不出去"。
    expect(api.applyChannelDeviceConfigs).toHaveBeenCalledTimes(1);
    const blocks = api.applyChannelDeviceConfigs.mock.calls[0]![1] as Record<string, unknown>;
    expect((blocks.pictureMask as { on: number }).on).toBe(1);
    const exposed = wrapper.vm as unknown as { pictureError: string; pictureMaskNotice: string };
    // 没有错误（这次什么都没失败）……
    expect(exposed.pictureError).toBe("");
    // ……但有一句提示：本次没带区域 + 设备里没有残留区域 ⇒ 画面不会变。
    expect(exposed.pictureMaskNotice).toContain("没有携带任何遮挡区域");
    expect(exposed.pictureMaskNotice).toContain("画面上不会有任何变化");
  });

  it("禁用遮挡不产生提示：那是干净路径（后端把区域铺成零面积删掉）", async () => {
    // 与上一条互为反向锚点：少了它，`pictureMaskApplyNotice` 很容易被写成"只要下发
    // 画面组就提示一句"，用户每次停用都要读一句莫名其妙的警告。
    api.getChannelDeviceConfigs.mockResolvedValue(
      deviceConfigOk([
        frameMirrorEntry(),
        pictureMaskEntry({ on: 1, regions: [{ seq: 1, left: 144, top: 295, right: 704, bottom: 576 }] })
      ])
    );
    api.applyChannelDeviceConfigs.mockResolvedValue({
      code: 0,
      message: "ok",
      data: { action: "apply-device-config", configTypes: ["PictureMask"], reconcilePending: false }
    });
    const wrapper = mountDrawer();
    await flushPromises();

    await wrapper.find("[data-testid='dcg-nav-picture']").trigger("click");
    await flushPromises();
    await wrapper.find("[data-testid='dcg-field-maskOn'] .dcg-switch").trigger("click");
    await flushPromises();
    await wrapper.find("[data-testid='dcg-apply']").trigger("click");
    await flushPromises();

    const exposed = wrapper.vm as unknown as { pictureError: string; pictureMaskNotice: string };
    expect(exposed.pictureError).toBe("");
    expect(exposed.pictureMaskNotice).toBe("");
  });

  it("遮挡坐标基准取**设备声明的图像画布**（OSD Length/Width），不是画面解码尺寸", async () => {
    // ⭐ 2026-09-19 真机定因（海康 IPC 192.168.10.203，主码流 2560×1440）：
    //    设备 `OSDConfig` 回读 `Length=704 Width=576`，遮挡 `Point` 就是按这把尺子解释的
    //    （发 `0,0,640,360` ⇒ 实测黑块 x 0~90.8% / y 0~62.6% = 640/704、360/576）。
    // ⛔ 平台此前拿解码尺寸当基准 ⇒ 遮挡块整体右移放大，"我画的框挡住了别的地方"。
    api.getChannelDeviceConfigs.mockResolvedValue(
      deviceConfigOk([
        osdEntry({ length: 704, width: 576 }),
        frameMirrorEntry(),
        pictureMaskEntry({ on: 1, regions: [{ seq: 1, left: 77, top: 132, right: 232, bottom: 265 }] })
      ])
    );
    const wrapper = mountDrawer();
    await flushPromises();
    await wrapper.find("[data-testid='dcg-nav-picture']").trigger("click");
    await flushPromises();

    const exposed = wrapper.vm as unknown as {
      pictureCanvasSize: { width: number; height: number } | null;
    };
    expect(exposed.pictureCanvasSize).toEqual({ width: 704, height: 576 });
    // 手填坐标的那四个输入框也得知道基准：同一个 `640` 在 704 画布上是 91% 宽、
    // 在 2560 画布上只有 25% 宽。
    expect(wrapper.get("[data-testid='dcg-field-mask1']").text()).toContain("坐标基准 704×576");
  });

  it("没读到 OSD 事实时**不给画布**：模板里的 1920×1080 不是设备声明", async () => {
    // ⛔ `familyBaseline` 平时躺着的是 `CONFIG_GROUP_DEFAULTS` 的 1920×1080 —— 那是
    //    **平台的初值、不是设备值**。拿它当画布比拿解码尺寸更糟：它会伪装成"设备声明"，
    //    而且错得没有任何征兆（卡片上写着"设备声明的图像尺寸"，其实是模板）。
    api.getChannelDeviceConfigs.mockResolvedValue(
      deviceConfigOk([
        frameMirrorEntry(),
        pictureMaskEntry({ on: 1, regions: [{ seq: 1, left: 77, top: 132, right: 232, bottom: 265 }] })
      ])
    );
    const wrapper = mountDrawer();
    await flushPromises();
    await wrapper.find("[data-testid='dcg-nav-picture']").trigger("click");
    await flushPromises();

    const exposed = wrapper.vm as unknown as { pictureCanvasSize: unknown };
    expect(exposed.pictureCanvasSize).toBeNull();
  });

  it("零面积区域不算「已配置」：设备回读的删除痕迹不是一条遮挡（反向锚点：有面积的照旧算）", async () => {
    // 平台停用遮挡时把 `Seq 1..4` 全铺成零面积让设备删；这台海康会把它按自己的画布
    // 回读回来（实测 `704,576,704,576`，`Num` 仍是 1）。按"四个数不全为 0"判 ⇒
    // 画面什么都没有、卡片却列着一条区域 —— 与"设备已清除、页面还有一条数据"同症状。
    api.getChannelDeviceConfigs.mockResolvedValue(
      deviceConfigOk([
        frameMirrorEntry(),
        pictureMaskEntry({
          on: 0,
          regions: [
            { seq: 1, left: 704, top: 576, right: 704, bottom: 576 },
            { seq: 2, left: 1, top: 1, right: 2, bottom: 2 }
          ]
        })
      ])
    );
    const wrapper = mountDrawer();
    await flushPromises();
    await wrapper.find("[data-testid='dcg-nav-picture']").trigger("click");
    await flushPromises();

    const exposed = wrapper.vm as unknown as {
      pictureRegions: Array<{ seq: number; used: boolean }>;
      pictureRegionCount: number;
      pictureRetainedRegionCount: number;
    };
    expect(exposed.pictureRegions[0]!.used).toBe(false);
    // 反向锚点：只判"零面积"是不够的 —— 有面积的那条不能被一起判死。
    expect(exposed.pictureRegions[1]!.used).toBe(true);
    expect(exposed.pictureRegionCount).toBe(1);
    expect(exposed.pictureRetainedRegionCount).toBe(1);
  });

  it("设备停用但留着旧区域时清掉草稿再启用 ⇒ 提示「旧区域会被一并清掉」，不是「画面不会变」", async () => {
    // ⛔ 与上一条互为边界，也是海康最真实的一种形态：停用只关开关、**不清区域列表**。
    //    这时把草稿区域清掉再只发 `On=1`，报文层会把没给的槽位补成零面积
    //    （后端 `pictureMaskFullRegions`）⇒ 设备上那片旧区域**会被删掉**。
    //    说"画面上不会有任何变化"就是错的 —— 用户会以为平台偷删了他的配置。
    //    （2026-09-20 口径变更：这句原来是「启用后它们会一起生效」，那是"省略的槽位
    //    设备会保留"时代的说法；真机实测证明省略 ≠ 删除，旧文案已不成立。）
    //    直接调 Drawer 暴露的入口构造这个形态：播放控制台的卡片在这种设备形态下
    //    走的是"已停用 + 设备仍保留 N 个区域"说明态，根本不渲染槽位网格、删不掉区域。
    api.getChannelDeviceConfigs.mockResolvedValue(
      deviceConfigOk([
        frameMirrorEntry(),
        pictureMaskEntry({ on: 0, regions: [{ seq: 1, left: 144, top: 295, right: 704, bottom: 576 }] })
      ])
    );
    api.applyChannelDeviceConfigs.mockResolvedValue({
      code: 0,
      message: "ok",
      data: { action: "apply-device-config", configTypes: ["PictureMask"], reconcilePending: false }
    });
    const wrapper = mountDrawer();
    await flushPromises();
    const exposed = wrapper.vm as unknown as {
      clearPictureRegion: (seq: number) => void;
      setPictureMaskOn: (value: boolean) => void;
      applyPictureMaskOnly: () => Promise<void>;
      pictureMaskNotice: string;
    };

    // 清掉草稿区域（设备事实不动）⇒ 报文里不会携带任何**区域**（意图层）。
    exposed.clearPictureRegion(1);
    exposed.setPictureMaskOn(true);
    await exposed.applyPictureMaskOnly();
    await flushPromises();

    expect(exposed.pictureMaskNotice).toContain("没有携带任何遮挡区域");
    expect(exposed.pictureMaskNotice).toContain("设备里还留着 1 个旧遮挡区域");
    expect(exposed.pictureMaskNotice).toContain("会被一并清掉");
    expect(exposed.pictureMaskNotice).not.toContain("画面上不会有任何变化");
    expect(exposed.pictureMaskNotice).not.toContain("会一起生效");
  });

  it("点开关即时下发：**只发 PictureMask**，不捎带镜像草稿；发完草稿即基准", async () => {
    // 2026-09-19 产品决定：总闸改成"点即发"。
    // ⛔ 这里钉两个容易做错的点：
    //    ① 只发遮挡那一块 —— 用户点的是遮挡开关，不该顺手把还没点下发的镜像草稿提交出去；
    //    ② 发完把遮挡草稿推成基准 —— 否则浮条会一直挂着"1 项画面改动未下发"、
    //       按钮也一直停在"将启用"，看起来像根本没发出去。
    api.getChannelDeviceConfigs.mockResolvedValue(
      deviceConfigOk([
        frameMirrorEntry(),
        pictureMaskEntry({ on: 0, regions: [{ seq: 1, left: 144, top: 295, right: 704, bottom: 576 }] })
      ])
    );
    api.applyChannelDeviceConfigs.mockResolvedValue({
      code: 0,
      message: "ok",
      data: { action: "apply-device-config", configTypes: ["PictureMask"], reconcilePending: false }
    });
    const wrapper = mountDrawer();
    await flushPromises();
    const exposed = wrapper.vm as unknown as {
      setPictureMirror: (value: string) => void;
      setPictureMaskOn: (value: boolean) => void;
      applyPictureMaskOnly: () => Promise<void>;
      pictureDirtyCount: number;
      pictureError: string;
    };

    // 先把镜像改脏：它必须**留在草稿里**，不能被这次遮挡下发顺手提交。
    exposed.setPictureMirror("1");
    exposed.setPictureMaskOn(true);
    await exposed.applyPictureMaskOnly();
    await flushPromises();

    expect(api.applyChannelDeviceConfigs).toHaveBeenCalledTimes(1);
    const blocks = api.applyChannelDeviceConfigs.mock.calls[0]![1] as Record<string, unknown>;
    expect(Object.keys(blocks)).toEqual(["pictureMask"]);
    expect((blocks.pictureMask as { on: number }).on).toBe(1);
    // 镜像草稿还在（脏值剩 1 项 = mirror），没有被这次下发吞掉。
    expect(exposed.pictureDirtyCount).toBe(1);
    expect(exposed.pictureError).toBe("");
  });

  it("下发错误按组归属：别的组的错不会跑到画面卡片上", async () => {
    // ⛔ `familyError` 是跨组共用的一个槽。不记归属的话，用户在「图像叠加」组撞的错
    //    会顶到画面卡片的提示条上 —— 卡片在讲遮挡、提示条在讲 OSD，只会把人带偏。
    api.getChannelDeviceConfigs.mockResolvedValue(
      deviceConfigOk([osdEntry(), frameMirrorEntry(), pictureMaskEntry({ on: 0, regions: [] })])
    );
    api.applyChannelDeviceConfigs.mockRejectedValueOnce(new Error("设备离线"));
    const wrapper = mountDrawer();
    await flushPromises();
    const exposed = wrapper.vm as unknown as { pictureError: string };

    // ① 先在 OSD 组撞一次真实的传输失败。
    await wrapper.find("[data-testid='dcg-nav-osd']").trigger("click");
    await flushPromises();
    await wrapper.find("[data-testid='osd-text-switch']").trigger("click");
    await flushPromises();
    await wrapper.find("[data-testid='dcg-apply']").trigger("click");
    await flushPromises();

    expect(wrapper.find("[data-testid='dcg-error']").text()).toContain("设备离线");
    expect(exposed.pictureError).toBe("");

    // ② 轮到画面组自己撞一次传输失败时，才该是它出面。
    //    （原来这里借的是"画面组被本地守卫拒发"来造错；2026-09-19 那道守卫改成
    //     "放行 + 提示"了，所以改用同样的传输失败 —— 要验的是**归属**，不是错的来源。）
    api.applyChannelDeviceConfigs.mockRejectedValueOnce(new Error("设备离线"));
    await wrapper.find("[data-testid='dcg-nav-picture']").trigger("click");
    await flushPromises();
    await wrapper.find("[data-testid='dcg-field-maskOn'] .dcg-switch").trigger("click");
    await flushPromises();
    await wrapper.find("[data-testid='dcg-apply']").trigger("click");
    await flushPromises();

    expect(exposed.pictureError).toContain("设备离线");
  });

  it("差异抽屉：从「N 项未下发」展开逐项差异，并能单项撤销", async () => {
    api.getChannelDeviceConfigs.mockResolvedValue(deviceConfigOk([osdEntry()]));
    const wrapper = mountDrawer();
    await flushPromises();

    await wrapper.find("[data-testid='dcg-nav-osd']").trigger("click");
    await flushPromises();

    // 刚回读完 ⇒ 没有未下发改动，也就没有抽屉开关
    expect(wrapper.find("[data-testid='dcg-pending-toggle']").exists()).toBe(false);

    await wrapper.find("[data-testid='osd-time-switch']").trigger("click");
    await flushPromises();

    const toggle = wrapper.find("[data-testid='dcg-pending-toggle']");
    expect(toggle.text()).toContain("1 项未下发");
    // 未点开之前不渲染清单
    expect(wrapper.find("[data-testid='dcg-pending']").exists()).toBe(false);

    await toggle.trigger("click");
    await flushPromises();

    const row = wrapper.find("[data-testid='dcg-pending-row-timeEnable']");
    expect(row.exists()).toBe(true);
    expect(row.text()).toContain("时间显示");
    // ⛔ 两侧都是人读形态：开关要说「开/关」，不能把 0/1 丢到屏幕上
    expect(row.find(".dcg-pending-from").text()).toBe("开");
    expect(row.find(".dcg-pending-to").text()).toBe("关");

    await wrapper.find("[data-testid='dcg-revert-timeEnable']").trigger("click");
    await flushPromises();

    // 撤销回到设备值：计数归零、控件也回到原状
    expect(wrapper.find("[data-testid='dcg-pending-toggle']").exists()).toBe(false);
    expect(wrapper.find("[data-testid='osd-time-switch']").classes()).toContain("is-on");
  });

  it("差异抽屉的行数与计数同源（不会说 2 项只列 1 行）", async () => {
    api.getChannelDeviceConfigs.mockResolvedValue(deviceConfigOk([osdEntry()]));
    const wrapper = mountDrawer();
    await flushPromises();

    await wrapper.find("[data-testid='dcg-nav-osd']").trigger("click");
    await flushPromises();

    await wrapper.find("[data-testid='osd-time-switch']").trigger("click");
    await wrapper.find("[data-testid='osd-text-switch']").trigger("click");
    await flushPromises();

    const toggle = wrapper.find("[data-testid='dcg-pending-toggle']");
    expect(toggle.text()).toContain("2 项未下发");

    await toggle.trigger("click");
    await flushPromises();

    expect(wrapper.findAll(".dcg-pending-row")).toHaveLength(2);
  });

  it("画面镜像用四个方向按钮表达，且每个按钮都带文字副标题", async () => {
    api.getChannelDeviceConfigs.mockResolvedValue(deviceConfigOk([frameMirrorEntry({ value: 1 }), pictureMaskEntry()]));
    const wrapper = mountDrawer();
    await flushPromises();

    await wrapper.find("[data-testid='dcg-nav-picture']").trigger("click");
    await flushPromises();

    const buttons = wrapper.findAll(".dcg-mirror-btn");
    expect(buttons).toHaveLength(4);
    // ⛔ 图标不能替代文字：对账比的是值，不是画面对不对 —— 副标题就是给对账看的
    expect(buttons.map(button => button.text())).toEqual(["原图", "左右", "上下", "中心"]);
    // 回读值 1 = 水平镜像 → 「左右」高亮
    expect(wrapper.find("[data-testid='dcg-mirror-mirror-1']").classes()).toContain("is-active");
    expect(wrapper.find("[data-testid='dcg-mirror-mirror-0']").classes()).not.toContain("is-active");
    // 悬浮说明里要能看到标准取值，避免"图标对了、值写反了"没人发现
    expect(wrapper.find("[data-testid='dcg-mirror-mirror-1']").attributes("title")).toContain("值 1");

    await wrapper.find("[data-testid='dcg-mirror-mirror-2']").trigger("click");
    await flushPromises();

    expect(wrapper.find("[data-testid='dcg-mirror-mirror-2']").classes()).toContain("is-active");
    expect(wrapper.find("[data-testid='dcg-mirror-mirror-1']").classes()).not.toContain("is-active");
    expect(wrapper.find("[data-testid='dcg-pending-toggle']").text()).toContain("1 项未下发");
  });

  it("没拿到设备事实时不给控件：字段只显示「—」，杜绝在空白模板上盲写", async () => {
    // 读成功，但设备一块都没返回（既不是设备值，也不是"不支持"）
    api.getChannelDeviceConfigs.mockResolvedValue(deviceConfigOk([]));
    const wrapper = mountDrawer();
    await flushPromises();

    await wrapper.find("[data-testid='dcg-nav-picture']").trigger("click");
    await flushPromises();

    // 控件不渲染 ⇒ 造不出脏值 ⇒ 下发按钮不会被点亮
    expect(wrapper.find("[data-testid='dcg-field-maskOn'] .dcg-switch").exists()).toBe(false);
    expect(wrapper.find("[data-testid='dcg-field-unknown-maskOn']").text()).toBe("—");
    // coords 字段同待遇（非 embedded 时它也在主字段区）
    expect(wrapper.find("[data-testid='dcg-field-unknown-mask1']").text()).toBe("—");
    expect(wrapper.find("[data-testid='dcg-no-facts-notice']").exists()).toBe(true);
    expect(wrapper.find("[data-testid='dcg-apply']").attributes("disabled")).toBeDefined();
  });

  it("设备未返回该类型（type_absent）时同样不给编辑，说明是「不支持」而不是「值是 0」", async () => {
    api.getChannelDeviceConfigs.mockResolvedValue(deviceConfigOk([], ["FrameMirror", "PictureMask"]));
    const wrapper = mountDrawer();
    await flushPromises();

    await wrapper.find("[data-testid='dcg-nav-picture']").trigger("click");
    await flushPromises();

    expect(wrapper.find("[data-testid='dcg-field-maskOn'] .dcg-switch").exists()).toBe(false);
    expect(wrapper.find("[data-testid='dcg-field-unknown-maskOn']").text()).toBe("—");
    // 说明文案必须点名"设备未返回"，而不是让用户以为遮挡被关掉了
    expect(wrapper.find("[data-testid='dcg-absent-notice']").text()).toContain("FrameMirror");
  });

  it("已回读到的组仍可正常编辑（新闸门不能误伤）", async () => {
    api.getChannelDeviceConfigs.mockResolvedValue(deviceConfigOk([frameMirrorEntry(), pictureMaskEntry()]));
    const wrapper = mountDrawer();
    await flushPromises();

    await wrapper.find("[data-testid='dcg-nav-picture']").trigger("click");
    await flushPromises();

    expect(wrapper.find("[data-testid='dcg-field-maskOn'] .dcg-switch").attributes("disabled")).toBeUndefined();
    expect(wrapper.find("[data-testid='dcg-no-facts-notice']").exists()).toBe(false);
  });

  it("导航项按接入状态给标记：七组全部 ready（含两条通道）", async () => {
    const wrapper = mountDrawer();
    await flushPromises();

    expect(wrapper.find("[data-testid='dcg-nav-video-param']").attributes("data-state")).toBe("ready");
    expect(wrapper.find("[data-testid='dcg-nav-osd']").attributes("data-state")).toBe("ready");
    expect(wrapper.findAll(".dcg-nav-flag.is-ready")).toHaveLength(CONFIG_GROUPS.length);
    expect(wrapper.findAll(".dcg-nav-flag.is-static")).toHaveLength(0);
  });

  it("2022 独有分组才带版本徽章：基本参数两版都有，不带", async () => {
    const wrapper = mountDrawer();
    await flushPromises();

    // 视频参数属性是 2022 新增
    expect(wrapper.find("[data-testid='dcg-standard-badge']").exists()).toBe(true);

    await wrapper.find("[data-testid='dcg-nav-basic']").trigger("click");
    await flushPromises();
    expect(wrapper.find("[data-testid='dcg-standard-badge']").exists()).toBe(false);

    await wrapper.find("[data-testid='dcg-nav-picture']").trigger("click");
    await flushPromises();
    expect(wrapper.find("[data-testid='dcg-standard-badge']").exists()).toBe(true);
  });

  it("关闭按钮 emit update:visible=false", async () => {
    const wrapper = mountDrawer();
    await flushPromises();

    await wrapper.find("[data-testid='dcg-close']").trigger("click");
    expect(wrapper.emitted("update:visible")?.[0]).toEqual([false]);
  });
});
