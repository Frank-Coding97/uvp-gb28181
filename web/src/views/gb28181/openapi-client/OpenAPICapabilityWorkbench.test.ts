import { mount } from "@vue/test-utils";
import { describe, expect, it } from "vitest";
import OpenAPICapabilityWorkbench from "./OpenAPICapabilityWorkbench.vue";

const groups = [
  {
    code: "device-management",
    name: "设备管理",
    capabilities: [
      {
        scope: "device:list",
        name: "设备列表",
        method: "GET",
        externalPath: "/openapi/v1/devices",
        resourceType: "device",
        risk: "read",
        idempotencyRequired: false
      }
    ]
  },
  {
    code: "device-control",
    name: "设备控制",
    capabilities: [
      {
        scope: "ptz:preset:call",
        name: "调用预置位",
        method: "POST",
        externalPath: "/openapi/v1/devices/{deviceId}/channels/{channelId}/ptz/presets/{presetId}/call",
        resourceType: "channel",
        risk: "control",
        idempotencyRequired: true
      }
    ]
  }
];

function mountWorkbench() {
  return mount(OpenAPICapabilityWorkbench, {
    props: {
      groups,
      enabledScopes: ["device:list"],
      editable: true,
      saving: false
    },
    global: {
      stubs: {
        "s-layout-search": { template: "<div><slot name='fields'/><slot name='actions'/><slot name='extra'/></div>" },
        "a-input": { template: "<input />" },
        "a-select": { template: "<select><slot /></select>" },
        "a-option": { template: "<option><slot /></option>" },
        "a-menu": { template: "<nav><slot /></nav>" },
        "a-menu-item": { template: "<button><slot /></button>" },
        "a-card": { template: "<section><slot name='extra'/><slot /></section>" },
        "a-table": { template: "<div><slot name='columns'/><slot name='empty'/></div>" },
        "a-table-column": { template: "<div />" },
        "a-alert": { template: "<div><slot /></div>" },
        "a-button": { template: "<button :disabled='disabled' @click='$emit(`click`)'><slot /></button>", props: ["disabled"] },
        "a-checkbox": { template: "<button @click='$emit(`change`, !modelValue)'><slot /></button>", props: ["modelValue"] },
        "a-switch": { template: "<button @click='$emit(`change`, !modelValue)'>switch</button>", props: ["modelValue"] },
        "a-tag": { template: "<span><slot /></span>" },
        "a-empty": { template: "<span><slot />{{ description }}</span>", props: ["description"] }
      }
    }
  });
}

describe("OpenAPI capability workbench", () => {
  it("tracks additions and removals before emitting one atomic scope set", async () => {
    const wrapper = mountWorkbench();
    const vm = wrapper.vm as any;

    vm.toggleScope("ptz:preset:call", true);
    await wrapper.vm.$nextTick();

    expect(vm.addedScopes).toEqual(["ptz:preset:call"]);
    expect(vm.removedScopes).toEqual([]);
    vm.save();
    expect(wrapper.emitted("save")?.[0]).toEqual([["device:list", "ptz:preset:call"]]);
  });

  it("supports category batch selection without hiding the atomic scopes", async () => {
    const wrapper = mountWorkbench();
    const vm = wrapper.vm as any;

    vm.toggleGroup("device-management", false);
    await wrapper.vm.$nextTick();

    expect(vm.removedScopes).toEqual(["device:list"]);
    expect(vm.filteredCapabilities.map((item: { scope: string }) => item.scope)).toEqual(["device:list", "ptz:preset:call"]);
  });
});
