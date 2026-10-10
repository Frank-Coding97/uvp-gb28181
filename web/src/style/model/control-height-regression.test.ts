import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";
import { hasRuleBlock } from "@/test/source-assert";

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
  it("keeps compact console buttons independent of Arco size and text-type defaults", () => {
    const details = readSource("src/views/gb28181/components/play-console/detail-cards.scss");
    expect(
      hasRuleBlock(
        details,
        ":where(.play-console-modal, .preset-popover, .home-settings-form, .cruise-save-form) .arco-btn",
        "height: auto",
        "min-height: 0"
      )
    ).toBe(true);
    expect(hasRuleBlock(details, '.resource-sync-btn.arco-btn[type="button"]', "padding: 2px 6px")).toBe(true);
    expect(hasRuleBlock(details, '.mask-switch.arco-btn[type="button"]', "height: 20px")).toBe(true);
    expect(hasRuleBlock(details, '.mirror-choice.arco-btn[type="button"]', "padding: 6px 4px")).toBe(true);
  });

  it("keeps control sizing owned by the shared components", () => {
    const searchPanel = readSource("src/components/s-layout-search/index.vue");

    expect(searchPanel).toMatch(/:deep\(\.arco-input-wrapper\)[\s\S]*?min-height:\s*40px;/);
    expect(searchPanel).toMatch(/:deep\(\.arco-btn\)[\s\S]*?height:\s*40px;/);
    // ⛔ `src/style/model/uvp-density.scss` 已于 2026-09-29 删除：它从未被任何入口引入
    //    （main.ts → style/index.scss → style/model/index.scss 里没有它），
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
    // ⛔ 别写串序正则（`/\.x\s*\{[^}]*a:[^}]*b:/`）：stylelint 的 recess-order 会重排声明，
    //    顺序一变就红。用 hasRuleBlock 钉「同一个规则块里有这几条声明」。
    //
    // 2026-10-03 该页控件换成 Arco：查询栏/控制条的高度（32px）改由 Arco 默认提供，页面只
    // 声明宽度与图标按钮的 32×32 方格。原来钉「.query-field input/select { height:32px }」
    // 和「.query-submit { height:32px }」的两条随原生控件一起退役，改钉新的落点 —— 目的不变：
    // 这些自定义控件的尺寸是刻意调过的，别被全局口径（30px / 44px）改掉。
    expect(hasRuleBlock(playback, ".query-field", "width: 360px")).toBe(true);
    expect(hasRuleBlock(playback, ".query-field", "width: 108px")).toBe(true);
    // 固定方格必须同时清 padding：Arco 按钮默认靠 `padding: 0 12px` 撑开，漏了会把图标挤出去。
    expect(hasRuleBlock(playback, ".icon-command", "width: 32px", "height: 32px", "padding: 0")).toBe(true);
    // 起止时间合成一个 a-range-picker 之后，「至」分隔符（.range-separator）退役；换来的是
    // 窄屏下时间范围必须独占一行，否则两个日期输入挤在半列里会把时间文本截断。
    expect(hasRuleBlock(playback, ".time-field", "grid-column: 1 / -1")).toBe(true);
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
