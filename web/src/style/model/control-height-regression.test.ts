import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

const readSource = (path: string) => readFileSync(resolve(process.cwd(), path), "utf8");

const pageSources = [
  "src/views/gb28181/alarm-management/index.vue",
  "src/views/gb28181/cloud-recordings/index.vue",
  "src/views/gb28181/device-mgmt/index.vue",
  "src/views/gb28181/security/preview.vue",
  "src/views/gb28181/sip-log-v2/index.vue",
  "src/views/gb28181/sip/PlatformInfo.vue",
  "src/views/gb28181/sip/ServiceConfig.vue",
  "src/views/gb28181/zlm/NodeConfig.vue",
  "src/views/gb28181/zlm/NodeDetail.vue",
  "src/views/gb28181/zlm/NodeList.vue",
  "src/views/gb28181/zlm/SchedulerLog.vue",
  "src/views/gb28181/zlm/SchedulerStrategy.vue",
  "src/views/system/account/account.vue",
  "src/views/system/affix/affix.vue",
  "src/views/system/codegen/codegen.vue",
  "src/views/system/dictionary/dictionary.vue",
  "src/views/system/division/division.vue",
  "src/views/system/log/log.vue",
  "src/views/system/login-log/index.vue",
  "src/views/system/menu/menu.vue",
  "src/views/system/pluginsmanager/pluginsmanager.vue",
  "src/views/system/role/role.vue",
  "src/views/system/sysapi/sysapi.vue",
  "src/views/system/sysconfig/sysconfig.vue",
  "src/views/system/sysjobresults/sysjobresultslist.vue",
  "src/views/system/sysjobs/sysjobslist.vue",
  "src/views/system/userinfo/userinfo.vue"
];

describe("control height regression", () => {
  it("keeps control sizing owned by the shared components", () => {
    const searchPanel = readSource("src/components/s-layout-search/index.vue");

    expect(searchPanel).toMatch(/:deep\(\.arco-input-wrapper\)[\s\S]*?min-height:\s*40px;/);
    expect(searchPanel).toMatch(/:deep\(\.arco-btn\)[\s\S]*?height:\s*40px;/);
    // ⛔ `src/style/model/uvp-density.scss` 已于 2026-09-29 删除：它从未被任何入口引入
    //    （main.ts → style.css → style/index.scss → style/model/index.scss 里没有它），
    //    且其分页段是 30px/4px 圆角的旧口径，与分页统一标准 32px/8px 冲突。
    //    分页尺寸现由 uvp-ui-language.scss 的 `.uvp-data-table / .uvp-pagination-bar` 统一负责，
    //    防回归见 src/style/model/pagination-unification.test.ts。
  });

  it("keeps search control colors owned by s-layout-search", () => {
    const searchPanel = readSource("src/components/s-layout-search/index.vue");

    expect(searchPanel).toMatch(
      /:deep\(\.arco-input-wrapper\),\s*:deep\(\.arco-select-view\),\s*:deep\(\.arco-picker\)\s*\{[^}]*background:\s*var\(--uvp-search-control-bg\)\s*!important;[^}]*border:\s*1px solid var\(--uvp-search-secondary-btn-border\)\s*!important;[^}]*box-shadow:\s*var\(--uvp-search-control-shadow\)\s*!important;/s
    );
    expect(searchPanel).toMatch(
      /:deep\(\.arco-select-view:hover\)[\s\S]*?:deep\(\.arco-select-view-focus\)[\s\S]*?\{[^}]*border-color:\s*var\(--uvp-brand\)\s*!important;[^}]*box-shadow:\s*var\(--uvp-search-control-focus-shadow\)\s*!important;/s
    );
  });

  it("does not force list-page controls to 44px", () => {
    for (const path of pageSources) {
      expect(readSource(path), path).not.toMatch(/height:\s*44px;\s*min-height:\s*44px;/);
    }
  });

  it("restores the original sizes of custom controls", () => {
    const playback = readSource("src/views/gb28181/device-record-playback/index.vue");
    // ⛔ 并列选择器会被 prettier 拆成两行（`.query-field input,\n.query-field select {`），
    //    选择器内部必须容许换行，否则格式化一次就红一次。
    expect(playback).toMatch(/\.query-field input,\s*\.query-field select\s*\{[^}]*height:\s*32px;/s);
    expect(playback).toMatch(/\.range-separator\s*\{[^}]*height:\s*32px;[^}]*line-height:\s*32px;/s);
    expect(playback).toMatch(/\.query-submit\s*\{[^}]*height:\s*32px;/s);
    expect(readSource("src/views/gb28181/device-mgmt/index.vue")).not.toMatch(/\.create-device-btn\s*\{[^}]*height:\s*44px;/s);
    expect(readSource("src/views/gb28181/zlm/NodeDetail.vue")).not.toMatch(/\.back-btn\s*\{[^}]*height:\s*44px;/s);
    expect(readSource("src/views/gb28181/zlm/NodeForm.vue")).not.toMatch(
      /:global\(\.zlm-node-form \.arco-input-wrapper\)[\s\S]*?\{[^}]*min-height:\s*44px;/s
    );
    expect(readSource("src/views/gb28181/zlm/SchedulerLog.vue")).not.toMatch(/\.log-count\s*\{[^}]*height:\s*44px;/s);
    expect(readSource("src/views/system/online-user/index.vue")).not.toMatch(
      /\.online-user-filter :deep\(\.arco-input-wrapper\),\s*\.online-user-filter :deep\(\.arco-select-view\)\s*\{[^}]*\b(?:min-)?height:\s*44px;/s
    );
  });
});
