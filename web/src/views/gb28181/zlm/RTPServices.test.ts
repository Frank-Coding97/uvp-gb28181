import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";
import {
  buildRTPCreateRequest,
  rtpCloseDecision
} from "./rtpServicesState";

const base = {
  vhost: "__defaultVhost__",
  app: "rtp",
  stream: "receiver-1",
  port: "",
  tcpMode: "0",
  ssrc: "",
  onlyTrack: "0",
  localIp: "",
  reuse: false
};

describe("RTP service state", () => {
  it("uses blank port as auto-allocation and never clamps invalid numeric text", () => {
    const automatic = buildRTPCreateRequest(base);
    expect(automatic.errors).toEqual({});
    expect(automatic.request).toMatchObject({ port: 0, tcpMode: 0, onlyTrack: 0 });

    const invalid = buildRTPCreateRequest({ ...base, port: "65536", tcpMode: "1.5", ssrc: "12x", reuse: true });
    expect(invalid.request).toBeUndefined();
    expect(invalid.errors).toMatchObject({ port: expect.any(String), tcpMode: expect.any(String), ssrc: expect.any(String) });

    const reuseWithoutPort = buildRTPCreateRequest({ ...base, reuse: true });
    expect(reuseWithoutPort.errors).toHaveProperty("reuse");

    expect(buildRTPCreateRequest({ ...base, localIp: "::::" }).errors).toHaveProperty("localIp");
    expect(buildRTPCreateRequest({ ...base, localIp: "2001:db8::1" }).errors).not.toHaveProperty("localIp");
  });

  it("allows ordinary close only for managed resources on a supported node", () => {
    expect(rtpCloseDecision({ managed: true, released: false }, "supported", true, false)).toMatchObject({ allowed: true, mode: "normal" });
    expect(rtpCloseDecision({ managed: false, released: false }, "supported", true, false)).toMatchObject({ allowed: false, mode: "blocked" });
    expect(rtpCloseDecision({ managed: false, released: false }, "supported", true, true)).toMatchObject({ allowed: true, mode: "force" });
    expect(rtpCloseDecision({ managed: true, released: false }, "unknown", true, true)).toMatchObject({ allowed: false });
  });

  it("shows actual port and ownership while closing through preflight APIs", () => {
    const source = readFileSync(resolve(process.cwd(), "src/views/gb28181/zlm/RTPServices.vue"), "utf8");
    const form = readFileSync(resolve(process.cwd(), "src/views/gb28181/zlm/RTPServerForm.vue"), "utf8");
    expect(source).toContain("record.port");
    expect(source).toContain("record.managed");
    expect(source).toContain("preflightCloseZLMRTPServer");
    expect(source).toContain("forceCloseZLMRTPServer");
    expect(source).toContain("useZLMRuntimePolling");
    expect(form).not.toContain("a-input-number");
    expect(form).toContain("allow-clear");
    expect(source).not.toContain("index/api");
  });
});
