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

  /**
   * 回归护栏：选中格的底色必须走 **solid 组**（--uvp-solid-bg/#2563eb，与主按钮同色），
   * ⛔ 不得用 --uvp-brand —— brand 是**文字/图标色**，暗色下是浅蓝 #60a5fa，
   * 拿它当整格底色会变成一大片浅蓝发白（老板要求「和按钮同步」）。
   * 同时守住 solid 组「不许写 var() 别名」的约定。
   */
  it("paints selected slots with the solid surface token so they match the primary button", () => {
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
});
