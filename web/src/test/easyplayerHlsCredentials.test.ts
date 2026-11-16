import { readFileSync } from "node:fs";
import { afterEach, expect, it, vi } from "vitest";

afterEach(() => {
  vi.unstubAllGlobals();
  document.body.innerHTML = "";
});

it("sends HLS playlist and segment requests with the media server playback cookie", async () => {
  const source = readFileSync("public/easyplayer/EasyPlayer-pro.js", "utf8");
  const Player = new Function("module", "exports", `${source};return window.EasyPlayerPro;`)(undefined, undefined);
  const fetchMock = vi.fn(
    async (url: string, _init?: RequestInit) =>
      new Response(
        url.endsWith(".m3u8")
          ? "#EXTM3U\n#EXT-X-VERSION:3\n#EXT-X-TARGETDURATION:2\n#EXT-X-MEDIA-SEQUENCE:0\n#EXTINF:2,\nsegment.ts\n#EXT-X-ENDLIST\n"
          : "",
        { status: url.endsWith(".m3u8") ? 200 : 401 }
      )
  );
  vi.stubGlobal("fetch", fetchMock);
  vi.stubGlobal(
    "Worker",
    class {
      onmessage: ((event: { data: unknown }) => void) | null = null;
      constructor() {
        queueMicrotask(() => this.onmessage?.({ data: { cmd: "init" } }));
      }
      postMessage() {}
      terminate() {}
    }
  );
  const container = document.createElement("div");
  document.body.append(container);
  const player = new Player(container, { hasAudio: false, isH265: true, MSE: false, WCS: false, WASM: false });
  try {
    void player.play("https://media.example.test:51007/rtp/test/hls.m3u8").catch(() => {});
    await vi.waitFor(() => expect(fetchMock.mock.calls.some(([url]) => url.endsWith("segment.ts"))).toBe(true));
    for (const [, init] of fetchMock.mock.calls) expect(init).toMatchObject({ credentials: "include" });
  } finally {
    await player.destroy();
  }
});
