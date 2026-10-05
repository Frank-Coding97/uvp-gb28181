import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";
import { hasMarkup, hasRuleBlock } from "@/test/source-assert";

const CONTROL_COMPONENTS = [
  "components/play-console/PlayConsoleDialogs.vue",
  "components/play-console/PlayConsoleProbePanel.vue",
  "components/play-console/PlayConsolePtzSidebar.vue",
  "components/play-console/PtzScanCard.vue",
  "device-mgmt/DeviceConfigDrawer.vue",
  "device-mgmt/DeviceConfigOsdBlocks.vue",
  "device-mgmt/DeviceConfigSlider.vue",
  "device-mgmt/DeviceConfigTextItems.vue",
  "device-mgmt/DeviceConfigWeekPlan.vue"
];

const PLAY_CONSOLE_ACTION_COMPONENTS = [
  "components/play-console/PlayConsoleTitleBar.vue",
  "components/play-console/PlayConsoleProtocolBar.vue",
  "components/play-console/PlayConsoleTabs.vue"
];

describe("播放控制台表单控件", () => {
  it("所有禁用动作和配置区不叠加整体透明度，避免文字再次变淡", () => {
    const paths = [
      "components/PlayConsoleLinked.vue",
      "components/play-console/detail-cards.scss",
      "components/play-console/PlayConsoleDialogs.vue",
      "components/play-console/PlayConsolePtzSidebar.vue",
      "components/play-console/PictureVideoParamCard.vue",
      "device-mgmt/DeviceConfigDrawer.vue",
      "device-mgmt/DeviceConfigOsdBlocks.vue",
      "device-mgmt/DeviceConfigSlider.vue",
      "device-mgmt/DeviceConfigWeekPlan.vue"
    ];
    const violations = paths.flatMap(path => {
      const source = readFileSync(resolve(process.cwd(), "src/views/gb28181", path), "utf8");
      return [...source.matchAll(/([^{}]+)\{([^{}]*)\}/g)]
        .filter(
          ([, selector, body]) =>
            /:disabled|\.is-disabled|\.disabled|\.is-muted|\.is-static/.test(selector) && /opacity:\s*0\./.test(body)
        )
        .map(([, selector]) => `${path}: ${selector.trim()}`);
    });
    expect(violations).toEqual([]);
  });

  it("播放失败重试使用 Arco 按钮并保留原事件和样式", () => {
    const source = readFileSync(resolve(process.cwd(), "src/views/gb28181/components/PlayConsoleLinked.vue"), "utf8");
    expect(
      hasMarkup(
        source,
        '<a-button type="primary" size="mini" html-type="button" class="btn-primary player-retry" @click="reconnect">'
      )
    ).toBe(true);
  });

  it("画面草稿浮条的还原与下发操作使用 Arco 按钮", () => {
    const source = readFileSync(resolve(process.cwd(), "src/views/gb28181/components/PlayConsoleLinked.vue"), "utf8");
    expect(source).not.toMatch(/<button\b/);
  });

  it("紧凑按钮样式覆盖 Arco 类型默认值并保留原尺寸", () => {
    const source = readFileSync(resolve(process.cwd(), "src/views/gb28181/components/play-console/detail-cards.scss"), "utf8");
    expect(
      hasRuleBlock(source, '.resource-sync-btn.arco-btn[type="button"]', "padding: 2px 6px", "color: var(--uvp-text-secondary)")
    ).toBe(true);
    expect(hasRuleBlock(source, '.mask-switch.arco-btn[type="button"]', "height: 20px", "padding: 0 7px")).toBe(true);
    expect(hasRuleBlock(source, '.mirror-choice.arco-btn[type="button"]', "padding: 6px 4px")).toBe(true);
  });

  it("普通标签和状态说明使用清晰的次级文字，保留语义状态颜色", () => {
    const source = readFileSync(resolve(process.cwd(), "src/views/gb28181/components/play-console/detail-cards.scss"), "utf8");
    for (const selector of [".section-meta", ".linked-card-note", ".scan-label", ".mask-canvas-note"]) {
      expect(hasRuleBlock(source, selector, "color: var(--uvp-text-secondary)")).toBe(true);
    }
    expect(hasRuleBlock(source, ".section-meta.good", "color: var(--uvp-brand-cyan)")).toBe(true);
    expect(hasRuleBlock(source, ".mask-canvas-note.is-unverified", "color: var(--uvp-warning)")).toBe(true);
  });

  it("状态徽标统一使用语义色 token，暗色主题不再依赖浅色硬编码", () => {
    const cardSource = readFileSync(
      resolve(process.cwd(), "src/views/gb28181/components/play-console/detail-cards.scss"),
      "utf8"
    );
    const drawerSource = readFileSync(resolve(process.cwd(), "src/views/gb28181/device-mgmt/DeviceConfigDrawer.vue"), "utf8");

    expect(
      hasRuleBlock(
        cardSource,
        '.mask-switch.arco-btn[type="button"].on',
        "color: var(--uvp-success)",
        "background: var(--uvp-success-soft)",
        "border-color: var(--uvp-success-border)"
      )
    ).toBe(true);
    expect(hasMarkup(drawerSource, "&.is-ready { color: var(--uvp-success); background: var(--uvp-success-soft);")).toBe(true);
    expect(hasMarkup(drawerSource, "&.is-ok { color: var(--uvp-success); background: var(--uvp-success-soft);")).toBe(true);
    expect(
      hasRuleBlock(
        drawerSource,
        '.dcg-row-hint[data-source="设备"]',
        "color: var(--uvp-success)",
        "background: var(--uvp-success-soft)"
      )
    ).toBe(true);
    expect(drawerSource).not.toMatch(/color:\s*#047857|background:\s*#ecfdf5|color:\s*#059669/);
  });

  it("设备配置控件为默认、聚焦和禁用态提供统一 Arco 外层状态", () => {
    const source = readFileSync(resolve(process.cwd(), "src/views/gb28181/device-mgmt/DeviceConfigDrawer.vue"), "utf8");
    expect(source).toContain(":deep(.dcg-select.arco-select-view)");
    expect(source).toContain(":deep(.dcg-select.arco-select-view-focus)");
    expect(source).toContain(":deep(.dcg-select.arco-select-view-disabled)");
    expect(source).toContain(":deep(.dcg-input.arco-input-wrapper.arco-input-disabled)");
    expect(source).toContain("background: var(--uvp-dialog-control-bg");
    expect(source).toContain("color: var(--uvp-text-disabled)");
  });

  it("看守位的处理中、成功和中性状态以及未知对照均保持明确层级", () => {
    const source = readFileSync(resolve(process.cwd(), "src/views/gb28181/components/play-console/detail-cards.scss"), "utf8");
    expect(
      hasRuleBlock(
        source,
        ".home-config.state-loading .home-state-icon",
        "color: var(--uvp-brand)",
        "background: var(--uvp-brand-soft)"
      )
    ).toBe(true);
    expect(
      hasRuleBlock(
        source,
        ".home-config.state-success .home-state-icon",
        "color: var(--uvp-success)",
        "background: var(--uvp-success-soft)"
      )
    ).toBe(true);
    expect(hasRuleBlock(source, ".home-state-icon", "color: var(--uvp-text-secondary)")).toBe(true);
    expect(hasRuleBlock(source, ".vpc-verdict.is-unknown", "color: var(--uvp-text-secondary)")).toBe(true);
  });

  it("共享配置控件的禁用态保留层级和边界，不用整体透明度压低文字", () => {
    for (const name of ["DeviceConfigSlider.vue", "DeviceConfigTextItems.vue", "DeviceConfigWeekPlan.vue"]) {
      const source = readFileSync(resolve(process.cwd(), "src/views/gb28181/device-mgmt", name), "utf8");
      expect(source).toContain("&.is-disabled");
      expect(source).toContain("color: var(--uvp-text-disabled)");
      expect(source).not.toContain("opacity: 0.55");
    }
  });

  it("状态反馈使用主题语义色，禁用帧概览不再整体变淡", () => {
    const cards = readFileSync(resolve(process.cwd(), "src/views/gb28181/components/play-console/detail-cards.scss"), "utf8");
    const title = readFileSync(
      resolve(process.cwd(), "src/views/gb28181/components/play-console/PlayConsoleTitleBar.vue"),
      "utf8"
    );
    const dialogs = readFileSync(
      resolve(process.cwd(), "src/views/gb28181/components/play-console/PlayConsoleDialogs.vue"),
      "utf8"
    );

    expect(hasRuleBlock(cards, ".frame-overview.muted", "color: var(--uvp-text-disabled)", "opacity: 1")).toBe(true);
    expect(cards).toContain(".frame-overview.muted .frame-overview-bar");
    expect(hasRuleBlock(title, ".session-badge.paused", "color: var(--uvp-text-secondary)")).toBe(true);
    expect(hasRuleBlock(dialogs, ".preset-save-count.ok", "color: var(--uvp-success)")).toBe(true);
  });

  it("画面遮挡和镜像按钮的禁用态保留文字对比度，不整体降低透明度", () => {
    const source = readFileSync(resolve(process.cwd(), "src/views/gb28181/components/play-console/detail-cards.scss"), "utf8");
    expect(
      hasRuleBlock(
        source,
        '.mask-switch.arco-btn[type="button"]:disabled',
        "color: var(--uvp-text-disabled)",
        "background: var(--uvp-dialog-control-bg)",
        "border-color: var(--uvp-panel-border)"
      )
    ).toBe(true);
    expect(
      hasRuleBlock(
        source,
        '.mirror-choice.arco-btn[type="button"]:disabled',
        "color: var(--uvp-text-disabled)",
        "background: var(--uvp-dialog-control-bg)",
        "border-color: var(--uvp-panel-border)"
      )
    ).toBe(true);
    expect(source).not.toMatch(
      /\.mask-switch\.arco-btn\[type="button"\]:disabled,\s*\n\.mirror-choice\.arco-btn\[type="button"\]:disabled\s*\{[^}]*opacity:/s
    );
  });

  it("探针禁用态和未完成摘要保留文字层级", () => {
    const source = readFileSync(
      resolve(process.cwd(), "src/views/gb28181/components/play-console/PlayConsoleProbeSidebar.vue"),
      "utf8"
    );
    expect(
      hasRuleBlock(
        source,
        '.probe-action.arco-btn[type="button"]:disabled',
        "color: var(--uvp-text-disabled)",
        "background: var(--uvp-dialog-control-bg)"
      )
    ).toBe(true);
    expect(hasRuleBlock(source, ".probe-summary.muted strong", "color: var(--uvp-text-secondary)")).toBe(true);
    expect(source).not.toMatch(/\.probe-action\.arco-btn\[type="button"]:disabled\s*\{[^}]*opacity:\s*0(?:\.\d+)?/s);
    expect(source).not.toMatch(/\.probe-summary\.muted\s*\{[^}]*opacity:/s);
  });

  it("协议快捷按钮的禁用态使用明确的深色栏文字层级", () => {
    const source = readFileSync(
      resolve(process.cwd(), "src/views/gb28181/components/play-console/PlayConsoleProtocolBar.vue"),
      "utf8"
    );
    expect(
      hasRuleBlock(
        source,
        '.proto-btn.arco-btn[type="button"]:disabled',
        "color: rgb(148 163 184 / 72%)",
        "background: rgb(255 255 255 / 4%)"
      )
    ).toBe(true);
    expect(source).not.toMatch(/\.proto-btn\.arco-btn\[type="button"]:disabled\s*\{[^}]*opacity:\s*0(?:\.\d+)?/s);
  });

  it("控制台分区标题和探针标题保持清晰的层级对比", () => {
    const consoleSource = readFileSync(resolve(process.cwd(), "src/views/gb28181/components/PlayConsoleLinked.vue"), "utf8");
    const probeSource = readFileSync(
      resolve(process.cwd(), "src/views/gb28181/components/play-console/PlayConsoleProbeSidebar.vue"),
      "utf8"
    );
    expect(hasRuleBlock(consoleSource, ".section-title", "color: var(--uvp-text-primary)")).toBe(true);
    expect(hasRuleBlock(consoleSource, ".section-meta", "color: var(--uvp-text-secondary)")).toBe(true);
    expect(hasRuleBlock(probeSource, ".section-title", "color: var(--uvp-text-primary)")).toBe(true);
  });

  it("标题栏、协议栏和工作区页签使用 Arco 按钮承载标准操作", () => {
    const violations = PLAY_CONSOLE_ACTION_COMPONENTS.flatMap(path => {
      const source = readFileSync(resolve(process.cwd(), "src/views/gb28181", path), "utf8");
      return [...source.matchAll(/<button\b/g)].map(match => `${path}: <${match[0].slice(1, -1)}>`);
    });

    expect(violations).toEqual([]);
    for (const path of PLAY_CONSOLE_ACTION_COMPONENTS) {
      const source = readFileSync(resolve(process.cwd(), "src/views/gb28181", path), "utf8");
      expect(source).toContain("<a-button");
    }
  });

  it("统一使用 Arco 表单组件，不直接渲染原生输入、下拉或文本域", () => {
    const violations = CONTROL_COMPONENTS.flatMap(path => {
      const source = readFileSync(resolve(process.cwd(), "src/views/gb28181", path), "utf8");
      return [...source.matchAll(/<(input|select|textarea)\b/g)].map(match => `${path}: <${match[1]}>`);
    });

    expect(violations).toEqual([]);
  });

  it("滑块旋钮把圆形视觉样式应用在 Arco 的伪元素上，避免出现方框叠层", () => {
    const source = readFileSync(resolve(process.cwd(), "src/views/gb28181/device-mgmt/DeviceConfigSlider.vue"), "utf8");
    expect(source).toContain(".cfg-slider-track :deep(.arco-slider-track::before)");
    expect(source).toContain(".cfg-slider-track :deep(.arco-slider-btn::after)");
    expect(source).toContain("border-radius: 50%");
    expect(source).not.toMatch(/\.cfg-slider-track :deep\(\.arco-slider-btn\)\s*\{[^}]*border:/s);
  });

  it("数值输入框收紧 Arco 外层内边距，避免窄布局遮挡数值", () => {
    const source = readFileSync(resolve(process.cwd(), "src/views/gb28181/device-mgmt/DeviceConfigSlider.vue"), "utf8");
    expect(source).toMatch(/\.cfg-slider-input :deep\(\.arco-input-wrapper\)\s*\{[^}]*padding:\s*0 5px/s);
    expect(source).toMatch(/\.cfg-slider-input :deep\(\.arco-input\)\s*\{[^}]*min-width:\s*0;[^}]*padding:\s*0;/s);
  });

  it("暗色模式下码率选中项和 OSD 开启态使用清晰的纯蓝填充", () => {
    const drawer = readFileSync(resolve(process.cwd(), "src/views/gb28181/device-mgmt/DeviceConfigDrawer.vue"), "utf8");
    const osd = readFileSync(resolve(process.cwd(), "src/views/gb28181/device-mgmt/DeviceConfigOsdBlocks.vue"), "utf8");

    expect(drawer).toMatch(
      /body\[arco-theme="dark"\] \.dcg-segment button\.arco-btn\[type="button"\]\.is-on:not\(:disabled\)\s*\{[^}]*#2563eb/s
    );
    expect(osd).toMatch(
      /body\[arco-theme="dark"\] \.osd-switch\.arco-btn\[type="button"\]\.is-on:not\(:disabled\)\s*\{[^}]*#2563eb/s
    );
  });

  it("嵌入式视频编码面板保留 515px 最小高度并隐藏底部汇总条", () => {
    const source = readFileSync(resolve(process.cwd(), "src/views/gb28181/device-mgmt/DeviceConfigDrawer.vue"), "utf8");
    expect(source).toMatch(/\.dcg-stream\.is-compact\s*\{[^}]*min-height:\s*515px/s);
    expect(source).toContain('<p v-if="!embedded" class="dcg-params-foot"');
  });
});
