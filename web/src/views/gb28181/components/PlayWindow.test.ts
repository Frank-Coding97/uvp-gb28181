import { flushPromises, mount } from "@vue/test-utils";
import { reactive } from "vue";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import PlayWindow from "./PlayWindow.vue";
import playWindowSource from "./PlayWindow.vue?raw";

const userState = vi.hoisted(() => ({
  account: { permissions: [] as string[] }
}));

vi.mock("@/store/modules/user", () => ({ useUserStoreHook: () => userState }));

describe("PlayWindow playback embedding", () => {
  beforeEach(() => {
    userState.account = reactive({ permissions: ["*:*:*"] });
  });

  afterEach(() => {
    delete (globalThis as { EasyPlayerPro?: unknown }).EasyPlayerPro;
    vi.useRealTimers();
  });

  it("keeps the original EasyPlayer controls visible in playback mode", async () => {
    const options: Record<string, unknown>[] = [];
    class FakeEasyPlayer {
      constructor(_element: HTMLElement, value: Record<string, unknown>) {
        options.push(value);
      }
      on = vi.fn();
      play = vi.fn();
      destroy = vi.fn();
    }
    (globalThis as { EasyPlayerPro?: unknown }).EasyPlayerPro = FakeEasyPlayer;

    const wrapper = mount(PlayWindow, {
      props: { url: "ws://zlm/playback.live.flv", playback: true, hasAudio: false }
    });
    await flushPromises();

    expect(wrapper.classes()).toContain("playback");
    expect(options[0]).toMatchObject({
      isBand: true,
      btns: { fullscreen: true, screenshot: true, play: true, audio: true, record: false, stretch: true }
    });
    expect(playWindowSource).not.toContain(".play-window.playback :deep(.easyplayer-controls)");
    expect(playWindowSource).not.toContain(".play-window.playback :deep(.easyplayer-zoom-controls)");
  });

  it("hides EasyPlayer screenshot control when the user lacks snapshot permission", async () => {
    userState.account.permissions = ["gb28181:play:start"];
    const options: Record<string, any>[] = [];
    class FakeEasyPlayer {
      constructor(_element: HTMLElement, value: Record<string, any>) {
        options.push(value);
      }
      on = vi.fn();
      play = vi.fn();
      destroy = vi.fn();
    }
    (globalThis as { EasyPlayerPro?: unknown }).EasyPlayerPro = FakeEasyPlayer;

    mount(PlayWindow, { props: { url: "ws://zlm/live.flv" } });
    await flushPromises();

    expect(options[0]).toMatchObject({ btns: { screenshot: false } });
  });

  it("rebuilds the player when snapshot permission changes", async () => {
    const options: Record<string, any>[] = [];
    const instances: FakeEasyPlayer[] = [];
    class FakeEasyPlayer {
      constructor(_element: HTMLElement, value: Record<string, any>) {
        options.push(value);
        instances.push(this);
      }
      on = vi.fn();
      play = vi.fn();
      destroy = vi.fn();
    }
    (globalThis as { EasyPlayerPro?: unknown }).EasyPlayerPro = FakeEasyPlayer;

    const wrapper = mount(PlayWindow, { props: { url: "ws://zlm/live.flv", hasAudio: false } });
    await flushPromises();
    expect(options[0]).toMatchObject({ hasAudio: false, btns: { screenshot: true } });

    userState.account.permissions = ["gb28181:play:start"];
    await flushPromises();
    expect(instances[0].destroy).toHaveBeenCalledTimes(1);
    expect(options[1]).toMatchObject({ hasAudio: false, btns: { screenshot: false } });
    expect(instances[1].play).toHaveBeenCalledWith("ws://zlm/live.flv");

    userState.account.permissions = ["gb28181:play:start", "gb28181:play:snapshot"];
    await flushPromises();
    expect(instances[1].destroy).toHaveBeenCalledTimes(1);
    expect(options[2]).toMatchObject({ hasAudio: false, btns: { screenshot: true } });
    expect(instances[2].play).toHaveBeenCalledWith("ws://zlm/live.flv");

    wrapper.unmount();
  });

  it("enables the ZLM WebRTC adapter only when requested by the caller", async () => {
    const options: Record<string, unknown>[] = [];
    class FakeEasyPlayer {
      constructor(_element: HTMLElement, value: Record<string, unknown>) {
        options.push(value);
      }
      on = vi.fn();
      play = vi.fn();
      destroy = vi.fn();
    }
    (globalThis as { EasyPlayerPro?: unknown }).EasyPlayerPro = FakeEasyPlayer;

    const wrapper = mount(PlayWindow, {
      props: {
        url: "webrtc://zlm:18080/index/api/webrtc?app=rtp&stream=stream-1&type=play",
        zlmWebrtc: true
      }
    });
    await flushPromises();

    expect(options[0]).toMatchObject({ isRtcZLM: true });
    wrapper.unmount();
  });

  it("forwards EasyPlayer time and loading events", async () => {
    const listeners: Record<string, (value?: unknown) => void> = {};
    class FakeEasyPlayer {
      constructor(_element: HTMLElement, _value: Record<string, unknown>) {}
      on = vi.fn((event: string, callback: (value?: unknown) => void) => {
        listeners[event] = callback;
      });
      play = vi.fn();
      destroy = vi.fn();
    }
    (globalThis as { EasyPlayerPro?: unknown }).EasyPlayerPro = FakeEasyPlayer;

    const wrapper = mount(PlayWindow, { props: { url: "ws://zlm/playback.live.flv" } });
    await flushPromises();

    listeners.timeUpdate?.(1234);
    listeners.loading?.(true);

    expect(wrapper.emitted("timeupdate")).toEqual([[1234]]);
    expect(wrapper.emitted("loading")).toEqual([[true]]);
  });

  it("从播放器挂载开始显示等待首帧状态,直到真实解码出画面", async () => {
    vi.useFakeTimers();
    let videoInfo: { width: number; height: number } | null = null;
    class FakeEasyPlayer {
      on = vi.fn();
      play = vi.fn();
      destroy = vi.fn();
      getVideoInfo = vi.fn(() => videoInfo);
    }
    (globalThis as { EasyPlayerPro?: unknown }).EasyPlayerPro = FakeEasyPlayer;

    const wrapper = mount(PlayWindow, { props: { url: "ws://zlm/live.flv" } });
    await flushPromises();

    expect(wrapper.get('[data-testid="player-loading"]').text()).toContain("等待首帧");

    videoInfo = { width: 1920, height: 1080 };
    vi.advanceTimersByTime(1000);
    await wrapper.vm.$nextTick();

    expect(wrapper.find('[data-testid="player-loading"]').exists()).toBe(false);
    expect(wrapper.emitted("firstFrame")).toHaveLength(1);
    wrapper.unmount();
  });

  it("播放器报错后移除等待首帧状态并展示错误", async () => {
    const listeners: Record<string, (value?: unknown) => void> = {};
    class FakeEasyPlayer {
      on = vi.fn((event: string, callback: (value?: unknown) => void) => {
        listeners[event] = callback;
      });
      play = vi.fn();
      destroy = vi.fn();
    }
    (globalThis as { EasyPlayerPro?: unknown }).EasyPlayerPro = FakeEasyPlayer;

    const wrapper = mount(PlayWindow, { props: { url: "ws://zlm/live.flv" } });
    await flushPromises();
    expect(wrapper.find('[data-testid="player-loading"]').exists()).toBe(true);

    listeners.timeout?.();
    await wrapper.vm.$nextTick();

    expect(wrapper.find('[data-testid="player-loading"]').exists()).toBe(false);
    expect(wrapper.get(".err").text()).toContain("拉流超时");
    wrapper.unmount();
  });

  it("reports the first decoded frame once per playback URL", async () => {
    vi.useFakeTimers();
    class FakeEasyPlayer {
      on = vi.fn();
      play = vi.fn();
      destroy = vi.fn();
      getVideoInfo = vi.fn(() => ({ width: 1920, height: 1080 }));
    }
    (globalThis as { EasyPlayerPro?: unknown }).EasyPlayerPro = FakeEasyPlayer;

    const wrapper = mount(PlayWindow, { props: { url: "ws://zlm/live-1.flv" } });
    await flushPromises();
    vi.advanceTimersByTime(2000);
    expect(wrapper.emitted("firstFrame")).toHaveLength(1);
    expect(wrapper.emitted("firstFrame")?.[0]?.[0]).toMatchObject({ event: "first_frame" });

    await wrapper.setProps({ url: "ws://zlm/live-2.flv" });
    await flushPromises();
    expect(wrapper.emitted("firstFrame")).toHaveLength(2);
    wrapper.unmount();
    vi.useRealTimers();
  });

  it("reports only a safe player error code once per playback URL", async () => {
    const listeners: Record<string, (value?: unknown) => void> = {};
    class FakeEasyPlayer {
      on = vi.fn((event: string, callback: (value?: unknown) => void) => {
        listeners[event] = callback;
      });
      play = vi.fn();
      destroy = vi.fn();
    }
    (globalThis as { EasyPlayerPro?: unknown }).EasyPlayerPro = FakeEasyPlayer;

    const wrapper = mount(PlayWindow, { props: { url: "ws://zlm/live.flv?token=secret" } });
    await flushPromises();
    listeners.error?.("Authorization: Bearer secret");
    listeners.timeout?.();

    expect(wrapper.emitted("playerError")).toHaveLength(1);
    const payload = wrapper.emitted("playerError")?.[0]?.[0];
    expect(payload).toMatchObject({ event: "player_error", code: "player_error" });
    expect(JSON.stringify(payload)).not.toContain("secret");
    expect(JSON.stringify(payload)).not.toContain("token=");
    wrapper.unmount();
  });

  it("reuses the EasyPlayer instance when the stream protocol URL changes", async () => {
    const instances: FakeEasyPlayer[] = [];
    class FakeEasyPlayer {
      on = vi.fn();
      play = vi.fn();
      destroy = vi.fn();

      constructor(_element: HTMLElement, _value: Record<string, unknown>) {
        instances.push(this);
      }
    }
    (globalThis as { EasyPlayerPro?: unknown }).EasyPlayerPro = FakeEasyPlayer;

    const wrapper = mount(PlayWindow, { props: { url: "ws://zlm/live.flv" } });
    await flushPromises();
    await wrapper.setProps({ url: "http://zlm/live.flv" });
    await flushPromises();

    expect(instances).toHaveLength(1);
    expect(instances[0].play).toHaveBeenNthCalledWith(1, "ws://zlm/live.flv");
    expect(instances[0].play).toHaveBeenNthCalledWith(2, "http://zlm/live.flv");
    expect(instances[0].destroy).not.toHaveBeenCalled();

    wrapper.unmount();
    expect(instances[0].destroy).toHaveBeenCalledTimes(1);
  });

  /**
   * 跨 WebRTC 边界必须换实例。
   *
   * ⛔ 回归点:`isRtcZLM` / `isWebrtc` 都是 EasyPlayer 的**构造期**选项,复用实例
   *    改不动;库在 URL 变化时只重建内部 core,顶层默认配置里没有 `isWebrtc`
   *    字段,`_replay()` 清不掉上一次写入的值 ⇒ 从 WebRTC 切回 FLV 时 `<video>`
   *    拿不到 MediaSource(src 恒为空串),画面全黑且无法自愈。
   */
  it("跨 WebRTC 边界切换协议时销毁重建播放器实例", async () => {
    const options: Record<string, any>[] = [];
    const instances: any[] = [];
    class FakeEasyPlayer {
      constructor(_element: HTMLElement, value: Record<string, any>) {
        options.push(value);
        instances.push(this);
      }
      on = vi.fn();
      play = vi.fn();
      destroy = vi.fn();
    }
    (globalThis as { EasyPlayerPro?: unknown }).EasyPlayerPro = FakeEasyPlayer;

    const wrapper = mount(PlayWindow, {
      props: { url: "ws://zlm/live.flv", hasAudio: false, zlmWebrtc: false }
    });
    await flushPromises();
    expect(options[0]).toMatchObject({ isRtcZLM: false });

    // FLV → WebRTC
    await wrapper.setProps({
      url: "webrtc://zlm:18080/index/api/webrtc?app=rtp&stream=stream-1&type=play",
      zlmWebrtc: true
    });
    await flushPromises();
    expect(instances).toHaveLength(2);
    expect(instances[0].destroy).toHaveBeenCalledTimes(1);
    expect(options[1]).toMatchObject({ isRtcZLM: true });

    // WebRTC → FLV：必须再换一次，否则残留判定会让实例继续走 WebRTC 链路
    await wrapper.setProps({ url: "ws://zlm/live.flv", zlmWebrtc: false });
    await flushPromises();
    expect(instances).toHaveLength(3);
    expect(instances[1].destroy).toHaveBeenCalledTimes(1);
    expect(options[2]).toMatchObject({ isRtcZLM: false });
    expect(instances[2].play).toHaveBeenCalledWith("ws://zlm/live.flv");

    wrapper.unmount();
  });

  /**
   * 跨协议重建前必须等 `destroy()` **落定**，不能抢跑。
   *
   * ⛔ 回归点：`EasyPlayerPro.destroy()` 返回 Promise，库靠容器上的 `data-EasyProv`
   *    标记判定「容器已被占用」，该标记要到 destroy 的 then 链里才删（实测跨 1 个
   *    宏任务）。早先的写法是 `destroy(); void play(u)` —— 不等就 `new`，而 `play()`
   *    里只 `await nextTick()`（微任务），构造函数会当场抛 `EasyPlayerPro err container`，
   *    线上表现为「点播未完成 / 初始化失败: EasyPlayerPro err container」。
   */
  it("跨 WebRTC 边界重建时,必须先等销毁落定再构造新实例", async () => {
    const order: string[] = [];
    // 手动控制「销毁落定」的时机,避免依赖 setImmediate / setTimeout 的先后。
    let settleDestroy: (() => void) | null = null;
    class FakeEasyPlayer {
      constructor() {
        order.push("construct");
      }
      on = vi.fn();
      play = vi.fn();
      // 模拟真实库:销毁是异步的,容器要到 promise 落定后才算交还。
      destroy = () => {
        order.push("destroy()");
        return new Promise<void>(resolve => {
          settleDestroy = () => {
            order.push("destroyed");
            resolve();
          };
        });
      };
    }
    (globalThis as { EasyPlayerPro?: unknown }).EasyPlayerPro = FakeEasyPlayer;

    const wrapper = mount(PlayWindow, {
      props: { url: "ws://zlm/live.flv", hasAudio: false, zlmWebrtc: false }
    });
    await flushPromises();
    expect(order).toEqual(["construct"]);

    // FLV → WebRTC:进入跨边界重建
    await wrapper.setProps({
      url: "webrtc://zlm:18080/index/api/webrtc?app=rtp&stream=stream-1&type=play",
      zlmWebrtc: true
    });
    expect(order).toEqual(["construct", "destroy()"]);

    // 销毁未落定:排空微任务也不允许出现第二个 construct
    await Promise.resolve();
    await Promise.resolve();
    await Promise.resolve();
    expect(order).toEqual(["construct", "destroy()"]);

    // 销毁落定后才允许重建
    expect(settleDestroy).toBeTypeOf("function");
    settleDestroy!();
    await flushPromises();
    expect(order).toEqual(["construct", "destroy()", "destroyed", "construct"]);

    wrapper.unmount();
  });

  /**
   * FLV → HLS 同样必须换实例。
   *
   * ⛔ 回归点：库给 `.flv` / `.fmp4` / `.mpeg4` / `.h264` / `.mp4` / `.ts` 都写了
   *    demux 复位分支，**唯独 `.m3u8` 没有**；而 demuxType 的取值链是
   *    `… isFlv ? flv : isTs ? ts : (… : 后缀 .m3u8 ? hls : …)`，`isFlv` 排在
   *    `.m3u8` 后缀判定**之前** ⇒ 复用实例时实测得到
   *    `play protocol is hls, demuxType is flv` 的错配，画面出不来。
   */
  it("跨解复用族(FLV → HLS)切换协议时销毁重建播放器实例", async () => {
    const options: Record<string, any>[] = [];
    const instances: any[] = [];
    class FakeEasyPlayer {
      constructor(_element: HTMLElement, value: Record<string, any>) {
        options.push(value);
        instances.push(this);
      }
      on = vi.fn();
      play = vi.fn();
      destroy = vi.fn();
    }
    (globalThis as { EasyPlayerPro?: unknown }).EasyPlayerPro = FakeEasyPlayer;

    const wrapper = mount(PlayWindow, {
      props: { url: "ws://zlm/rtp/stream-1.live.flv", hasAudio: false, zlmWebrtc: false }
    });
    await flushPromises();
    expect(instances).toHaveLength(1);

    // FLV → HLS：必须换实例，否则 demuxType 会残留成 flv
    await wrapper.setProps({ url: "http://zlm:18080/rtp/stream-1/hls.m3u8" });
    await flushPromises();
    expect(instances).toHaveLength(2);
    expect(instances[0].destroy).toHaveBeenCalledTimes(1);
    expect(instances[1].play).toHaveBeenCalledWith("http://zlm:18080/rtp/stream-1/hls.m3u8");

    // HLS → FLV：反向同样换
    await wrapper.setProps({ url: "ws://zlm/rtp/stream-1.live.flv" });
    await flushPromises();
    expect(instances).toHaveLength(3);
    expect(instances[1].destroy).toHaveBeenCalledTimes(1);

    wrapper.unmount();
  });

  /** 同族切换（HTTP-HLS → HTTPS-HLS）不换实例，避免无谓重建。 */
  it("同解复用族(HTTP-HLS → HTTPS-HLS)切换协议时复用播放器实例", async () => {
    const instances: any[] = [];
    class FakeEasyPlayer {
      on = vi.fn();
      play = vi.fn();
      destroy = vi.fn();
      constructor(_element: HTMLElement, _value: Record<string, any>) {
        instances.push(this);
      }
    }
    (globalThis as { EasyPlayerPro?: unknown }).EasyPlayerPro = FakeEasyPlayer;

    const wrapper = mount(PlayWindow, {
      props: { url: "http://zlm:18080/rtp/stream-1/hls.m3u8?play_token=A" }
    });
    await flushPromises();

    await wrapper.setProps({ url: "https://zlm:18443/rtp/stream-1/hls.m3u8?play_token=B" });
    await flushPromises();

    expect(instances).toHaveLength(1);
    expect(instances[0].destroy).not.toHaveBeenCalled();
    expect(instances[0].play).toHaveBeenNthCalledWith(2, "https://zlm:18443/rtp/stream-1/hls.m3u8?play_token=B");

    wrapper.unmount();
  });

  /**
   * 通道音频开关 → 播放器音频链路/控件/出声状态。
   * ⚠️ EasyPlayer 的 `isMute` 语义与命名相反（库内 `void 0!==e.isMute&&(t.isNotMute=e.isMute)`），
   * 传 true 才是「不静音」，所以通道开音频时它必须是 true。
   */
  function mountWithAudio(hasAudio?: boolean) {
    const options: Record<string, any>[] = [];
    const instances: any[] = [];
    class FakeEasyPlayer {
      constructor(_element: HTMLElement, value: Record<string, any>) {
        options.push(value);
        instances.push(this);
      }
      on = vi.fn();
      play = vi.fn().mockResolvedValue(undefined);
      destroy = vi.fn();
      setMute = vi.fn();
    }
    (globalThis as { EasyPlayerPro?: unknown }).EasyPlayerPro = FakeEasyPlayer;
    const props: { url: string; hasAudio?: boolean } = { url: "ws://zlm/live.flv" };
    if (hasAudio !== undefined) props.hasAudio = hasAudio;
    return { options, instances, props };
  }

  it("开启音频的通道：建音频链路、播放后解锁静音并显示音频控件", async () => {
    const { options, instances, props } = mountWithAudio(true);

    const wrapper = mount(PlayWindow, { props });
    await flushPromises();

    expect(options[0]).toMatchObject({ hasAudio: true, isMute: true });
    expect(instances[0].setMute).toHaveBeenCalledWith(0);

    wrapper.unmount();
  });

  it("关闭音频的通道：不建音频链路、不出现音频控件、也不解锁静音", async () => {
    const { options, instances, props } = mountWithAudio(false);

    const wrapper = mount(PlayWindow, { props });
    await flushPromises();

    expect(options[0]).toMatchObject({ hasAudio: false, isMute: false });
    expect(instances[0].setMute).not.toHaveBeenCalled();

    wrapper.unmount();
  });

  it("未声明 hasAudio 时按无音频处理", async () => {
    const { options, instances, props } = mountWithAudio();

    const wrapper = mount(PlayWindow, { props });
    await flushPromises();

    expect(options[0]).toMatchObject({ hasAudio: false, isMute: false });
    expect(instances[0].setMute).not.toHaveBeenCalled();

    wrapper.unmount();
  });
});
