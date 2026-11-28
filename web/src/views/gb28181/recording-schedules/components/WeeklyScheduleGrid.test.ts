import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { mount } from "@vue/test-utils";
import { describe, expect, it } from "vitest";
import WeeklyScheduleGrid from "./WeeklyScheduleGrid.vue";

const source = readFileSync(
  resolve(process.cwd(), "src/views/gb28181/recording-schedules/components/WeeklyScheduleGrid.vue"),
  "utf8"
);

describe("WeeklyScheduleGrid shared modes", () => {
  it("renders the same 7 by 48 half-hour grid in edit and read-only modes", () => {
    expect(source).toContain("const SLOT_MINUTES = 30");
    expect(source).toContain("Array.from({ length: 48 }");
    expect(source).toContain("props.editable");
    expect(source).toContain('v-if="editable"');
    expect(source).toMatch(/v-else\s+class="time-slot readonly"/);
  });

  it("preserves the primary blue surface in light mode", () => {
    const active = source.match(/\.time-slot\.active\s*\{([^}]*)}/s)?.[1] ?? "";
    expect(active).toContain("var(--uvp-solid-bg)");
    expect(active).toContain("var(--uvp-solid-hover-bg)");
    // ⛔ 不得回退成 brand
    expect(active).not.toContain("var(--uvp-brand)");
    // 悬停态同样不混 brand
    const hover = source.match(/button\.time-slot:hover\s*\{([^}]*)}/s)?.[1] ?? "";
    expect(hover).toContain("var(--uvp-solid-bg)");
    expect(hover).not.toContain("var(--uvp-brand)");
    // solid 组本身不能被写成 var() 别名
    const tokens = readFileSync(resolve(process.cwd(), "src/style/var/uvp-ui-tokens.scss"), "utf8");
    expect(tokens).toMatch(/--uvp-solid-bg:\s*#2563eb;/);
  });

  it("softens dark selected slots without bright inset outlines in both grid modes", () => {
    const active =
      source.match(/:global\(body\[arco-theme="dark"\] \.week-slot-grid \.time-slot\.active\)\s*\{([^}]*)}/s)?.[1] ?? "";
    expect(active).toContain("color-mix(in srgb, var(--uvp-brand) 78%, var(--uvp-table-row-bg))");
    expect(active).toContain("box-shadow: none");
    expect(active).toContain("border-right-color:");
    const hover =
      source.match(/:global\(body\[arco-theme="dark"\] \.week-slot-grid button\.time-slot\.active:hover\)\s*\{([^}]*)}/s)?.[1] ??
      "";
    expect(hover).toContain("color-mix(in srgb, var(--uvp-brand) 88%, var(--uvp-table-row-bg))");
  });

  it("emits immutable slot updates only from edit interactions", () => {
    expect(source).toContain('emit("update:slots", nextSlots)');
    expect(source).toContain("if (!props.editable)");
    expect(source).toContain("function fillDay");
    expect(source).toContain("function clearDay");
  });

  it("keeps read-only cells non-interactive and emits painted slots in edit mode", async () => {
    const emptySlots = Array.from({ length: 7 }, () => Array.from({ length: 48 }, () => false));
    const readonly = mount(WeeklyScheduleGrid, { props: { slots: emptySlots, editable: false } });
    expect(readonly.findAll(".time-slot.readonly")).toHaveLength(336);
    expect(readonly.findAll("button.time-slot")).toHaveLength(0);
    expect(readonly.emitted("update:slots")).toBeUndefined();

    const editable = mount(WeeklyScheduleGrid, { props: { slots: emptySlots, editable: true } });
    await editable.find("button.time-slot").trigger("mousedown");
    const updates = editable.emitted<boolean[][][]>("update:slots");
    expect(updates).toHaveLength(1);
    expect(updates?.[0][0][0][0]).toBe(true);
    expect(emptySlots[0][0]).toBe(false);
  });

  it("「全天 / 清空」两枚按钮塞得进末列（曾经被裁掉半颗）", () => {
    /*
     * 现场（老板 2026-11-04 截图）：末列 80px，而全局 small 档按钮 min-width 是 64px，
     * 两枚并排 = 64×2 + 4 + 10 = 142px ⇒ 顶出列外，被 `.week-slot-grid{overflow:hidden}`
     * 裁成「清空」只剩左半边。
     * ⛔ 原因是原来那条 `.day-actions :deep(.arco-btn-size-mini)` 写错了类名：模板传的是
     * `size="small"`（mini 已全站并入 small）⇒ 紧凑规则从未命中过。
     * 这里同时钉三件事：按钮挂了专用类、紧凑规则还在、末列宽度容得下。
     */
    expect(source.match(/class="week-day-action"/g)).toHaveLength(2);
    const compact = source.match(/\.day-actions :deep\(\.week-day-action\)\s*\{([^}]*)}/s)?.[1] ?? "";
    expect(compact).toContain("min-width: 34px");
    // ⛔ 死选择器不许回来：mini 类在模板里根本不存在。
    //    用「行首选择器」判据，注释里提到类名不会误伤自己。
    expect(source).not.toMatch(/^\.day-actions :deep\(\.arco-btn-size-mini\)/m);
    // 末列宽度 ≥ 5(padding) + 34 + 4(gap) + 34 + 5(padding) = 82
    const lastColumn = source.match(/grid-template-columns: 82px repeat\(24, minmax\(36px, 1fr\)\) (\d+)px/)?.[1];
    expect(Number(lastColumn)).toBeGreaterThanOrEqual(82);
  });
});
