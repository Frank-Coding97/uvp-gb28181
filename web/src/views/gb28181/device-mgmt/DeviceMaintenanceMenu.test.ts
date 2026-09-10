import { mount } from "@vue/test-utils";
import { describe, expect, it } from "vitest";
import DeviceMaintenanceMenu from "./DeviceMaintenanceMenu.vue";

function menu(canUpgrade = true, canReboot = true) {
    return mount(DeviceMaintenanceMenu, {
        props: { canUpgrade, canReboot },
        global: { stubs: { 'a-doption': { props: ['disabled'], emits: ['click'], template: '<button :disabled="disabled" @click="$emit(\'click\')"><slot /></button>' } } }
    });
}
describe('device maintenance menu', () => {
    it('exposes three named actions in a consistent order without posting', async () => {
        const wrapper = menu();
        expect(wrapper.findAll('button').map(button => button.text())).toEqual(['固件升级', '维护记录', '重启设备']);
        await wrapper.findAll('button')[0].trigger('click');
        await wrapper.findAll('button')[1].trigger('click');
        await wrapper.findAll('button')[2].trigger('click');
        expect(wrapper.emitted('upgrade')).toHaveLength(1);
        expect(wrapper.emitted('records')).toHaveLength(1);
        expect(wrapper.emitted('reboot')).toHaveLength(1);
    });
    it('keeps records available without write permissions', () => {
        const buttons = menu(false, false).findAll('button');
        expect(buttons[0].attributes('disabled')).toBeDefined();
        expect(buttons[1].attributes('disabled')).toBeUndefined();
        expect(buttons[2].attributes('disabled')).toBeDefined();
    });
});
