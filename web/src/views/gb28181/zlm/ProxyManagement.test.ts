import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";
import {
  buildProxyCreateRequest,
  ingressCapabilityFromError,
  proxyAddressText,
  proxyCapabilityPresentation,
  proxyDeleteDecision
} from "./proxyManagementState";

const base = {
  schema: "rtsp",
  vhost: "__defaultVhost__",
  app: "live",
  stream: "camera-1",
  url: "rtsp://user:secret@example.test/live?token=hidden",
  retryCount: "",
  rtpType: "",
  timeoutSec: ""
};

describe("proxy management state", () => {
  it("keeps clear optional numbers omitted and rejects malformed strings without clamping", () => {
    const valid = buildProxyCreateRequest("pull", base);
    expect(valid.errors).toEqual({});
    expect(valid.request).toEqual({
      media: { schema: "rtsp", vhost: "__defaultVhost__", app: "live", stream: "camera-1" },
      sourceUrl: base.url
    });

    const invalid = buildProxyCreateRequest("pull", {
      ...base,
      retryCount: "11",
      rtpType: "1.5",
      timeoutSec: "0.05"
    });
    expect(invalid.request).toBeUndefined();
    expect(invalid.errors).toMatchObject({ retryCount: expect.any(String), rtpType: expect.any(String), timeoutSec: expect.any(String) });
  });

  it("only enables mutations for supported capabilities", () => {
    expect(proxyCapabilityPresentation("supported").actionable).toBe(true);
    expect(proxyCapabilityPresentation("unsupported")).toMatchObject({ actionable: false, label: "节点不支持" });
    expect(proxyCapabilityPresentation("unknown")).toMatchObject({ actionable: false, label: "能力未探测" });
  });

  it("keeps explicit unsupported responses distinct from failed probes", () => {
    expect(ingressCapabilityFromError({ response: { status: 422 } })).toBe("unsupported");
    expect(ingressCapabilityFromError({ response: { status: 503 } })).toBe("unknown");
    expect(ingressCapabilityFromError(new Error("network"))).toBe("unknown");
  });

  it("uses only the backend redacted display and fails closed on ownership", () => {
    const summary = {
      scheme: "rtsp", host: "example.test", fingerprint: "sha256", hasUserInfo: true,
      hasSensitiveQuery: true, display: "rtsp://example.test"
    };
    expect(proxyAddressText(summary)).toBe("rtsp://example.test");
    expect(proxyDeleteDecision({ status: "managed", present: true, presenceKnown: true })).toMatchObject({ allowed: true });
    expect(proxyDeleteDecision({ status: "owned", present: true, presenceKnown: true })).toMatchObject({ allowed: false });
    expect(proxyDeleteDecision({ status: "unknown", present: true, presenceKnown: false })).toMatchObject({ allowed: false });
  });

  it("uses one page with pull/push tabs, typed polling, preflight and result refresh", () => {
    const source = readFileSync(resolve(process.cwd(), "src/views/gb28181/zlm/ProxyManagement.vue"), "utf8");
    expect(source).toContain('value="pull"');
    expect(source).toContain('value="push"');
    expect(source).toContain("useZLMRuntimePolling");
    expect(source).toContain("preflightDeleteZLMPullProxy");
    expect(source).toContain("preflightDeleteZLMPushProxy");
    expect(source).toContain("refresh");
    expect(source).not.toContain("index/api");
    expect(source).not.toContain("http.request");
  });
});
