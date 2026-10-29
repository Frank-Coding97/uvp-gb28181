import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

/**
 * 存储卡格式化（A.2.3.1.13）的**权限与入口契约**（源码级）。
 *
 * 为什么只能钉源码：这几条是"跨文件接线"，挂载单测看不到。
 *   - `index.vue` 里权限码 → prop 的那一跳（下面会说明为什么不能只靠单测）；
 *   - 面板的 prop 默认值（fail-closed）；
 *   - 按钮"禁用而非隐藏"。
 *
 * 已经由行为用例覆盖的部分**不在这里重复**（无权限时禁用、二次确认、无应答文案、
 * 进度跟踪带宽上限等，见 StorageCardStatusPanel.test.ts）—— 重复钉会让两处一起腐烂。
 */
const DIR = resolve(process.cwd(), "src/views/gb28181/device-mgmt");
const indexSource = readFileSync(resolve(DIR, "index.vue"), "utf8");
const panelSource = readFileSync(resolve(DIR, "StorageCardStatusPanel.vue"), "utf8");
const dialogSource = readFileSync(resolve(DIR, "StorageCardFormatDialog.vue"), "utf8");

describe("存储卡格式化的权限与入口契约", () => {
  it("用的是独立权限码 gb28181:device:format_sd，不复用设备控制/查看", () => {
    expect(indexSource).toContain('const canFormatStorageCard = computed(() => hasPermission("gb28181:device:format_sd"));');
    // ⛔ 破坏性动作不能挂在日常操作权限上：device:control 能开灯/能云台，
    //    与"把卡上的录像全抹掉"不该是同一个授权。
    expect(indexSource).not.toContain('canFormatStorageCard = computed(() => hasPermission("gb28181:device:control"));');
    expect(indexSource).not.toContain('canFormatStorageCard = computed(() => hasPermission("gb28181:ptz:view"));');
  });

  it("存储卡页把读权限与格式化权限分别接到面板上", () => {
    const panelUse = indexSource.slice(
      indexSource.indexOf("<StorageCardStatusPanel"),
      indexSource.indexOf("/>", indexSource.indexOf("<StorageCardStatusPanel"))
    );
    expect(panelUse.length).toBeGreaterThan(0);
    expect(panelUse).toContain(':can-view="canViewDeviceFacts"');
    expect(panelUse).toContain(':can-format="canFormatStorageCard"');
    // ⛔ 别把格式化接到读权限或设备控制上（那正是"独立权限码形同虚设"的写法）。
    expect(panelUse).not.toContain(':can-format="canViewDeviceFacts"');
    expect(panelUse).not.toContain(':can-format="canControlDevice"');
  });

  it("面板的 canFormat 是可选且 fail-closed 的 prop", () => {
    // ⛔ 缺省必须是"不能格式化"：万一有调用方忘了传，宁可按钮不可用，
    //    也不能默认放行一个不可撤销的破坏性动作。
    expect(panelSource).toMatch(/canFormat\?: boolean;/);
    expect(panelSource).toMatch(/function canFormatCard\(card: StorageCard\)[\s\S]{0,200}Boolean\(props\.canFormat\)/);
    // 不传的调用方（如旧引用）走的还是禁用态。
    expect(panelSource).not.toContain("canFormat = true");
  });

  it("无权限时按钮禁用而不是隐藏（用户要知道这里有个功能）", () => {
    const button = panelSource.slice(
      panelSource.indexOf('data-testid="`storage-format-${card.cardId}`"') - 400,
      panelSource.indexOf('data-testid="`storage-format-${card.cardId}`"') + 100
    );
    expect(button).toContain(':disabled="!canFormatCard(card)"');
    // ⛔ 没有 v-if 门禁：隐藏会让用户以为平台没这个能力，
    //    也让"未授权用户按钮不可用"这条验收退化成"看不到"。
    expect(button).not.toMatch(/v-if="[^"]*canFormat/);
  });

  it("确认弹窗把「无权限」与「无应答」两件事都写在弹窗里", () => {
    expect(dialogSource).toContain("当前账号没有存储卡格式化权限。");
    // ⛔ 「已下发」不等于「已完成」是这条命令的**协议事实**（9.3.1 d)），
    //    写进弹窗而不是只在提交后提示：用户是在点之前做决定的。
    expect(dialogSource).toContain("无应答命令");
    expect(dialogSource).toContain("9.3.1 d)");
    expect(dialogSource).toContain("全部录像会被清空且无法恢复");
  });
});
