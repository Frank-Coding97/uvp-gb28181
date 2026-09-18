import { flushPromises, mount } from "@vue/test-utils";
import { beforeEach, describe, expect, it, vi } from "vitest";
import DeviceConfigDrawer from "./DeviceConfigDrawer.vue";
import { CONFIG_GROUPS } from "./deviceConfigGroups";

const api = vi.hoisted(() => ({
    getChannelVideoParams: vi.fn(),
    applyChannelVideoParams: vi.fn()
}));

vi.mock("@/api/gb28181", () => api);

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

function mountDrawer(props: Record<string, unknown> = {}) {
    return mount(DeviceConfigDrawer, {
        props: {
            visible: true,
            deviceName: "UVP-Sim",
            deviceCode: "37010301021180000007",
            channelName: "前置摄像头",
            online: true,
            effectiveVersion: "2022",
            channelId: 3539,
            ...props
        }
    });
}

describe("DeviceConfigDrawer 设备配置中心", () => {
    beforeEach(() => {
        api.getChannelVideoParams.mockReset();
        api.applyChannelVideoParams.mockReset();
        api.getChannelVideoParams.mockResolvedValue(readOk());
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

    it("打开时按通道读一次平台缓存事实，且不带 refresh", async () => {
        mountDrawer();
        await flushPromises();
        expect(api.getChannelVideoParams).toHaveBeenCalledTimes(1);
        expect(api.getChannelVideoParams).toHaveBeenCalledWith(3539, false);
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

    it("未接入的分组：标「未接入」、控件禁用、给出静态说明", async () => {
        const wrapper = mountDrawer();
        await flushPromises();

        await wrapper.find("[data-testid='dcg-nav-osd']").trigger("click");
        await flushPromises();

        expect(wrapper.find("[data-testid='dcg-group-title']").text()).toBe("图像叠加 OSD");
        expect(wrapper.find("[data-testid='dcg-group-state']").text()).toBe("未接入");
        expect(wrapper.find("[data-testid='dcg-static-note']").exists()).toBe(true);
        // ⛔ 未接入组一律禁用：让人以为能下发是这版最危险的失败模式
        expect(wrapper.find("[data-testid='dcg-field-timeShow'] .dcg-switch").attributes("disabled")).toBeDefined();
        expect(wrapper.find("[data-testid='dcg-field-dateFormat'] select").attributes("disabled")).toBeDefined();
    });

    it("导航项按接入状态给标记，只有视频参数属性是 ready", async () => {
        const wrapper = mountDrawer();
        await flushPromises();

        expect(wrapper.find("[data-testid='dcg-nav-video-param']").attributes("data-state")).toBe("ready");
        expect(wrapper.find("[data-testid='dcg-nav-osd']").attributes("data-state")).toBe("static");
        expect(wrapper.findAll(".dcg-nav-flag.is-ready")).toHaveLength(1);
    });

    it("关闭按钮 emit update:visible=false", async () => {
        const wrapper = mountDrawer();
        await flushPromises();

        await wrapper.find("[data-testid='dcg-close']").trigger("click");
        expect(wrapper.emitted("update:visible")?.[0]).toEqual([false]);
    });
});
