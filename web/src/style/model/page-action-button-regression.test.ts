import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

const readSource = (path: string) => readFileSync(resolve(process.cwd(), path), "utf8");

const pageActionSources = [
  "src/views/gb28181/cascade/index.vue",
  "src/views/gb28181/cloud-recordings/RecordingWorkspacePanel.vue",
  "src/views/gb28181/device-mgmt/index.vue",
  "src/views/gb28181/recording-schedules/RecordingPlansPanel.vue",
  "src/views/gb28181/security/preview.vue",
  "src/views/gb28181/sip-log-v2/index.vue",
  "src/views/gb28181/sip/PlatformInfo.vue",
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

    expect(platformInfo.match(/uvp-page-action-btn/g)).toHaveLength(3);
    expect(serviceConfig.match(/uvp-page-action-btn/g)).toHaveLength(2);
  });
});
