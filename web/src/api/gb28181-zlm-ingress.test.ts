import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { beforeEach, describe, expect, it, vi } from "vitest";

const request = vi.hoisted(() => vi.fn());
vi.mock("@/utils/http", () => ({ http: { request } }));
vi.mock("@/api/utils", () => ({ baseUrlApi: (path: string) => `/api/${path}` }));

import {
  closeZLMRTPServer,
  createZLMFFmpegSource,
  createZLMPullProxy,
  deleteZLMPullProxy,
  listZLMFFmpegSources,
  listZLMPullProxies,
  listZLMRTPServers,
  preflightDeleteZLMPullProxy,
  type ZLMProxyDeleteRequest
} from "./gb28181-zlm-ingress";

const media = { schema: "rtsp", vhost: "__defaultVhost__", app: "live", stream: "camera/1" };

describe("ZLM ingress API", () => {
  beforeEach(() => {
    request.mockReset();
    request.mockResolvedValue({ code: 0, message: "", data: {} });
  });

  it("lists typed ingress resources through UVP and forwards AbortSignal", async () => {
    const controller = new AbortController();
    await listZLMPullProxies(9, { page: 2, pageSize: 50 }, controller.signal);
    await listZLMFFmpegSources(9, { page: 1, pageSize: 20 }, controller.signal);
    await listZLMRTPServers(9, { page: 3, pageSize: 10 }, controller.signal);

    expect(request).toHaveBeenNthCalledWith(1, "get", "/api/gb28181/zlm/nodes/9/proxies/pull", {
      params: { page: 2, pageSize: 50 }, signal: controller.signal
    });
    expect(request).toHaveBeenNthCalledWith(2, "get", "/api/gb28181/zlm/nodes/9/ffmpeg-sources", {
      params: { page: 1, pageSize: 20 }, signal: controller.signal
    });
    expect(request).toHaveBeenNthCalledWith(3, "get", "/api/gb28181/zlm/nodes/9/rtp-servers", {
      params: { page: 3, pageSize: 10 }, signal: controller.signal
    });
  });

  it("keeps source URLs in request bodies and never reconstructs a ZLM endpoint", async () => {
    await createZLMPullProxy(9, { media, sourceUrl: "rtsp://user:pass@example.test/live?token=hidden", retryCount: 2 });
    await createZLMFFmpegSource(9, {
      templateKey: "copy",
      srcUrl: "rtsp://example.test/source",
      dstUrl: "rtmp://example.test/live/camera",
      timeoutMs: 3000,
      enableHls: true,
      enableMp4: false
    });

    expect(request).toHaveBeenNthCalledWith(1, "post", "/api/gb28181/zlm/nodes/9/proxies/pull", {
      data: { nodeId: 9, media, sourceUrl: "rtsp://user:pass@example.test/live?token=hidden", retryCount: 2 }
    });
    expect(request).toHaveBeenNthCalledWith(2, "post", "/api/gb28181/zlm/nodes/9/ffmpeg-sources", {
      data: {
        templateKey: "copy",
        srcUrl: "rtsp://example.test/source",
        dstUrl: "rtmp://example.test/live/camera",
        timeoutMs: 3000,
        enableHls: true,
        enableMp4: false
      }
    });
  });

  it("binds proxy and RTP deletion to explicit preflight data", async () => {
    const deletion: ZLMProxyDeleteRequest = { nodeId: 9, key: "opaque/key", media, fingerprint: "fp" };
    await preflightDeleteZLMPullProxy(9, "opaque/key", deletion);
    await deleteZLMPullProxy(9, "opaque/key", deletion);
    await closeZLMRTPServer(9, { nodeId: 9, vhost: media.vhost, app: media.app, stream: media.stream });

    const encoded = "opaque%2Fkey";
    expect(request).toHaveBeenNthCalledWith(1, "post", `/api/gb28181/zlm/nodes/9/proxies/pull/${encoded}/preflight`, { data: deletion });
    expect(request).toHaveBeenNthCalledWith(2, "delete", `/api/gb28181/zlm/nodes/9/proxies/pull/${encoded}`, { data: deletion });
    expect(request).toHaveBeenNthCalledWith(3, "post", "/api/gb28181/zlm/nodes/9/rtp-servers/close", {
      data: { nodeId: 9, vhost: media.vhost, app: media.app, stream: media.stream }
    });
  });

  it("contains no arbitrary or direct ZLM API surface", () => {
    const source = readFileSync(resolve(process.cwd(), "src/api/gb28181-zlm-ingress.ts"), "utf8");
    expect(source).not.toMatch(/\/index\/api\//i);
    expect(source).not.toMatch(/apiSecret|[?&]secret=/i);
    expect(source).not.toMatch(/apiName|endpointName/i);
  });
});
