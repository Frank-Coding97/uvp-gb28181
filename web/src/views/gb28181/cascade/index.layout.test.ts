import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

const source = readFileSync(resolve(process.cwd(), "src/views/gb28181/cascade/index.vue"), "utf8");

describe("cascade platform editor layout", () => {
  it("shows sharing mode as a summary and directs changes to the resource workspace", () => {
    expect(source).not.toContain('v-model="form.publishGroup"');
    expect(source).toContain('form.publishGroup ? "按照目录共享" : "按照通道共享"');
    expect(source).toContain("共享方式请在“共享与目录”页面切换并保存");
  });
  it("edits and confirms catalog automation, recording authorization and RTCP without codec policy", () => {
    for (const [field, label] of [
      ["autoPushCatalog", "自动推送目录"],
      ["recordingEnabled", "允许录像查询与回放"],
      ["rtcpKeepalive", "RTCP 保活"]
    ]) {
      expect(source).toContain(`v-model="form.${field}"`);
      expect(source).toContain(`<dt>${label}</dt>`);
      expect(source).toContain(`${field}: platform.${field} ?? false`);
    }
    expect(source).not.toContain("视频编码策略");
  });
  it("keeps publication switch rows unfilled while retaining switch state colors", () => {
    const rowStyle = source.match(/\.switch-grid label\s*\{([^}]*)\}/)?.[1];
    expect(rowStyle).toBeDefined();
    expect(rowStyle).not.toMatch(/background(?:-color)?\s*:/);
    expect(rowStyle).toContain("min-height: 36px");
    expect(source).toContain('<a-switch v-model="form.publishPlatform" />');
  });

  it("uses a preset catalog batch selector and only displays legacy values as disabled choices", () => {
    const field = source.slice(
      source.indexOf('<a-form-item label="每批目录条数"'),
      source.indexOf('<a-form-item label="最大并发流"')
    );
    expect(field).toContain('<a-select v-model="form.catalogBatchSize">');
    expect(field).toContain('v-for="size in CASCADE_CATALOG_BATCH_SIZES"');
    expect(field).toContain(':value="size"');
    expect(field).toContain("editing.catalogBatchSize");
    expect(field).toContain("disabled");
    expect(field).not.toContain("allow-create");
    expect(field).not.toContain("s-number-field");
    expect(source).toContain("<dt>每批目录条数</dt>");
    expect(source).toContain("validateCascadeStep(form, editorStep.value, editing.value?.catalogBatchSize)");
    expect(source).toContain("validateCascadeStep(form, step, editing.value?.catalogBatchSize)");
  });

  it("uses the GB minimum positive registration expiry in the editor", () => {
    expect(source).toContain('<s-number-field ref="registerExpiresField" v-model="form.registerExpires" :min="3600"');
  });

  it("explains the unlimited stream value and displays it in confirmation", () => {
    expect(source).toContain('label="最大并发流" help="0 表示不限制并发流。"');
    expect(source).toContain('v-model="form.maxStreams" :min="0"');
    expect(source).toContain('form.maxStreams === 0 ? "不限制"');
  });

  it("offers version-based charset selection and preserves existing overrides when editing", () => {
    const charsetField = source.slice(
      source.indexOf('<a-form-item label="字符集'),
      source.indexOf('<a-form-item label="注册有效期')
    );
    expect(charsetField).toContain('<a-option value="">跟随协议版本（默认）</a-option>');
    expect(charsetField).toContain('<a-option value="GB2312">GB2312</a-option>');
    expect(charsetField).toContain('<a-option value="GB18030">GB18030</a-option>');
    expect(charsetField).toContain('<a-option value="UTF-8">UTF-8（兼容扩展）</a-option>');
    expect(source).toContain('charsetOverride: platform.charsetOverride || ""');
  });

  it("presents confirmation as labelled groups with edit links and safe credential status", () => {
    const confirmation = source.slice(source.indexOf('<section v-show="editorStep === 5"'), source.indexOf("</a-form>"));
    expect(confirmation).toContain('class="cascade-confirm-grid"');
    expect(confirmation.match(/<dl>/g)).toHaveLength(4);
    for (const step of [1, 2, 3, 4]) {
      expect(confirmation).toContain(`@click="editorStep = ${step}"`);
    }
    for (const label of ["上级平台 ID", "认证用户名", "认证密码", "媒体宣告地址", "注册有效期", "心跳间隔", "发布业务分组"]) {
      expect(confirmation).toContain(`<dt>${label}</dt>`);
    }
    expect(confirmation).toContain('form.profileOverride === "auto" ? "自动协商"');
    expect(confirmation).toContain('form.charsetOverride || "跟随协议版本"');
    expect(confirmation).not.toContain("{{ form.password }}");
    expect(confirmation).not.toContain(" · ");
  });

  it("guides configuration through five steps and validates before advancing", () => {
    expect(source).toContain('<a-steps :current="editorStep"');
    expect(source).toContain('title="完成确认"');
    expect(source).toContain("validateCascadeStep(form, editorStep.value, editing.value?.catalogBatchSize)");
    expect(source).toContain('@click="nextEditorStep"');
    expect(source).toContain('v-show="editorStep === 5"');
    expect(source).toContain('v-show="editorStep === 3" class="form-section"');
    expect(source).toContain('v-show="editorStep === 4" class="form-section"');
    expect(source.match(/<\/a-form>/g)).toHaveLength(1);
    expect(source).toContain("stepNumberFields(editorStep.value)");
    expect(source).toContain("step === 3) return [registerExpiresField, keepaliveIntervalField]");
    expect(source).toContain("step === 4) return [maxStreamsField]");
    expect(source).toContain("form.authUsername || form.localDeviceId");
    expect(source).toContain('form.publishGroup ? "发布" : "不发布"');
    for (const field of ["upstreamDomain", "localDomain", "authUsername"]) {
      expect(source).toContain(`:value="form.${field}.length"`);
    }
  });

  it("groups upstream credentials with the upstream connection instead of the local identity", () => {
    const sections = source.match(/<section[^>]*class="form-section"[^>]*>[\s\S]*?<\/section>/g) ?? [];
    const upstream = sections.find(section => section.includes("<h3>上级平台</h3>"))!;
    const local = sections.find(section => section.includes("<h3>本平台身份</h3>"))!;
    for (const field of ["authUsername", "password"]) {
      expect(upstream).toContain(`v-model="form.${field}"`);
      expect(local).not.toContain(`v-model="form.${field}"`);
    }
    expect(upstream).toContain("editing?.credentialNeedsReset");
    expect(upstream).toContain("留空则保持原密码");
    for (const field of ["localDeviceId", "localDomain", "localSipIp", "localSipPort", "mediaAdvertiseIp"]) {
      expect(local).toContain(`v-model="form.${field}"`);
      expect(upstream).not.toContain(`v-model="form.${field}"`);
    }
  });

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
    expect(source).toContain(
      "Object.assign(form, defaultCascadePlatform(), cascadeLocalIdentityDefaults(localSipConfig.value || undefined))"
    );
  });

  it("does not repeat the route title and description above the workspace", () => {
    expect(source).not.toContain("<h2>国标级联</h2>");
    expect(source).not.toContain("管理 UVP 作为下级平台向上级平台的注册、资源共享和运行状态。");
    expect(source).not.toContain('<header class="cascade-header">');
    expect(source).toContain("<template #actions>");
    expect(source).toContain('class="cascade-search-actions"');
  });

  it("keeps the platform editor dialog and removes the old resource dialogs", () => {
    const editorStart = source.indexOf("<a-modal");
    const editorEnd = source.indexOf("</a-modal>", editorStart) + "</a-modal>".length;

    expect(editorStart).toBeGreaterThan(-1);
    expect(editorEnd).toBeGreaterThan(editorStart);
    const editor = source.slice(editorStart, editorEnd);
    expect(editor).toContain('v-model:visible="editorVisible"');
    expect(editor).toContain('modal-class="uvp-system-dialog cascade-platform-dialog"');
    expect(editor).toContain('width="min(820px, calc(100vw - 24px))"');
    expect(editor).toContain(':mask-closable="!saving"');
    expect(editor).toContain("<template #footer>");
    expect(editor).not.toContain("<a-drawer");
    expect(source.match(/<a-modal\b/g)).toHaveLength(1);
    expect(source).not.toContain("BusinessGroupManager");
    expect(source).not.toContain("shareVisible");
    expect(source).not.toContain("sharedDevicesVisible");
    expect(source).not.toContain("cascade-share-dialog");
    expect(source).not.toContain("shared-devices-dialog");
  });

  it("uses one shared-and-directory entry for platform resource management", () => {
    const actionStart = source.indexOf('data-action="shared-directory"');
    const editActionStart = source.indexOf('class="uvp-table-action uvp-table-action--edit"', actionStart);
    const action = source.slice(actionStart, editActionStart);
    const summaryLink = source.match(/<a-link class="shared-channels-link"[^>]*>[\s\S]*?<\/a-link>/)?.[0] ?? "";

    expect(actionStart).toBeGreaterThan(-1);
    expect(action).toContain("共享与目录");
    expect(action).toContain(':disabled="!canView"');
    expect(action).toContain('@click="openSharedDirectory(record)"');
    expect(source.match(/data-action="shared-directory"/g)).toHaveLength(1);
    expect(source).not.toContain('data-action="business-group"');
    expect(source).not.toContain('data-action="share-platform"');
    expect(source).toContain('import { useRouter } from "vue-router"');
    expect(source).toContain("router.push(`/gb28181/cascade/${platform.id}/resources`)");
    expect(summaryLink).toContain(':disabled="!canView"');
    expect(summaryLink).toContain('@click="openSharedDirectory(record)"');
    expect(source).toContain("function sharedSummary(id: number)");
    expect(source).toContain("loadSharedSnapshot(platform.id)");
  });

  it("keeps list actions behind their existing permission checks and preserves platform editing", () => {
    expect(source).toContain('data-action="shared-directory"');
    expect(source).toContain(':disabled="!canView"');
    expect(source).toContain(':disabled="!canShare || !record.enabled"');
    expect(source).toContain(':disabled="!canManage" @click="openEdit(record)"');
    expect(source).toContain("function openEdit(platform: CascadePlatform)");
    expect(source).toContain("async function savePlatform()");
    expect(source.match(/if \(cascadeRuntimeSyncPending\(response\)\)/g)).toHaveLength(2);
    expect(source).toContain("配置已保存，但运行时尚未同步；请稍后重试");
    expect(source).toContain("平台已启用，但运行时尚未完成注册同步");
    expect(source).toContain("上级平台已保存并启用，正在向上级注册；通道请到“共享与目录”配置");
    expect(source).toContain("上级平台已保存，当前未启用；通道请到“共享与目录”配置");
    expect(source).toContain("保存后可到平台列表的“共享与目录”配置通道。");
    expect(source).toContain('<a-table-column title="操作" :width="340"');
  });

  it("exposes register/deregister as an inline enable switch instead of a dropdown option", () => {
    expect(source).toContain('<a-table-column title="启用"');
    expect(source).toMatch(
      /<a-switch[\s\S]*?:model-value="record\.enabled"[\s\S]*?@change="\(value: boolean \| string \| number\) => toggleEnabled\(record, Boolean\(value\)\)"/
    );
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

  it("follows the form-input template rules in the platform editor dialog", () => {
    const editorStart = source.indexOf("<a-modal");
    const editor = source.slice(editorStart, source.indexOf("</a-modal>", editorStart));
    // 硬规则 2:数值输入不使用 a-input-number 的静默 clamp。
    expect(editor).not.toContain("<a-input-number");
    expect(editor.match(/<s-number-field/g)?.length).toBeGreaterThanOrEqual(5);
    // 硬规则 3:20 位国标编码带位数指示。
    expect(
      editor.match(/<s-counter-suffix :value="(form\.upstreamServerId|form\.localDeviceId)\.length" :total="20" \/>/g)?.length
    ).toBe(2);
    // 上级提供的认证密码只需输入和显隐，不展示强度提示。
    expect(editor).toMatch(/<a-input-password\s+v-model="form.password"\s+allow-clear/);
    expect(editor).not.toContain("<s-password-field");
    // 硬规则 4:关键字段 blur 后显示逐字段错误。
    expect(editor.match(/@blur="touched\.\w+ = true"/g)?.length).toBeGreaterThanOrEqual(6);
    expect(source).toContain("cascadeFormFieldErrors(form, touched)");
    // 硬规则 1:文本输入开 allow-clear(选择器/密码组件除外)。
    const inputsMissingClear = (editor.match(/<a-input\b[^>]*\/>/g) ?? []).filter(tag => !tag.includes("allow-clear"));
    expect(inputsMissingClear).toEqual([]);
  });
});

describe("upstream subscription integration", () => {
  it("renders the per-platform snapshot in the ledger with refresh-failure handling", () => {
    expect(source).toContain('<a-table-column title="上级订阅"');
    expect(source).toContain('<UpstreamSubscriptions :subscriptions="record.subscriptions" :stale="Boolean(errorMessage)" />');
    expect(source).toContain('import UpstreamSubscriptions from "./UpstreamSubscriptions.vue"');
  });
});
