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
  // ⛔ baseUrl 必须由后端下发（2026-10-08 新契约）。
  // 桩照真接口建模：漏掉它 ⇒ 组件判定"接入地址不可用"⇒ 不出码，
  // 正好证明前端真的在用后端给的值，而不是自己推断。
  return {
    code: 0,
    message: "",
    data: { token, expiresInSeconds, baseUrl: "http://192.168.1.10:51010" }
  };
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
    api.generateSipQrToken.mockResolvedValueOnce(okResponse("tok-1", 1)).mockResolvedValueOnce(okResponse("tok-2", 60));

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
    api.generateSipQrToken.mockResolvedValueOnce(okResponse("tok-1", 1)).mockRejectedValue(new Error("network down"));

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
    api.generateSipQrToken.mockResolvedValueOnce(okResponse("tok-1", 1)).mockRejectedValue(new Error("network down"));

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
    api.generateSipQrToken.mockResolvedValueOnce(okResponse("tok-1", 1)).mockImplementationOnce(
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
    api.generateSipQrToken.mockResolvedValueOnce(okResponse("tok-1", 1)).mockImplementationOnce(
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

describe("QrProvisionCard 接入基址来自后端（2026-10-08）", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    api.generateSipQrToken.mockReset();
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  // ⛔⛔ 回归防线：曾经的默认值是 window.location.origin。
  // 用户从 nginx 自签 HTTPS 打开平台 ⇒ 二维码里带自签地址 ⇒ 手机侧证书校验失败
  // ⇒ 失败被报成「连不上平台,请检查手机与平台是否同网络」, 用户去查 Wi-Fi 永远查不出。
  it("后端没给基址时不出码，也不静默回退到浏览器地址", async () => {
    api.generateSipQrToken.mockResolvedValue({
      code: 0,
      message: "",
      data: { token: "tok-x", expiresInSeconds: 300 } // ⛔ 没有 baseUrl
    });

    const wrapper = mountCard();
    await flushPromises();

    expect(wrapper.find("[data-qrcode]").exists()).toBe(false);
    // 且必须给用户看得见的提示，而不是空白或"转圈"
    expect(wrapper.text()).toContain("平台未下发");
    wrapper.unmount();
  });

  it("基址非法（缺 scheme）时同样不出码", async () => {
    api.generateSipQrToken.mockResolvedValue({
      code: 0,
      message: "",
      data: { token: "tok-y", expiresInSeconds: 300, baseUrl: "192.168.1.10:51010" }
    });

    const wrapper = mountCard();
    await flushPromises();

    expect(wrapper.find("[data-qrcode]").exists()).toBe(false);
    wrapper.unmount();
  });

  it("基址合法时二维码里的地址就是后端给的那个", async () => {
    api.generateSipQrToken.mockResolvedValue(okResponse("tok-z", 300));

    const wrapper = mountCard();
    await flushPromises();

    // 二维码组件被 stub 成空标签，改由链接区断言实际 URL
    expect(wrapper.text()).toContain("http://192.168.1.10:51010/gb28181/qr#t=tok-z");
    wrapper.unmount();
  });

  it("源码里不再用 window.location.origin 当默认值（防止被加回来）", () => {
    const source = readFileSync(resolve(process.cwd(), "src/views/gb28181/sip/QrProvisionCard.vue"), "utf8");
    // ⛔ 只扫**代码**：注释里必须留着这段事故说明（那是给后来人看的），
    //   全文扫会把说明本身判成违规。判据 = 该串出现在赋值/传参位置。
    const code = source.replace(/\/\/[^\n]*/g, "").replace(/\/\*[\s\S]*?\*\//g, "");
    expect(code).not.toContain("window.location.origin");
  });

  it("界面不再提供「平台访问地址」输入框（只展示二维码）", () => {
    const source = readFileSync(resolve(process.cwd(), "src/views/gb28181/sip/QrProvisionCard.vue"), "utf8");
    // 地址改由后端下发，界面上不该再有让用户填地址的地方
    expect(source).not.toContain('v-model="baseUrl"');
    expect(source).not.toContain("平台访问地址");
  });
});

describe("QrProvisionCard 模拟器下载入口", () => {
  const source = readFileSync(resolve(process.cwd(), "src/views/gb28181/sip/QrProvisionCard.vue"), "utf8");

  it("keeps the simulator download hover surface theme-aware", () => {
    const hoverRule = source.match(/\.qr-download:hover\s*\{([^}]*)\}/)?.[1] || "";

    expect(hoverRule).toContain("var(--uvp-panel-bg");
    expect(hoverRule).not.toContain("var(--uvp-brand-soft, #e8f2ff) 70%, #ffffff");
  });

  it("uses a solid brand blue for the download icon", () => {
    const iconRule = source.match(/\.qr-download__icon\s*\{([^}]*)\}/)?.[1] || "";

    expect(iconRule).toContain("background: #2563eb;");
    expect(iconRule).not.toContain("background: var(--uvp-brand");
  });

  it("opens the public download site in a new tab", () => {
    expect(source).toContain('href="https://download.uvplatform.com/"');
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
