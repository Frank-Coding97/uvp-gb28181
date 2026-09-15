import { mount } from "@vue/test-utils";
import { nextTick } from "vue";
import { describe, expect, it } from "vitest";
import RecordTimeline from "./RecordTimeline.vue";
import timelineSource from "./RecordTimeline.vue?raw";

const range = {
    startTime: "2026-08-02T08:00:00+08:00",
    endTime: "2026-08-02T12:00:00+08:00"
};

const records = [{
    recordKey: "record-a",
    startTime: "2026-08-02T09:00:00+08:00",
    endTime: "2026-08-02T10:00:00+08:00",
    type: "time"
}];

describe("RecordTimeline", () => {
    it("renders labeled ticks and zoom controls", async () => {
        const wrapper = mount(RecordTimeline, { props: { range, records, selectedRecordKey: "record-a", currentTime: null } });
        expect(wrapper.text()).toContain("08:30");
        expect(wrapper.text()).toContain("09:30");
        expect(wrapper.get('[data-testid="timeline-zoom-value"]').text()).toBe("4x");
        expect(wrapper.find('[data-testid="timeline-playhead"]').exists()).toBe(true);
        expect(wrapper.get('[data-testid="timeline-range"]').text()).toContain("当前录像 · 2026-08-02 09:00:00 - 2026-08-02 10:00:00");
        await wrapper.get('[data-testid="timeline-zoom-in"]').trigger("click");
        expect(wrapper.get('[data-testid="timeline-zoom-value"]').text()).toBe("8x");
        expect(wrapper.text()).toContain("09:00");
    });

    it("locates the exact clicked time inside a record segment", async () => {
        const wrapper = mount(RecordTimeline, { props: { range, records, selectedRecordKey: null, currentTime: null } });
        const surface = wrapper.get('[data-testid="timeline-surface"]').element as HTMLElement;
        surface.getBoundingClientRect = () => ({ x: 0, y: 0, left: 0, top: 0, right: 400, bottom: 80, width: 400, height: 80, toJSON: () => ({}) });
        await wrapper.get('[data-testid="timeline-segment-record-a"]').trigger("click", { clientX: 150 });
        expect(wrapper.emitted("select")?.[0]).toEqual(["record-a"]);
        expect(wrapper.emitted("locate")?.[0]).toEqual([{ recordKey: "record-a", time: "2026-08-02T09:52:30.000+08:00" }]);
    });

    it("drags a recording segment under the fixed playhead and locates only when released", async () => {
        const wrapper = mount(RecordTimeline, { props: { range, records, selectedRecordKey: "record-a", currentTime: null } });
        const surface = wrapper.get('[data-testid="timeline-surface"]').element as HTMLElement;
        surface.getBoundingClientRect = () => ({ x: 0, y: 0, left: 0, top: 0, right: 400, bottom: 110, width: 400, height: 110, toJSON: () => ({}) });
        await wrapper.get('[data-testid="timeline-segment-record-a"]').trigger("pointerdown", { button: 0, clientX: 200 });
        window.dispatchEvent(new MouseEvent("pointermove", { clientX: 100 }));
        await nextTick();
        expect(wrapper.get('[data-testid="timeline-surface"]').classes()).toContain("dragging");
        expect(wrapper.get('[data-testid="timeline-playhead"]').text()).toContain("09:15:00");
        expect(wrapper.emitted("locate")).toBeUndefined();

        window.dispatchEvent(new MouseEvent("pointerup", { clientX: 100 }));
        await nextTick();
        expect(wrapper.get('[data-testid="timeline-surface"]').classes()).not.toContain("dragging");
        expect(wrapper.emitted("locate")).toHaveLength(1);
        expect(wrapper.emitted("locate")?.at(-1)).toEqual([{ recordKey: "record-a", time: "2026-08-02T09:15:00.000+08:00" }]);
    });

    it("allows the fixed playhead itself to start a drag", async () => {
        const wrapper = mount(RecordTimeline, { props: { range, records, selectedRecordKey: "record-a", currentTime: null } });
        const surface = wrapper.get('[data-testid="timeline-surface"]').element as HTMLElement;
        surface.getBoundingClientRect = () => ({ x: 0, y: 0, left: 0, top: 0, right: 400, bottom: 110, width: 400, height: 110, toJSON: () => ({}) });
        expect(timelineSource).toContain(".timeline-playhead {");
        expect(timelineSource).toContain("pointer-events: auto;");

        await wrapper.get('[data-testid="timeline-playhead"]').trigger("pointerdown", { button: 0, clientX: 200 });
        window.dispatchEvent(new MouseEvent("pointermove", { clientX: 100 }));
        window.dispatchEvent(new MouseEvent("pointerup", { clientX: 100 }));
        await nextTick();

        expect(wrapper.emitted("locate")?.at(-1)).toEqual([{ recordKey: "record-a", time: "2026-08-02T09:15:00.000+08:00" }]);
    });
});
