import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

import { buildNodeRequest, clearNodeSecret, createNodeFormState, validateNodeForm } from "./nodeFormState";

const existingNode = {
  id: 7,
  revision: 3,
  name: "zlm-a",
  host: "10.0.0.7",
  receiveHost: "10.0.0.8",
  playbackHost: "media.example.com",
  apiPort: 18080,
  mediaServerUUID: "uuid-a",
  weight: 50,
  state: "active" as const,
  recoveryRequired: false,
  rtpPortStart: 30000,
  rtpPortEnd: 35000,
  stats: {
    lastHeartbeatAt: "2026-08-30T00:00:00Z",
    mediaSourceCount: 0,
    sessionCount: 0,
    netThreadLoadAvg: 0,
    workThreadLoadAvg: 0,
    memoryUsageBytes: 0,
    totalBytesIn: 0,
    totalBytesOut: 0
  },
  autoOnDemandReady: true,
  createdAt: "2026-08-30T00:00:00Z",
  updatedAt: "2026-08-30T00:00:00Z"
};

describe("ZLM node form state", () => {
  it("loads editable connection candidates without ever loading the existing secret", () => {
    const form = createNodeFormState(existingNode);
    expect(form).toMatchObject({ host: "10.0.0.7", apiPort: "18080", apiSecret: "" });
  });

  it("validates numeric fields manually instead of relying on input clamping", () => {
    const form = createNodeFormState();
    Object.assign(form, {
      name: "zlm-a",
      host: "10.0.0.7",
      apiSecret: "secret",
      apiPort: "70000",
      weight: "101",
      rtpPortStart: "35000",
      rtpPortEnd: "30000"
    });
    expect(validateNodeForm(form, false)).toMatchObject({
      apiPort: expect.any(String),
      weight: expect.any(String),
      rtpPortEnd: expect.any(String)
    });
  });

  it("submits edited host and API port while omitting an unchanged secret", () => {
    const form = createNodeFormState(existingNode);
    form.host = "10.0.0.9";
    form.apiPort = "28080";
    expect(buildNodeRequest(form, true)).toMatchObject({ host: "10.0.0.9", apiPort: 28080 });
    expect(buildNodeRequest(form, true)).not.toHaveProperty("apiSecret");
  });

  it("clears the write-only secret after a successful save", () => {
    const form = createNodeFormState();
    form.apiSecret = "do-not-retain";
    expect(clearNodeSecret(form).apiSecret).toBe("");
    expect(form.apiSecret).toBe("do-not-retain");
  });

  it("keeps connection fields editable and uses clearable project controls", () => {
    const source = readFileSync(resolve(process.cwd(), "src/views/gb28181/zlm/NodeForm.vue"), "utf8");
    expect(source).not.toMatch(/v-model="form\.(host|apiPort)"[^>]*:disabled="editing\(\)"/);
    expect(source).toContain("allow-clear");
    expect(source).toContain("validateNodeForm");
  });

  it("uses the system dialog for both create and edit flows", () => {
    const source = readFileSync(resolve(process.cwd(), "src/views/gb28181/zlm/NodeForm.vue"), "utf8");
    expect(source).toContain("<a-modal");
    expect(source).toContain('modal-class="uvp-system-dialog zlm-node-form"');
    expect(source).not.toContain("<a-drawer");
    expect(source).not.toContain("arco-drawer-title");
  });

  it("guides creation through required connection fields and a real ZLM preview", () => {
    const source = readFileSync(resolve(process.cwd(), "src/views/gb28181/zlm/NodeForm.vue"), "utf8");
    expect(source).toContain("<a-steps");
    expect(source).toContain("必填参数");
    expect(source).toContain("ZL 信息确认");
    expect(source).toContain("probeZLMNode");
    expect(source).toContain("连接并读取");
    expect(source).toContain("确认添加");
  });
});
