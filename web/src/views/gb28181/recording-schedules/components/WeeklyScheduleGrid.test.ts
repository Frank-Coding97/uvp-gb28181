import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { mount } from "@vue/test-utils";
import { describe, expect, it } from "vitest";
import WeeklyScheduleGrid from "./WeeklyScheduleGrid.vue";

const source = readFileSync(resolve(process.cwd(), "src/views/gb28181/recording-schedules/components/WeeklyScheduleGrid.vue"), "utf8");

describe("WeeklyScheduleGrid shared modes", () => {
  it("renders the same 7 by 48 half-hour grid in edit and read-only modes", () => {
    expect(source).toContain("const SLOT_MINUTES = 30");
    expect(source).toContain("Array.from({ length: 48 }");
    expect(source).toContain("props.editable");
    expect(source).toContain('v-if="editable"');
    expect(source).toContain('v-else class="time-slot readonly"');
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
