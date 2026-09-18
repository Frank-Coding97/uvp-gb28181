import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

const readSource = (path: string) => readFileSync(resolve(process.cwd(), path), "utf8");

// 有「页面级刷新动作」的页面:每个都必须在刷新按钮上带 uvp-page-action-btn(共用尺寸)。
// ⛔ `sip/PlatformInfo.vue` **不在**这张表里 —— 它在 2026-09-17 按定制要求去掉了页头的
//    「刷新状态」按钮,页头只剩「复制」与「编辑」两个动作(见 PlatformInfo.layout.test.ts)。
//    没有刷新动作的页面套不进这条约束;别再把它加回来,否则守卫会要求一个不该存在的按钮。
const pageActionSources = [
  "src/views/gb28181/cascade/index.vue",
  "src/views/gb28181/cloud-recordings/RecordingWorkspacePanel.vue",
  "src/views/gb28181/device-mgmt/index.vue",
  "src/views/gb28181/recording-schedules/RecordingPlansPanel.vue",
  "src/views/gb28181/security/preview.vue",
  "src/views/gb28181/sip-log-v2/index.vue",
  "src/views/gb28181/zlm/components/NodeConfigPanel.vue",
  "src/views/gb28181/zlm/components/ZLMNodeContextBar.vue",
  "src/views/gb28181/zlm/workbench/ingress/FFmpegPanel.vue",
  "src/views/gb28181/zlm/workbench/ingress/ProxyPanel.vue",
  "src/views/gb28181/zlm/workbench/ingress/RTPPanel.vue",
  "src/views/gb28181/zlm/workbench/monitoring/NetworkSessionPanel.vue",
  "src/views/gb28181/zlm/workbench/monitoring/StreamPanel.vue",
  "src/views/gb28181/zlm/workbench/nodes/NodeListPanel.vue"
];

const createActionSources = [
  "src/views/gb28181/cascade/index.vue",
  "src/views/gb28181/device-mgmt/index.vue",
  "src/views/gb28181/recording-schedules/RecordingPlansPanel.vue",
  "src/views/gb28181/security/preview.vue",
  "src/views/gb28181/zlm/workbench/ingress/FFmpegPanel.vue",
  "src/views/gb28181/zlm/workbench/ingress/ProxyPanel.vue",
  "src/views/gb28181/zlm/workbench/ingress/RTPPanel.vue",
  "src/views/gb28181/zlm/workbench/nodes/NodeListPanel.vue"
];

describe("page action button regression", () => {
  it("uses shared sizing for page-level refresh actions", () => {
    for (const path of pageActionSources) {
      expect(readSource(path), path).toMatch(/class="[^"]*uvp-page-action-btn[^"]*uvp-refresh-btn|class="[^"]*uvp-refresh-btn[^"]*uvp-page-action-btn/);
    }
  });

  it("uses the shared create semantic for page-level create actions", () => {
    for (const path of createActionSources) {
      expect(readSource(path), path).toMatch(/class="[^"]*uvp-page-action-btn[^"]*uvp-create-btn|class="[^"]*uvp-create-btn[^"]*uvp-page-action-btn/);
    }
  });

  it("keeps both SIP configuration toolbars on the shared page-action sizing", () => {
    const platformInfo = readSource("src/views/gb28181/sip/PlatformInfo.vue");
    const serviceConfig = readSource("src/views/gb28181/sip/ServiceConfig.vue");

    // PlatformInfo 是 2 个(复制 / 编辑)—— 2026-09-17 之前是 3 个,多的那个是已被移除的
    // 「刷新状态」。数字变了但**约束没变**:这页的页面级动作仍然全部走共用尺寸类。
    expect(platformInfo.match(/uvp-page-action-btn/g)).toHaveLength(2);
    expect(serviceConfig.match(/uvp-page-action-btn/g)).toHaveLength(2);
  });
});
