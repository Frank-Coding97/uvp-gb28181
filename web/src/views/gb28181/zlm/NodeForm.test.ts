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
  sdpIp: "192.0.2.21",
  playbackHost: "media.example.com",
  hookIp: "192.0.2.20",
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
  it("does not guess an API port for a new node", () => {
    expect(createNodeFormState().apiPort).toBe("");
    expect(createNodeFormState(existingNode).apiPort).toBe("18080");
  });

  it("loads editable connection candidates without ever loading the existing secret", () => {
    const form = createNodeFormState(existingNode);
    expect(form).toMatchObject({ host: "10.0.0.7", hookIp: "192.0.2.20", apiPort: "18080", apiSecret: "" });
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

  it("creates a node from only host, API port and secret without sending a name", () => {
    const form = createNodeFormState();
    Object.assign(form, { host: "10.0.0.7", apiPort: "18080", apiSecret: "secret" });
    expect(validateNodeForm(form, false)).toEqual({});
    expect(buildNodeRequest(form, false)).toMatchObject({
      host: "10.0.0.7",
      apiPort: 18080,
      apiSecret: "secret"
    });
    expect(buildNodeRequest(form, false)).not.toHaveProperty("name");
  });

  it("submits edited host and API port while omitting an unchanged secret", () => {
    const form = createNodeFormState(existingNode);
    form.host = "10.0.0.9";
    form.apiPort = "28080";
    expect(buildNodeRequest(form, true)).toMatchObject({ host: "10.0.0.9", apiPort: 28080 });
    expect(buildNodeRequest(form, true)).not.toHaveProperty("apiSecret");
  });

  it("validates and submits optional node media address overrides", () => {
    const form = createNodeFormState(existingNode);
    form.hookIp = "2001:db8::20";
    form.playbackHost = "stream.example.com";
    expect(validateNodeForm(form, true)).toEqual({});
    expect(buildNodeRequest(form, true)).toMatchObject({ hookIp: "2001:db8::20", playbackHost: "stream.example.com" });

    form.hookIp = "224.0.0.1";
    form.playbackHost = "https://stream.example.com/live";
    expect(validateNodeForm(form, true)).toMatchObject({ hookIp: expect.any(String), playbackHost: expect.any(String) });
  });

  it("clears the write-only secret after a successful save", () => {
    const form = createNodeFormState();
    form.apiSecret = "do-not-retain";
    expect(clearNodeSecret(form).apiSecret).toBe("");
    expect(form.apiSecret).toBe("do-not-retain");
  });

  it("finishes loading and closes the modal before refreshing the node list", () => {
    const source = readFileSync(resolve(process.cwd(), "src/views/gb28181/zlm/NodeForm.vue"), "utf8");
    expect(source).toMatch(
      /finally \{[\s\S]*loading\.value = false;[\s\S]*if \(saved\) \{[\s\S]*close\(\);[\s\S]*emit\("saved"\)/
    );
    expect(source).not.toMatch(/emit\("saved"\);\s*emit\("update:visible", false\)/);
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

  it("allows the API Secret to be revealed with the password eye button", () => {
    const source = readFileSync(resolve(process.cwd(), "src/views/gb28181/zlm/NodeForm.vue"), "utf8");
    expect(source).toContain(':invisible-button="true"');
    expect(source).not.toContain(':invisible-button="false"');
  });

  it("guides creation through required connection fields and a real ZLM preview", () => {
    const source = readFileSync(resolve(process.cwd(), "src/views/gb28181/zlm/NodeForm.vue"), "utf8");
    expect(source).toContain("<a-steps");
    expect(source).toContain("必填参数");
    expect(source).toContain("ZL 信息确认");
    expect(source).toContain("probeZLMNode");
    expect(source).toContain("连接并读取");
    expect(source).toContain("确认添加");
    expect(source).toMatch(/<a-form-item\s+v-if="editing"\s+label="节点名"/);
    expect(source).toContain("editing ? '管理地址（API Host）' : 'IP 地址'");
    expect(source).not.toContain('<a-descriptions-item label="节点名称">');
  });

  it("shows the detected ZLM connection details as a two-column confirmation", () => {
    const source = readFileSync(resolve(process.cwd(), "src/views/gb28181/zlm/NodeForm.vue"), "utf8");
    for (const label of [
      "IP 地址",
      "API 端口",
      "API Secret",
      "mediaServerId",
      "HTTP PORT",
      "HTTPS PORT",
      "RTSP PORT",
      "RTSPS PORT",
      "RTMP PORT",
      "RTMPS PORT",
      "RTP Proxy PORT",
      "ONVIF PORT",
      "RTP 端口范围",
      "协议状态"
    ])
      expect(source).toContain(`label="${label}"`);
    expect(source).toContain("已填写（不回显）");
    expect(source).toContain('class="probe-details" :column="2"');
  });
});

describe("receive port mode", () => {
  it("never guesses a fixed listener port from a node registration", () => {
    expect(createNodeFormState().rtpProxyPort).toBe("");
    expect(createNodeFormState({ ...existingNode, rtpProxyPort: 10000 }).rtpProxyPort).toBe("");
  });
  it("keeps legacy nodes on multi-port and round trips the single-port selection", () => {
    expect(createNodeFormState(existingNode).rtpReceiveMode).toBe("multi");
    const form = createNodeFormState({ ...existingNode, rtpReceiveMode: "single", rtpProxyPort: 10000 });
    expect(form.rtpProxyPort).toBe("");
    form.rtpProxyPort = "40000";
    expect(buildNodeRequest(form, true)).toMatchObject({ rtpReceiveMode: "single", rtpProxyPort: 40000 });
  });
  it("validates only the active receive port fields and reserves the RTCP successor", () => {
    const form = createNodeFormState(existingNode);
    form.rtpReceiveMode = "single";
    form.rtpProxyPort = "65535";
    expect(validateNodeForm(form, true)).toHaveProperty("rtpProxyPort");
    form.rtpProxyPort = "10000";
    form.rtpPortStart = "";
    form.rtpPortEnd = "";
    expect(validateNodeForm(form, true)).toEqual({});
    expect(buildNodeRequest(form, true)).not.toHaveProperty("rtpPortStart");
    form.rtpReceiveMode = "multi";
    expect(validateNodeForm(form, true)).toHaveProperty("rtpPortStart");
  });
});
