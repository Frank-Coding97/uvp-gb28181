import { afterEach, expect, it, vi } from "vitest";
import { observePlayerFirstFrame } from "./playerFirstFrame";

afterEach(() => vi.useRealTimers());

function nativePlayer() {
  const video = document.createElement("video");
  let callback: VideoFrameRequestCallback;
  video.requestVideoFrameCallback = vi.fn(fn => {
    callback = fn;
    return 7;
  });
  video.cancelVideoFrameCallback = vi.fn();
  const core = { _opt: { url: "ws://zlm/live.flv" }, video: { $videoElement: video }, playing: false };
  const host = { player: core, getVideoInfo: () => ({ width: 1920, height: 1080 }) };
  return { host, video, frame: () => callback!(performance.now(), { presentedFrames: 1 } as VideoFrameCallbackMetadata) };
}

it("waits for the browser frame callback instead of trusting video dimensions", () => {
  vi.useFakeTimers();
  const { host, video, frame } = nativePlayer();
  const ready = vi.fn();
  const stop = observePlayerFirstFrame(host, "ws://zlm/live.flv", ready);
  vi.advanceTimersByTime(500);
  expect(ready).not.toHaveBeenCalled();
  expect(video.requestVideoFrameCallback).toHaveBeenCalledTimes(1);
  frame();
  expect(ready).toHaveBeenCalledExactlyOnceWith("video-frame-callback");
  frame();
  expect(ready).toHaveBeenCalledTimes(1);
  stop();
});

it("cancels pending callbacks and ignores a late callback after disposal", () => {
  vi.useFakeTimers();
  const { host, video, frame } = nativePlayer();
  const ready = vi.fn();
  const stop = observePlayerFirstFrame(host, "ws://zlm/live.flv", ready);
  stop();
  frame();
  vi.advanceTimersByTime(1000);
  expect(ready).not.toHaveBeenCalled();
  expect(video.cancelVideoFrameCallback).toHaveBeenCalledWith(7);
  expect(vi.getTimerCount()).toBe(0);
});

it("ignores an old core while a new playback URL is being rebuilt", () => {
  vi.useFakeTimers();
  const { host, video } = nativePlayer();
  const ready = vi.fn();
  const stop = observePlayerFirstFrame(host, "ws://zlm/new.flv", ready);
  vi.advanceTimersByTime(300);
  expect(video.requestVideoFrameCallback).not.toHaveBeenCalled();
  const next = nativePlayer();
  next.host.player._opt.url = "ws://zlm/new.flv";
  host.player = next.host.player;
  vi.advanceTimersByTime(100);
  next.frame();
  expect(ready).toHaveBeenCalledExactlyOnceWith("video-frame-callback");
  stop();
});

it("does not accept a queued frame from a core replaced during automatic decoder fallback", () => {
  vi.useFakeTimers();
  const old = nativePlayer();
  const ready = vi.fn();
  const stop = observePlayerFirstFrame(old.host, "ws://zlm/live.flv", ready);
  const next = nativePlayer();
  old.host.player = next.host.player;
  old.frame();
  expect(ready).not.toHaveBeenCalled();
  vi.advanceTimersByTime(100);
  next.frame();
  expect(ready).toHaveBeenCalledTimes(1);
  stop();
});

it("waits for canvas rendering and a paint cycle instead of canvas dimensions alone", () => {
  vi.useFakeTimers();
  const canvas = document.createElement("canvas");
  const core = { _opt: { url: "ws://zlm/live.flv" }, video: { $videoElement: canvas }, playing: false };
  const ready = vi.fn();
  const stop = observePlayerFirstFrame({ player: core }, "ws://zlm/live.flv", ready);
  vi.advanceTimersByTime(300);
  expect(ready).not.toHaveBeenCalled();
  core.playing = true;
  vi.advanceTimersByTime(150);
  expect(ready).toHaveBeenCalledExactlyOnceWith("canvas-render");
  stop();
});

it("uses an explicitly identified size proxy only when renderer evidence is unavailable", () => {
  vi.useFakeTimers();
  let info = { width: 0, height: 0 };
  const ready = vi.fn();
  const stop = observePlayerFirstFrame({ getVideoInfo: () => info }, "ws://zlm/live.flv", ready);
  vi.advanceTimersByTime(100);
  expect(ready).not.toHaveBeenCalled();
  info = { width: 1920, height: 1080 };
  vi.advanceTimersByTime(100);
  expect(ready).toHaveBeenCalledExactlyOnceWith("video-size");
  expect(vi.getTimerCount()).toBe(0);
  stop();
});

it("keeps legacy video dimensions pending until there is playback progress", () => {
  vi.useFakeTimers();
  const video = document.createElement("video");
  Object.defineProperties(video, {
    requestVideoFrameCallback: { value: undefined },
    readyState: { value: 4 },
    paused: { value: false },
    currentTime: { value: 0, configurable: true }
  });
  const core = { _opt: { url: "ws://zlm/live.flv" }, video: { $videoElement: video } };
  const ready = vi.fn();
  const stop = observePlayerFirstFrame({ player: core }, "ws://zlm/live.flv", ready);
  vi.advanceTimersByTime(200);
  expect(ready).not.toHaveBeenCalled();
  Object.defineProperty(video, "currentTime", { value: 0.04 });
  vi.advanceTimersByTime(100);
  expect(ready).toHaveBeenCalledExactlyOnceWith("video-playing");
  stop();
});
