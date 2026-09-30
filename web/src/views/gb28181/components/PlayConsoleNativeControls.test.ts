import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

const CONTROL_COMPONENTS = [
  "components/play-console/PlayConsoleDialogs.vue",
  "components/play-console/PlayConsolePtzSidebar.vue",
  "components/play-console/PtzScanCard.vue",
  "device-mgmt/DeviceConfigDrawer.vue",
  "device-mgmt/DeviceConfigOsdBlocks.vue",
  "device-mgmt/DeviceConfigSlider.vue",
  "device-mgmt/DeviceConfigTextItems.vue",
  "device-mgmt/DeviceConfigWeekPlan.vue"
];

describe("播放控制台表单控件", () => {
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

    expect(drawer).toMatch(/:global\(body\[arco-theme="dark"\]\) \.dcg-segment button\.is-on\s*\{[^}]*#2563eb/s);
    expect(osd).toMatch(/:global\(body\[arco-theme="dark"\]\) \.osd-switch\.is-on\s*\{[^}]*#2563eb/s);
  });

  it("嵌入式视频编码面板保留 515px 最小高度并隐藏底部汇总条", () => {
    const source = readFileSync(resolve(process.cwd(), "src/views/gb28181/device-mgmt/DeviceConfigDrawer.vue"), "utf8");
    expect(source).toMatch(/\.dcg-stream\.is-compact\s*\{[^}]*min-height:\s*515px/s);
    expect(source).toContain('<p v-if="!embedded" class="dcg-params-foot"');
  });
});
