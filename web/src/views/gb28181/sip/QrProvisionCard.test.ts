import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { flushPromises, mount } from "@vue/test-utils";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import QrProvisionCard from "./QrProvisionCard.vue";

const api = vi.hoisted(() => ({
    generateSipQrToken: vi.fn()
}));

vi.mock("@/api/gb28181", () => api);
vi.mock("@arco-design/web-vue", () => ({
    Message: { success: vi.fn(), error: vi.fn(), warning: vi.fn() }
}));

function mountCard(props: { canGenerate?: boolean } = {}) {
    return mount(QrProvisionCard, {
        props,
        global: {
            stubs: {
                SQrcodeDraw: { template: "<span data-qrcode />" },
                "a-form": { template: "<form><slot /></form>" },
                "a-form-item": {
                    props: ["label", "required", "validateStatus", "help"],
                    template: "<div><slot /></div>"
                },
                "a-input": {
                    props: ["modelValue"],
                    emits: ["update:modelValue"],
                    template: `<input :value="modelValue" @input="$emit('update:modelValue', $event.target.value)" />`
                },
                "a-button": {
                    props: ["loading", "disabled"],
                    emits: ["click"],
                    template: `<button :disabled="loading || disabled" @click="$emit('click')"><slot name="icon" /><slot /></button>`
                }
            }
        }
    });
}

function okResponse(token: string, expiresInSeconds: number) {
    return { code: 0, message: "", data: { token, expiresInSeconds } };
}

describe("QrProvisionCard 自动续码", () => {
    beforeEach(() => {
        vi.useFakeTimers();
        api.generateSipQrToken.mockReset();
    });

    afterEach(() => {
        vi.useRealTimers();
    });

    it("过期后自动续码成功", async () => {
        api.generateSipQrToken
            .mockResolvedValueOnce(okResponse("tok-1", 1))
            .mockResolvedValueOnce(okResponse("tok-2", 60));

        const wrapper = mountCard();
        await flushPromises();
        expect(api.generateSipQrToken).toHaveBeenCalledTimes(1);

        // 倒计时走完 → 触发自动续码
        vi.advanceTimersByTime(1100);
        await flushPromises();

        expect(api.generateSipQrToken).toHaveBeenCalledTimes(2);
        wrapper.unmount();
    });

    it("续码失败按退避重试,3 次后放弃", async () => {
        api.generateSipQrToken
            .mockResolvedValueOnce(okResponse("tok-1", 1))
            .mockRejectedValue(new Error("network down"));

        const wrapper = mountCard();
        await flushPromises();
        expect(api.generateSipQrToken).toHaveBeenCalledTimes(1);

        vi.advanceTimersByTime(1100); // 过期 → 自动续码失败(第 2 次调用)
        await flushPromises();
        expect(api.generateSipQrToken).toHaveBeenCalledTimes(2);

        vi.advanceTimersByTime(5000); // 退避重试 1(第 3 次)
        await flushPromises();
        expect(api.generateSipQrToken).toHaveBeenCalledTimes(3);

        vi.advanceTimersByTime(10000); // 退避重试 2(第 4 次)
        await flushPromises();
        expect(api.generateSipQrToken).toHaveBeenCalledTimes(4);

        vi.advanceTimersByTime(20000); // 退避重试 3(第 5 次)
        await flushPromises();
        expect(api.generateSipQrToken).toHaveBeenCalledTimes(5);

        // 重试上限已到,不再调度
        vi.advanceTimersByTime(60000);
        await flushPromises();
        expect(api.generateSipQrToken).toHaveBeenCalledTimes(5);
        wrapper.unmount();
    });

    it("卸载后停止续码重试调度", async () => {
        api.generateSipQrToken
            .mockResolvedValueOnce(okResponse("tok-1", 1))
            .mockRejectedValue(new Error("network down"));

        const wrapper = mountCard();
        await flushPromises();

        vi.advanceTimersByTime(1100); // 过期 → 自动续码失败,进入 5s 重试等待
        await flushPromises();
        expect(api.generateSipQrToken).toHaveBeenCalledTimes(2);

        wrapper.unmount();

        vi.advanceTimersByTime(30000);
        await flushPromises();
        expect(api.generateSipQrToken).toHaveBeenCalledTimes(2);
    });

    it("卸载期间在途续码请求完成后不再调度重试", async () => {
        let rejectInFlight: ((e: Error) => void) | undefined;
        api.generateSipQrToken
            .mockResolvedValueOnce(okResponse("tok-1", 1))
            .mockImplementationOnce(
                () =>
                    new Promise((_, reject) => {
                        rejectInFlight = reject;
                    })
            );

        const wrapper = mountCard();
        await flushPromises();

        vi.advanceTimersByTime(1100); // 过期 → autoRenew 发起在途请求
        await flushPromises();
        expect(api.generateSipQrToken).toHaveBeenCalledTimes(2);

        wrapper.unmount();

        // 在途请求在卸载后才失败返回:不得再调度退避重试
        rejectInFlight!(new Error("network down"));
        await flushPromises();

        vi.advanceTimersByTime(30000);
        await flushPromises();
        expect(api.generateSipQrToken).toHaveBeenCalledTimes(2);
    });

    it("卸载期间在途续码成功返回不重建计时器", async () => {
        let resolveInFlight: ((r: unknown) => void) | undefined;
        api.generateSipQrToken
            .mockResolvedValueOnce(okResponse("tok-1", 1))
            .mockImplementationOnce(
                () =>
                    new Promise(resolve => {
                        resolveInFlight = resolve;
                    })
            );

        const wrapper = mountCard();
        await flushPromises();

        vi.advanceTimersByTime(1100); // 过期 → autoRenew 发起在途请求
        await flushPromises();
        expect(api.generateSipQrToken).toHaveBeenCalledTimes(2);

        wrapper.unmount();

        // 在途请求在卸载后才成功返回:不得更新状态/重建计时器
        resolveInFlight!(okResponse("tok-2", 1));
        await flushPromises();

        // 若计时器被重建,1 秒后会再次触发自动续码
        vi.advanceTimersByTime(30000);
        await flushPromises();
        expect(api.generateSipQrToken).toHaveBeenCalledTimes(2);
    });
});

describe("QrProvisionCard 模拟器下载入口", () => {
    const source = readFileSync(
        resolve(process.cwd(), "src/views/gb28181/sip/QrProvisionCard.vue"),
        "utf8"
    );

    it("keeps the simulator download hover surface theme-aware", () => {
        const hoverRule = source.match(/\.qr-download:hover\s*\{([^}]*)\}/)?.[1] || "";

        expect(hoverRule).toContain("var(--uvp-panel-bg");
        expect(hoverRule).not.toContain("var(--uvp-brand-soft, #e8f2ff) 70%, #ffffff");
    });

    it("opens the public download site in a new tab", () => {
        expect(source).toContain('href="https://download.uvplatform.cn/"');
        expect(source).toContain('target="_blank"');
        expect(source).toContain('rel="noopener noreferrer"');
    });

    it("shows the download entry even without qr permission", async () => {
        api.generateSipQrToken.mockResolvedValue(okResponse("tok-1", 60));

        const wrapper = mountCard({ canGenerate: false });
        await flushPromises();

        expect(api.generateSipQrToken).not.toHaveBeenCalled();
        expect(wrapper.find(".qr-download").exists()).toBe(true);
        expect(wrapper.find(".qr-stage").exists()).toBe(false);
        wrapper.unmount();
    });
});
