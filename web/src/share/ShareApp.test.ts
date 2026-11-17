import { flushPromises, mount, type VueWrapper } from "@vue/test-utils";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import ShareApp from "./ShareApp.vue";

const decoder = vi.hoisted(() => vi.fn());
vi.mock("@/views/gb28181/components/playerDecoderResources", () => ({ preparePlayerDecoder: decoder }));
vi.mock("qrcode", () => ({ default: { toDataURL: vi.fn().mockResolvedValue("data:image/png;base64,qr") } }));

function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>(done => {
    resolve = done;
  });
  return { promise, resolve };
}

const now = Date.parse("2026-11-17T12:00:00Z");
let expiry: string;
let fetchMock: ReturnType<typeof vi.fn>;
let playerConstructor: ReturnType<typeof vi.fn>;
let playerPlay: ReturnType<typeof vi.fn>;
let playerDestroy: ReturnType<typeof vi.fn>;
let wrappers: VueWrapper[];
let nextToken: number;
let resolveOnline: boolean;
let statusFailure: number | undefined;
let hasAudio: boolean;
let playerEvents: Record<string, (...args: unknown[]) => void>[];

function response(data: unknown, status = 200) {
  return new Response(JSON.stringify({ code: 0, data }), { status, headers: { "Content-Type": "application/json" } });
}
function shareResponse(online = resolveOnline) {
  return response({
    name: "门岗摄像头",
    online,
    hasAudio,
    expiresAt: expiry,
    serverTime: new Date(Date.now()).toISOString()
  });
}
function viewerResponse(token = `viewer-${++nextToken}`) {
  return response({ viewerToken: token, expiresAt: expiry, httpFlvPath: `/api/public/video-share/viewers/${token}/live.flv` });
}
function mountShare(fragment = "#id=public-id&sig=secret-signature") {
  history.replaceState(null, "", `/share.html${fragment}`);
  const wrapper = mount(ShareApp);
  wrappers.push(wrapper);
  return wrapper;
}
function requests(path: string, method = "POST") {
  return fetchMock.mock.calls.filter(([url, options]) => url === path && options.method === method);
}
function deleteURLs() {
  return fetchMock.mock.calls.filter(([, options]) => options.method === "DELETE").map(([url]) => url);
}
function retryButton(wrapper: VueWrapper) {
  return wrapper.findAll("button").find(button => button.text().includes("重试"))!;
}

beforeEach(() => {
  vi.useFakeTimers({ toFake: ["Date", "setTimeout", "clearTimeout", "setInterval", "clearInterval"] });
  vi.setSystemTime(now);
  expiry = new Date(now + 60_000).toISOString();
  nextToken = 0;
  resolveOnline = true;
  statusFailure = undefined;
  hasAudio = true;
  playerEvents = [];
  wrappers = [];
  decoder.mockReset().mockResolvedValue("");
  playerPlay = vi.fn().mockResolvedValue(undefined);
  playerDestroy = vi.fn().mockResolvedValue(undefined);
  playerConstructor = vi.fn(function () {
    const events: Record<string, (...args: unknown[]) => void> = {};
    playerEvents.push(events);
    return {
      play: playerPlay,
      destroy: playerDestroy,
      on: vi.fn((name, handler) => {
        events[name] = handler;
      })
    };
  });
  vi.stubGlobal("EasyPlayerPro", playerConstructor);
  fetchMock = vi.fn(async (url: string, options: RequestInit) => {
    if (options.method === "DELETE") return new Response(null, { status: 204 });
    if (url.endsWith("/resolve")) return shareResponse();
    if (url.endsWith("/viewers/status"))
      return statusFailure
        ? response(null, statusFailure)
        : response({ expiresAt: expiry, serverTime: new Date(Date.now()).toISOString() });
    if (url.endsWith("/viewers")) return viewerResponse();
    throw new Error("unexpected anonymous endpoint");
  });
  vi.stubGlobal("fetch", fetchMock);
});

