import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

import {
  buildNodeRequest,
  clearNodeSecret,
  createNodeFormState,
  inheritedPlaceholder,
  NODE_NAME_MAX_LENGTH,
  parseRTPPortRange,
  validateNodeForm
} from "./nodeFormState";

const existingNode = {
  id: 7,
  revision: 3,
  name: "zlm-a",
  host: "10.0.0.7",
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

  it("creates a node from only host, API port and secret without sending an empty alias", () => {
    const form = createNodeFormState();
    Object.assign(form, { host: "10.0.0.7", apiPort: "18080", apiSecret: "secret" });
    expect(validateNodeForm(form, false)).toEqual({});
    expect(buildNodeRequest(form, false)).toMatchObject({
      host: "10.0.0.7",
      apiPort: 18080,
      apiSecret: "secret"
    });
    // 留空 = 交给后端用 `host:port` 兜底，**不能**把空串写进库覆盖兜底值。
    expect(buildNodeRequest(form, false)).not.toHaveProperty("name");
  });

  it("sends the optional alias on create so new nodes are not stuck with host:port", () => {
    const form = createNodeFormState();
    Object.assign(form, { name: "  北京一楼弱电间  ", host: "10.0.0.7", apiPort: "18080", apiSecret: "secret" });
    expect(validateNodeForm(form, false)).toEqual({});
    expect(buildNodeRequest(form, false)).toMatchObject({ name: "北京一楼弱电间" });

    // 别名不参与唯一性：两台节点重名也照常提交（纯展示字段）。
    const duplicate = { ...form, name: "北京一楼弱电间", host: "10.0.0.8" };
    expect(buildNodeRequest(duplicate, false)).toMatchObject({ name: "北京一楼弱电间" });
  });

  it("caps the alias at the column width by characters, not bytes", () => {
    const form = createNodeFormState();
    Object.assign(form, { host: "10.0.0.7", apiPort: "18080", apiSecret: "secret" });

    // 64 个汉字 = 192 字节，仍应放行（DB 是 varchar(64) 按字符算）。
    form.name = "电".repeat(NODE_NAME_MAX_LENGTH);
    expect(validateNodeForm(form, false)).toEqual({});

    form.name = "电".repeat(NODE_NAME_MAX_LENGTH + 1);
    expect(validateNodeForm(form, false)).toMatchObject({ name: expect.stringContaining("最长") });
    // 超长时仍然提交原值，由后端同一套判据兜底，前端不做静默截断。
    expect(buildNodeRequest(form, false)).toMatchObject({ name: "电".repeat(NODE_NAME_MAX_LENGTH + 1) });
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
    // 别名在新建向导里也可填（可选）：不填由后端兜底成 host:port，
    // 因此这里**不能**要求它必填，也不能再退回「只在编辑态出现」。
    expect(source).toContain("editing ? '节点名' : '节点别名（可选）'");
    expect(source).toContain("留空则显示为「IP:端口」");
    expect(source).not.toContain('<a-descriptions-item label="节点名称">');
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
  it("only offers the mode switch when editing an existing node", () => {
    const source = readFileSync(resolve(process.cwd(), "src/views/gb28181/zlm/NodeForm.vue"), "utf8");
    // 新建时模式恒为多端口（后端 buildCreateCandidate 的默认），开关只留给编辑态：
    // 否则用户能在新建向导里选成单端口，却既看不到、也没法核对 ZLM 的单端口监听是否启用。
    expect(source).toMatch(/<a-form-item v-if="editing" label="收流端口模式">/);
    expect(source).not.toMatch(/<a-form-item label="收流端口模式">/);
  });
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

/**
 * 多端口收流范围的**唯一解析规则**，与后端 `node.ParseRTPPortRange` 逐条对齐。
 * ⛔ 两边判据各写一份就会「界面读得出来、后端解析不了」；所以这里的用例要和 Go 侧
 * `rtp_range_test.go` 的非法表保持一致。
 */
describe("RTP port range parsing", () => {
  it("accepts only a well formed start-end pair", () => {
    expect(parseRTPPortRange("51014-51063")).toEqual({ start: 51014, end: 51063 });
    expect(parseRTPPortRange("1024-65535")).toEqual({ start: 1024, end: 65535 });
    // 起止相等是合法的（单个会话的极端配置），后端也放行。
    expect(parseRTPPortRange("30000-30000")).toEqual({ start: 30000, end: 30000 });
  });

  it("rejects everything a caller must not treat as a port", () => {
    const unusable = [
      undefined,
      "",
      "51014",
      "51014-",
      "-51063",
      "51014-51063-51200",
      "51014--51063",
      " 51014-51063",
      "51014-51063 ",
      "rtp_proxy.port_range",
      "0-0",
      "51299-51200",
      "30000-70000",
      "1e4-2e4"
    ];
    for (const value of unusable) {
      expect(parseRTPPortRange(value as string | undefined), `${String(value)} 不应被当成可用范围`).toBeNull();
    }
  });
});

// 2026-10-20 现场反馈：引导步骤里配过的媒体地址，在节点编辑弹窗里「不回显」。
// 真因是这三项属于节点级覆盖，留空 = 跟随平台默认，所以空白是正常的；
// 但界面必须把「实际生效的是哪个地址」摆出来，否则看着像丢数据。
describe("inherited media address placeholder", () => {
  it("shows the effective address while the node-level value is empty", () => {
    expect(inheritedPlaceholder("", "192.168.10.220", "设备可访问的 IP 或域名")).toBe("留空则使用：192.168.10.220");
  });

  it("keeps the plain hint once the operator types an override", () => {
    expect(inheritedPlaceholder("198.51.100.21", "192.168.10.220", "设备可访问的 IP 或域名")).toBe("设备可访问的 IP 或域名");
  });

  it("falls back to the plain hint when there is no platform default either", () => {
    expect(inheritedPlaceholder("", "", "具体 IPv4 或 IPv6")).toBe("具体 IPv4 或 IPv6");
    expect(inheritedPlaceholder("", undefined, "具体 IPv4 或 IPv6")).toBe("具体 IPv4 或 IPv6");
    // 只有空白字符等同于没填。
    expect(inheritedPlaceholder("   ", "   ", "具体 IP 地址或域名")).toBe("具体 IP 地址或域名");
  });
});
