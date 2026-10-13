import { flushPromises, mount } from "@vue/test-utils";
import { beforeEach, describe, expect, it, vi } from "vitest";

const issueRecordingAccess = vi.hoisted(() => vi.fn());
vi.mock("../api", async importOriginal => ({
  ...(await importOriginal<typeof import("../api")>()),
  issueRecordingAccess,
  contentURL: (id: string, capability: string) => `/content/${id}?cap=${capability}`
}));

import RecordingPlayerDialog from "./RecordingPlayerDialog.vue";
import source from "./RecordingPlayerDialog.vue?raw";
import { hasRuleBlock } from "@/test/source-assert";

const recording = {
  id: "9007199254740993",
  fileName: "record.mp4",
  channelName: "东门",
  startTime: "2026-08-10T12:00:00Z"
};

const stubs = {
  "a-modal": {
    props: ["visible", "modalClass"],
    template:
      "<section v-if='visible' data-testid='player-dialog' :data-modal-class='modalClass'><slot name='title' /><slot /></section>"
  },
  "a-spin": { template: "<div><slot /></div>" },
  "a-alert": { template: "<div><slot /></div>" },
  "a-button": {
    emits: ["click"],
    template: "<button :data-testid='$attrs[`data-testid`]' @click='$emit(`click`)'><slot /></button>"
  }
};

describe("RecordingPlayerDialog", () => {
  beforeEach(() => {
    issueRecordingAccess.mockReset();
    issueRecordingAccess
      .mockResolvedValueOnce({
        code: 0,
        message: "",
        data: { mode: "play", capability: "first", expiresAt: "2026-08-10T13:00:00Z" }
      })
      .mockResolvedValueOnce({
        code: 0,
        message: "",
        data: { mode: "play", capability: "renewed", expiresAt: "2026-08-10T13:00:00Z" }
      });
  });

  it("挂统一弹窗类，弹窗壳层由全局规则接管", async () => {
    // ⛔ 防回归（起因 2026-10-06 用户实测「弹窗风格跟系统不像一家」）：
    //   Arco 的 Modal 是 `inheritAttrs: false`，模板上的 `class` 经 `$attrs` 落在
    //   **外层 `.arco-modal-container`** 上，而壳层规则（底色/描边/圆角/header 渐变/body 内边距）
    //   全部挂在 `.uvp-system-dialog .arco-modal-*` ⇒ 必须写 `modal-class`，写 `class` 一条都不命中。
    //   漏了就退回Arco 自带灰面板 = 暗色下"两套皮肤"。
    expect(source).toContain('modal-class="uvp-system-dialog recording-player-dialog"');
    // 同时钉住"不要再写回 class=" —— 那正是这次踩的坑
    expect(source).not.toMatch(/<a-modal[^>]*\sclass="recording-player-dialog"/);

    const wrapper = mount(RecordingPlayerDialog, {
      props: { visible: true, recording },
      global: { stubs }
    });
    await flushPromises();
    expect(wrapper.get("[data-testid='player-dialog']").attributes("data-modal-class")).toContain("uvp-system-dialog");
  });

  it("视频舞台固定深灰底，不跟主题走", () => {
    // 播放画面（尤其暗场录像）压浅色面板会看不清。锁死"两主题共用的深灰"这个口径。
    expect(hasRuleBlock(source, ".recording-player-stage", "background: #090b0f")).toBe(true);
    // 占位文案在深色舞台上，必须固定浅色，不能用主题文字色 token 跟着主题跑
    //（注：本文件的 `//` 行注释不会被 uvp-tokens 门禁剥掉 —— 它只剥 /* */ 与 <!-- -->，
    //  所以注释里别写「var(--双横线uvp-xxx)」这种引用写法，会被当成真引用而全仓报警。踩过。）
    expect(hasRuleBlock(source, ".recording-player-placeholder", "color: #a8b0bd")).toBe(true);
  });

  it("截断的长文本给出 title 悬浮全文", async () => {
    const long = { ...recording, channelName: "超长通道名称".repeat(8), fileName: "x".repeat(120) + ".mp4" };
    const wrapper = mount(RecordingPlayerDialog, {
      props: { visible: true, recording: long },
      global: { stubs }
    });
    await flushPromises();
    const strong = wrapper.get(".recording-player-title strong");
    const small = wrapper.get(".recording-player-title small");
    expect(strong.attributes("title")).toBe(long.channelName);
    expect(small.attributes("title")).toBe(long.fileName);
  });

  it("uses native MP4 content and renews at most once while preserving position", async () => {
    const wrapper = mount(RecordingPlayerDialog, {
      props: { visible: true, recording },
      global: { stubs }
    });
    await flushPromises();
    const video = wrapper.get("video");
    expect(video.attributes("src")).toBe("/content/9007199254740993?cap=first");
    expect(video.attributes("controlslist")).toBe("nodownload");
    Object.defineProperty(video.element, "currentTime", { configurable: true, writable: true, value: 37.5 });
    await video.trigger("error");
    await flushPromises();
    expect(issueRecordingAccess).toHaveBeenCalledTimes(2);
    expect(video.attributes("src")).toBe("/content/9007199254740993?cap=renewed");
    await video.trigger("loadedmetadata");
    expect((video.element as HTMLVideoElement).currentTime).toBe(37.5);
    await video.trigger("error");
    await flushPromises();
    expect(issueRecordingAccess).toHaveBeenCalledTimes(2);
    expect(wrapper.text()).toContain("播放失败");
  });
});
