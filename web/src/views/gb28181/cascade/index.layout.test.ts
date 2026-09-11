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
    expect(sharing).toContain('class="share-channel-table share-table"');
    expect(sharing).toContain('@update:selected-keys="handleChannelSelectionChange"');
    expect(sharing).toContain('placeholder="通道名称 / 国标编号"');
    // 设备模式已移除:弹窗只允许直接勾选具体通道。
    expect(sharing).not.toContain("share-mode-switch");
    expect(sharing).not.toContain('<a-radio value="device">按设备</a-radio>');
    expect(sharing).not.toContain("share-device-table");
    expect(sharing).not.toContain("设备模式下会将该设备的全部通道加入共享");
    expect(sharing).not.toContain("<a-drawer");
  });

  it("keeps device selection as a channel projection and supports server pagination", () => {
    expect(source).toContain("selectedDeviceIds = computed");
    expect(source).toContain("channelSourceDeviceIds");
    expect(source).toContain("projectionDevices.get(channel.deviceProjectionId)");
    expect(source).toContain("channelKeyword");
    expect(source).toContain("channelPage");
    expect(source).toContain("listDevicePage");
    expect(source).toContain("listChannelPage");
  });

  it("exposes register/deregister as an inline enable switch instead of a dropdown option", () => {
    expect(source).toContain('<a-table-column title="启用"');
    expect(source).toMatch(/<a-switch[\s\S]*?:model-value="record\.enabled"[\s\S]*?@change="\(value: boolean \| string \| number\) => toggleEnabled\(record, Boolean\(value\)\)"/);
    expect(source).toContain("async function toggleEnabled(platform: CascadePlatform, next: boolean)");
    expect(source).not.toContain("<a-doption");
    expect(source).not.toContain("toggleEnabled(record)");
    expect(source).toMatch(/class="uvp-table-action uvp-table-action--delete"/);
  });

  it("keeps the long form scrollable inside the dialog viewport", () => {
    expect(source).toContain(".cascade-platform-dialog .arco-modal-body");
    expect(source).toContain("max-height: calc(100dvh - 260px);");
    expect(source).toContain("overflow-y: auto;");
  });

  it("renders the shared-channel summary as an obvious link and the viewer as a system dialog with removal", () => {
    expect(source).toMatch(/<a-link class="shared-channels-link"[^>]*>\s*<ListVideo :size="14" \/>/s);
    expect(source).toMatch(/<a-modal\s+v-model:visible="sharedDevicesVisible"[\s\S]*?modal-class="uvp-system-dialog shared-devices-dialog"/);
    expect(source).not.toMatch(/<a-modal\s+v-model:visible="sharedDevicesVisible"[\s\S]*?:footer="false"/);
    expect(source).toContain('title="操作"');
    expect(source).toContain("function removeSharedChannel(");
    expect(source).toContain("expectedProjectionRevision: snapshot?.revision ?? 0");
    expect(source).toContain(".shared-devices-dialog .arco-modal-body");
  });

  it("follows the form-input template rules in the platform editor dialog", () => {
    const editorStart = source.indexOf("<a-modal");
    const editor = source.slice(editorStart, source.search(/<a-modal\s+v-model:visible="sharedDevicesVisible"/));
    // 硬规则 2:数值输入不使用 a-input-number 的静默 clamp。
    expect(editor).not.toContain("<a-input-number");
    expect(editor.match(/<s-number-field/g)?.length).toBeGreaterThanOrEqual(6);
    // 硬规则 3:20 位国标编码带位数指示。
    expect(editor.match(/<s-counter-suffix :value="(form\.upstreamServerId|form\.localDeviceId)\.length" :total="20" \/>/g)?.length).toBe(2);
    // 硬规则 5:密码字段带强度指示。
    expect(editor).toContain("<s-password-field");
    expect(editor).not.toContain("<a-input-password");
    // 硬规则 4:关键字段 blur 后显示逐字段错误。
    expect(editor.match(/@blur="touched\.\w+ = true"/g)?.length).toBeGreaterThanOrEqual(6);
    expect(source).toContain("cascadeFormFieldErrors(form, touched)");
    // 硬规则 1:文本输入开 allow-clear(选择器/密码组件除外)。
    const inputsMissingClear = (editor.match(/<a-input\b[^>]*\/>/g) ?? []).filter(tag => !tag.includes("allow-clear"));
    expect(inputsMissingClear).toEqual([]);
  });
});
