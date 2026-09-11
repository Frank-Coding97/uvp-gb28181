import { mount } from "@vue/test-utils";
import { describe, expect, it } from "vitest";
import DeviceRecordQueryDemo from "./DeviceRecordQueryDemo.vue";

function mountDemo() {
    return mount(DeviceRecordQueryDemo, {
        global: {
            stubs: {
                DeviceRecordQueryDrawer: {
                    props: ["visible", "channel"],
                    template: "<div data-testid='drawer-stub' :data-visible='String(visible)' :data-channel-id='channel?.id || ``' />"
                },
                "a-tooltip": { template: "<span><slot /></span>" }
            }
        }
    });
}

describe("DeviceRecordQueryDemo", () => {
    it("opens the selected channel from the list entry", async () => {
        const wrapper = mountDemo();
        await wrapper.get('[data-testid="record-query-list-entry-31"]').trigger("click");
        const drawer = wrapper.get('[data-testid="drawer-stub"]');
        expect(drawer.attributes("data-visible")).toBe("true");
        expect(drawer.attributes("data-channel-id")).toBe("31");
    });

    it("shows the card entry and opens the same drawer contract", async () => {
        const wrapper = mountDemo();
        await wrapper.get('[data-testid="record-query-view-card"]').trigger("click");
        await wrapper.get('[data-testid="record-query-card-entry-32"]').trigger("click");
        const drawer = wrapper.get('[data-testid="drawer-stub"]');
        expect(drawer.attributes("data-visible")).toBe("true");
        expect(drawer.attributes("data-channel-id")).toBe("32");
    });
});