afterEach(async () => {
  for (const wrapper of wrappers) wrapper.unmount();
  await flushPromises();
  vi.clearAllTimers();
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

describe("ShareApp anonymous component integration", () => {
  it("erases credentials before the first exchange and never sends backend cookies", async () => {
    const pending = deferred<Response>();
    fetchMock.mockImplementationOnce(() => {
      expect(location.hash).toBe("");
      return pending.promise;
    });
    const wrapper = mountShare();
    expect(location.hash).toBe("");
    pending.resolve(shareResponse());
    await flushPromises();
    expect(playerPlay).toHaveBeenCalledWith(expect.stringContaining("/viewer-1/live.flv"));
    expect(wrapper.find("#share-link").exists()).toBe(true);
    expect(wrapper.get("#share-link").element).toHaveProperty(
      "value",
      expect.stringContaining("#id=public-id&sig=secret-signature")
    );
    await vi.advanceTimersByTimeAsync(5000);
    wrapper.unmount();
    await flushPromises();
    expect(deleteURLs()).toEqual(["/api/public/video-share/viewers/viewer-1"]);
    for (const [url, options] of fetchMock.mock.calls) {
      expect(url).toMatch(/^\/api\/public\/video-share\//);
      expect(url).not.toContain("secret-signature");
      expect(options).toEqual(
        expect.objectContaining({ credentials: "omit", cache: "no-store", redirect: "error", referrerPolicy: "no-referrer" })
      );
      const headers = new Headers(options.headers);
      expect(headers.has("Authorization")).toBe(false);
      expect(headers.has("Cookie")).toBe(false);
    }
  });

  it("renders embed playback without duplicate links, iframe code or QR materials", async () => {
    const wrapper = mountShare("#id=public-id&sig=secret-signature&embed=1");
    await flushPromises();
    expect(wrapper.classes()).toContain("share-page--embed");
    expect(playerPlay).toHaveBeenCalledTimes(1);
    expect(wrapper.find("header").exists()).toBe(false);
    expect(wrapper.find("footer").exists()).toBe(false);
    expect(wrapper.find(".share-tools").exists()).toBe(false);
    expect(wrapper.find(".share-actions").exists()).toBe(false);
    expect(wrapper.find("input, textarea, img").exists()).toBe(false);
  });

  it("counts down to expiry, destroys playback and never offers renewal or resume", async () => {
    expiry = new Date(now + 3000).toISOString();
    const wrapper = mountShare();
    await flushPromises();
    expect(wrapper.text()).toContain("3 秒");
    await vi.advanceTimersByTimeAsync(1000);
    expect(wrapper.text()).toContain("2 秒");
    await vi.advanceTimersByTimeAsync(2000);
    await flushPromises();
    expect(wrapper.text()).toContain("分享已失效");
    expect(wrapper.text()).not.toContain("已暂停");
    expect(wrapper.text()).not.toContain("继续播放");
    expect(wrapper.find("button").exists()).toBe(false);
    expect(playerDestroy).toHaveBeenCalledTimes(1);
    expect(deleteURLs()).toEqual(["/api/public/video-share/viewers/viewer-1"]);
    await vi.advanceTimersByTimeAsync(30_000);
    expect(requests("/api/public/video-share/viewers")).toHaveLength(1);
    expect(requests("/api/public/video-share/viewers/status")).toHaveLength(0);
  });

  it("treats a status 410 as terminal and releases the current viewer", async () => {
    const wrapper = mountShare();
    await flushPromises();
    statusFailure = 410;
    await vi.advanceTimersByTimeAsync(5000);
    await flushPromises();
    expect(wrapper.text()).toContain("分享已失效");
    expect(wrapper.text()).not.toContain("已暂停");
    expect(wrapper.find("button").exists()).toBe(false);
    expect(playerDestroy).toHaveBeenCalledTimes(1);
    expect(deleteURLs()).toEqual(["/api/public/video-share/viewers/viewer-1"]);
    await vi.advanceTimersByTimeAsync(30_000);
    expect(requests("/api/public/video-share/viewers")).toHaveLength(1);
    expect(requests("/api/public/video-share/viewers/status")).toHaveLength(1);
  });

  it("stops playback when status reports server-side expiry before the local clock", async () => {
    const wrapper = mountShare();
    await flushPromises();
    expiry = new Date(now + 1000).toISOString();
    await vi.advanceTimersByTimeAsync(5000);
    await flushPromises();
    expect(wrapper.text()).toContain("分享已失效");
    expect(wrapper.find("button").exists()).toBe(false);
    expect(playerDestroy).toHaveBeenCalledTimes(1);
    expect(deleteURLs()).toEqual(["/api/public/video-share/viewers/viewer-1"]);
  });

  it("retries a failed initial anonymous exchange without renewing the expiry", async () => {
    fetchMock.mockRejectedValueOnce(new TypeError("offline"));
    const wrapper = mountShare();
    await flushPromises();
    expect(wrapper.text()).toContain("网络暂时不可用");
    await retryButton(wrapper).trigger("click");
    await flushPromises();
    expect(requests("/api/public/video-share/resolve")).toHaveLength(2);
    expect(requests("/api/public/video-share/viewers")).toHaveLength(1);
    expect(playerPlay).toHaveBeenCalledTimes(1);
    expect(wrapper.text()).toContain("1 分 0 秒");
  });

  it("re-resolves an offline channel on retry and starts playback once it is online", async () => {
    resolveOnline = false;
    const wrapper = mountShare();
    await flushPromises();
    expect(wrapper.text()).toContain("通道当前离线");
    expect(playerPlay).not.toHaveBeenCalled();
    resolveOnline = true;
    await retryButton(wrapper).trigger("click");
    await flushPromises();
    expect(requests("/api/public/video-share/resolve")).toHaveLength(2);
    expect(playerPlay).toHaveBeenCalledTimes(1);
  });

  it("retries a status network failure by releasing only the old viewer without renewing expiry", async () => {
    const wrapper = mountShare();
    await flushPromises();
    statusFailure = 503;
    await vi.advanceTimersByTimeAsync(5000);
    await flushPromises();
    expect(wrapper.text()).toContain("暂时无法验证分享");
    statusFailure = undefined;
    await retryButton(wrapper).trigger("click");
    await flushPromises();
    expect(deleteURLs()).toEqual(["/api/public/video-share/viewers/viewer-1"]);
    expect(playerDestroy).toHaveBeenCalledTimes(1);
    expect(requests("/api/public/video-share/resolve")).toHaveLength(1);
    expect(requests("/api/public/video-share/viewers")).toHaveLength(2);
    expect(playerPlay).toHaveBeenCalledTimes(2);
    expect(wrapper.text()).toContain("0 分 55 秒");
  });

  it("uses a stable server clock offset when the viewer clock is skewed", async () => {
    expiry = new Date(now + 3_603_000).toISOString();
    fetchMock.mockImplementationOnce(async () =>
      response({
        name: "门岗摄像头",
        online: true,
        hasAudio: false,
        expiresAt: expiry,
        serverTime: new Date(now + 3_600_000).toISOString()
      })
    );
    const wrapper = mountShare();
    await flushPromises();
    expect(wrapper.text()).toContain("0 分 3 秒");
    await vi.advanceTimersByTimeAsync(3000);
    await flushPromises();
    expect(wrapper.text()).toContain("分享已失效");
    expect(playerDestroy).toHaveBeenCalledTimes(1);
  });

  it("ignores an old status response after a retry creates another viewer", async () => {
    const pending = deferred<Response>();
    const wrapper = mountShare();
    await flushPromises();
    fetchMock.mockImplementationOnce(() => pending.promise);
    await vi.advanceTimersByTimeAsync(5000);
    // Pause and resume replace the viewer while its previous status request is pending.
    const pauseButton = wrapper.findAll("button").find(button => button.text().includes("暂停"))!;
    await pauseButton.trigger("click");
    await flushPromises();
    const resume = wrapper.findAll("button").find(button => button.text().includes("继续播放"))!;
    await resume.trigger("click");
    await flushPromises();
    expect(playerPlay).toHaveBeenCalledTimes(2);
    pending.resolve(response(null, 410));
    await flushPromises();
    expect(wrapper.text()).not.toContain("分享已失效");
    expect(wrapper.text()).toContain("暂停");
    expect(deleteURLs()).toEqual(["/api/public/video-share/viewers/viewer-1"]);
  });

  it("releases only the viewer belonging to the page that exits", async () => {
    const first = mountShare();
    await flushPromises();
    const second = mountShare("#id=other-id&sig=other-signature");
    await flushPromises();
    expect(playerPlay).toHaveBeenCalledTimes(2);
    first.unmount();
    await flushPromises();
    expect(deleteURLs()).toEqual(["/api/public/video-share/viewers/viewer-1"]);
    expect(second.text()).toContain("暂停");
    expect(fetchMock.mock.calls.some(([url]) => /stop|stop-all|renew|channel-shares/.test(url))).toBe(false);
    second.unmount();
    await flushPromises();
    expect(deleteURLs()).toEqual(["/api/public/video-share/viewers/viewer-1", "/api/public/video-share/viewers/viewer-2"]);
  });

  it("releases a viewer returned after the page exits without creating a player", async () => {
    const pending = deferred<Response>();
    fetchMock.mockImplementationOnce(async () => shareResponse()).mockImplementationOnce(() => pending.promise);
    const wrapper = mountShare();
    await flushPromises();
    wrapper.unmount();
    pending.resolve(viewerResponse("late-viewer"));
    await flushPromises();
    expect(playerConstructor).not.toHaveBeenCalled();
    expect(deleteURLs()).toEqual(["/api/public/video-share/viewers/late-viewer"]);
  });

  it("does not recreate playback after decoder preparation completes on an exited page", async () => {
    const pending = deferred<string>();
    decoder.mockReturnValueOnce(pending.promise);
    const wrapper = mountShare();
    await flushPromises();
    expect(decoder).toHaveBeenCalledTimes(1);
    wrapper.unmount();
    await flushPromises();
    pending.resolve("/decoder.js");
    await flushPromises();
    expect(playerConstructor).not.toHaveBeenCalled();
    expect(deleteURLs()).toEqual(["/api/public/video-share/viewers/viewer-1"]);
    await vi.advanceTimersByTimeAsync(10_000);
    expect(requests("/api/public/video-share/viewers/status")).toHaveLength(0);
  });

  it("offers manual playback after autoplay is blocked without creating or renewing a viewer", async () => {
    playerPlay.mockRejectedValueOnce(new DOMException("gesture required", "NotAllowedError"));
    const wrapper = mountShare();
    await flushPromises();
    expect(wrapper.text()).toContain("点击播放");
    expect(wrapper.text()).not.toContain("网络暂时不可用");
    const video = document.createElement("video");
    const nativePlay = vi.fn().mockResolvedValue(undefined);
    video.play = nativePlay;
    wrapper.get(".share-player__screen").element.append(video);
    const button = wrapper.findAll("button").find(button => button.text().includes("点击播放"))!;
    const clicked = button.trigger("click");
    // Native play must happen in the click handler, before an API request or nextTick consumes the gesture.
    expect(nativePlay).toHaveBeenCalledTimes(1);
    await clicked;
    await flushPromises();
    expect(wrapper.text()).not.toContain("点击播放");
    expect(requests("/api/public/video-share/viewers")).toHaveLength(1);
    expect(deleteURLs()).toEqual([]);
    expect(wrapper.text()).toContain("1 分 0 秒");
  });

  it("offers manual playback for the bundled player's terminal video rendering failure", async () => {
    const wrapper = mountShare();
    await flushPromises();
    playerEvents[0].playFailedAndPaused("videoElementPlayingFailed");
    await flushPromises();
    expect(wrapper.text()).toContain("点击播放");
    await vi.advanceTimersByTimeAsync(5000);
    expect(requests("/api/public/video-share/viewers/status")).toHaveLength(1);
  });

  it("allows the bundled decoder to fall back before reporting terminal codec incompatibility", async () => {
    const wrapper = mountShare();
    await flushPromises();
    playerEvents[0].error("webcodecsH265NotSupport");
    await flushPromises();
    expect(wrapper.text()).not.toContain("编码不支持");
    expect(wrapper.text()).not.toContain("视频暂时无法播放");
    playerEvents[0].playFailedAndPaused("webcodecsH265NotSupport");
    await flushPromises();
    expect(wrapper.text()).toContain("编码不支持");
    expect(wrapper.text()).not.toContain("检查网络");
    expect(wrapper.text()).not.toContain("正在连接");
  });

  it("distinguishes a standard unsupported format rejection from an ordinary playback failure", async () => {
    playerPlay.mockRejectedValueOnce(new DOMException("unsupported codec", "NotSupportedError"));
    const wrapper = mountShare();
    await flushPromises();
    expect(wrapper.text()).toContain("编码不支持");
    await retryButton(wrapper).trigger("click");
    await flushPromises();
    playerEvents[1].error("fetchError");
    await flushPromises();
    expect(wrapper.text()).toContain("视频暂时无法播放");
    expect(wrapper.text()).not.toContain("编码不支持");
  });

  it.each([true, false])("starts muted and exposes audio controls only when hasAudio is %s", async audio => {
    hasAudio = audio;
    mountShare();
    await flushPromises();
    expect(playerConstructor).toHaveBeenCalledWith(
      expect.any(HTMLDivElement),
      expect.objectContaining({
        hasAudio: audio,
        // The bundled player's isMute option maps to isNotMute, so false means muted.
        isMute: false,
        btns: expect.objectContaining({ audio })
      })
    );
  });

  it("ignores old player callbacks after retry and after page exit", async () => {
    const wrapper = mountShare();
    await flushPromises();
    const oldEvents = playerEvents[0];
    oldEvents.error("fetchError");
    await flushPromises();
    await retryButton(wrapper).trigger("click");
    await flushPromises();
    oldEvents.error("fetchError");
    oldEvents.timeout();
    oldEvents.playFailedAndPaused("webcodecsH265NotSupport");
    await flushPromises();
    expect(wrapper.text()).not.toContain("视频暂时无法播放");
    expect(wrapper.text()).not.toContain("编码不支持");
    expect(wrapper.text()).not.toContain("超时");
    wrapper.unmount();
    await flushPromises();
    playerEvents[1].playFailedAndPaused("videoElementPlayingFailed");
    playerEvents[1].error("fetchError");
    await flushPromises();
    expect(requests("/api/public/video-share/viewers")).toHaveLength(2);
    expect(deleteURLs()).toEqual(["/api/public/video-share/viewers/viewer-1", "/api/public/video-share/viewers/viewer-2"]);
  });

  it("does not replace a terminal codec state when the original play promise settles late", async () => {
    const pending = deferred<void>();
    playerPlay.mockReturnValueOnce(pending.promise);
    const wrapper = mountShare();
    await flushPromises();
    playerEvents[0].playFailedAndPaused("webcodecsUnsupportedConfigurationError");
    await flushPromises();
    expect(wrapper.text()).toContain("编码不支持");
    pending.resolve();
    await flushPromises();
    expect(wrapper.text()).toContain("编码不支持");
    expect(wrapper.find(".share-tools").exists()).toBe(false);
  });

  it("ignores player callbacks after the page has exited", async () => {
    const wrapper = mountShare();
    await flushPromises();
    wrapper.unmount();
    await flushPromises();
    playerEvents[0].error("fetchError");
    playerEvents[0].playFailedAndPaused("videoElementPlayingFailed");
    playerEvents[0].play();
    playerEvents[0].timeout();
    await vi.advanceTimersByTimeAsync(10_000);
    expect(playerConstructor).toHaveBeenCalledTimes(1);
    expect(deleteURLs()).toEqual(["/api/public/video-share/viewers/viewer-1"]);
    expect(requests("/api/public/video-share/viewers/status")).toHaveLength(0);
  });
});
