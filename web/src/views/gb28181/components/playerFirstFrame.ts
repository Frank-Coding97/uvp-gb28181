export type FirstFrameEvidence = "video-frame-callback" | "video-playing" | "canvas-render" | "video-size";

/**
 * EasyPlayer can rebuild its inner core during play() and decoder fallback.
 * Native frame callbacks prove presentation; canvas/legacy signals remain identified proxies.
 * The library's playToRenderTimes is not sufficient: MSE/HLS can emit it before rendering.
 */
export function observePlayerFirstFrame(host: any, url: string, ready: (evidence: FirstFrameEvidence) => void): () => void {
  let stopped = false;
  let timer: ReturnType<typeof setInterval> | undefined;
  let video: HTMLVideoElement | null = null;
  let videoCore: any = null;
  let videoCallback: number | undefined;
  let paintCallback: number | undefined;
  const currentCore = () => host.player ?? host;
  const matches = (core: any) => !core?._opt || core._opt.url === url;

  function cancelVideo() {
    if (videoCallback !== undefined) video?.cancelVideoFrameCallback?.(videoCallback);
    videoCallback = undefined;
    video = null;
    videoCore = null;
  }

  function stop() {
    stopped = true;
    if (timer !== undefined) clearInterval(timer);
    if (paintCallback !== undefined) cancelAnimationFrame(paintCallback);
    cancelVideo();
  }

  function finish(evidence: FirstFrameEvidence, core: any) {
    if (stopped || currentCore() !== core || !matches(core)) return;
    stop();
    ready(evidence);
  }

  function inspect() {
    if (stopped) return;
    const core = currentCore();
    if (!matches(core)) return;
    const element = core?.video?.$videoElement;
    if (video && (videoCore !== core || video !== element)) cancelVideo();
    if (element instanceof HTMLVideoElement) {
      if (typeof element.requestVideoFrameCallback === "function") {
        if (!video) {
          video = element;
          videoCore = core;
          videoCallback = element.requestVideoFrameCallback(() => finish("video-frame-callback", core));
        }
      } else if (!element.paused && element.readyState >= 2 && element.currentTime > 0) {
        // Older browsers expose playback progress, not a compositor frame callback.
        finish("video-playing", core);
      }
      return;
    }
    if (element instanceof HTMLCanvasElement) {
      if (core.playing === true && element.width > 0 && element.height > 0 && paintCallback === undefined) {
        // handleRender() follows the canvas write; allow a browser paint before dropping the overlay.
        paintCallback = requestAnimationFrame(() => {
          paintCallback = requestAnimationFrame(() => {
            paintCallback = undefined;
            finish("canvas-render", core);
          });
        });
      }
      return;
    }
    // No known renderer: preserve compatibility but never label dimensions as presentation evidence.
    if (core !== host) return;
    let info: { width?: number; height?: number } | undefined;
    try {
      info = host.getVideoInfo?.();
    } catch {
      return;
    }
    if (Number(info?.width) > 0 && Number(info?.height) > 0) finish("video-size", core);
  }

  timer = setInterval(inspect, 100);
  inspect();
  return stop;
}
