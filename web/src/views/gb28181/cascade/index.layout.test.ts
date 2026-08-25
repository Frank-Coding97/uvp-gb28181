import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

const source = readFileSync(resolve(process.cwd(), "src/views/gb28181/cascade/index.vue"), "utf8");

describe("cascade platform editor layout", () => {
  it("uses the shared page shell spacing without extra horizontal padding", () => {
    expect(source).toMatch(/\.cascade-shell\s*\{[^}]*display:\s*flex;[^}]*gap:\s*14px;[^}]*\}/s);
    expect(source).not.toMatch(/\.cascade-shell\s*\{[^}]*padding:/s);
  });

  it("uses compact metric cards and the shared fixed-width search panel", () => {
    expect(source).toContain('class="cascade-summary-card"');
    expect(source).not.toContain('class="cascade-summary__item"');
    expect(source).toContain('<s-layout-search class="cascade-search-panel">');
    expect(source).toContain("<a-input-search");
    expect(source).not.toContain('class="cascade-toolbar"');
    expect(source).toMatch(/\.cascade-search\s*\{[^}]*flex:\s*0 0 280px;[^}]*width:\s*280px;/s);
    expect(source).toMatch(/\.cascade-status-filter\s*\{[^}]*flex:\s*0 0 148px;[^}]*width:\s*148px;/s);
  });

  it("prefills a new platform with the current local SIP identity", () => {
    expect(source).toContain("fetchSipSetupStatus");
    expect(source).toContain("fetchSipNetworkInterfaces");
    expect(source).toContain("item.recommended");
    expect(source).toContain("cascadeLocalIdentityDefaults");
    expect(source).toContain("Object.assign(form, defaultCascadePlatform(), cascadeLocalIdentityDefaults(localSipConfig.value || undefined))");
  });

  it("does not repeat the route title and description above the workspace", () => {
    expect(source).not.toContain("<h2>国标级联</h2>");
    expect(source).not.toContain("管理 UVP 作为下级平台向上级平台的注册、资源共享和运行状态。");
    expect(source).not.toContain('<header class="cascade-header">');
    expect(source).toContain('<template #actions>');
    expect(source).toContain('class="cascade-search-actions"');
  });

  it("uses centered dialogs with explicit device/channel sharing modes", () => {
    const editorStart = source.indexOf("<a-modal");
    const shareStart = source.search(/<a-modal\s+v-model:visible="shareVisible"/);

    expect(editorStart).toBeGreaterThan(-1);
    expect(shareStart).toBeGreaterThan(editorStart);

    const editor = source.slice(editorStart, shareStart);
    expect(editor).toContain('v-model:visible="editorVisible"');
    expect(editor).toContain('modal-class="uvp-system-dialog cascade-platform-dialog"');
    expect(editor).toContain('width="min(820px, calc(100vw - 24px))"');
    expect(editor).toContain(':mask-closable="!saving"');
    expect(editor).toContain('<template #footer>');
    expect(editor).not.toContain("<a-drawer");

    const sharing = source.slice(shareStart);
    expect(sharing).toContain('modal-class="uvp-system-dialog cascade-share-dialog"');
    expect(sharing).toContain('width="min(1080px, calc(100vw - 24px))"');
    expect(sharing).toContain('class="share-mode-switch"');
    expect(sharing).toContain('<a-radio value="device">按设备</a-radio>');
    expect(sharing).toContain('<a-radio value="channel">按通道</a-radio>');
    expect(sharing).toContain('class="share-device-table share-table"');
    expect(sharing).toContain('class="share-channel-table share-table"');
    expect(sharing).toContain('@update:selected-keys="handleChannelSelectionChange"');
    expect(sharing).toContain('@update:selected-keys="handleDeviceSelectionChange"');
    expect(sharing).toContain('设备模式下会将该设备的全部通道加入共享');
    expect(sharing).not.toContain("<a-drawer");
  });

  it("keeps device selection as a channel projection and supports server pagination", () => {
    expect(source).toContain("selectedDeviceIds = computed");
    expect(source).toContain("channelSourceDeviceIds");
    expect(source).toContain("projectionDevices.get(channel.deviceProjectionId)");
    expect(source).toContain("channelKeyword");
    expect(source).toContain("shareMode");
    expect(source).toContain("devicePage");
    expect(source).toContain("channelPage");
    expect(source).toContain("listDevicePage");
    expect(source).toContain("listChannelPage");
    expect(source).toContain("loadAllDeviceChannels");
  });

  it("keeps the long form scrollable inside the dialog viewport", () => {
    expect(source).toContain(".cascade-platform-dialog .arco-modal-body");
    expect(source).toContain("max-height: calc(100dvh - 260px);");
    expect(source).toContain("overflow-y: auto;");
  });
});
