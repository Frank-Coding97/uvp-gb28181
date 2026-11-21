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
        description: "查询授权范围内的设备。",
        method: "GET",
        externalPath: "/openapi/v1/devices",
        resourceType: "device",
        risk: "read",
        protocolVersions: [],
        standardRefs: [],
        requiresConfirmation: false,
        idempotencyMode: "none",
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
        description: "让设备转到已配置的预置位。",
        method: "POST",
        externalPath: "/openapi/v1/devices/{deviceId}/channels/{channelId}/ptz/presets/{presetId}/call",
        resourceType: "channel",
        risk: "control",
        protocolVersions: ["2022"],
        standardRefs: ["GB/T 28181-2022 A.2.3.1.2"],
        requiresConfirmation: false,
        idempotencyMode: "required",
        idempotencyRequired: true
      }
    ]
  },
  {
    code: "maintenance",
    name: "设备维护",
    capabilities: [
      {
        scope: "storage:format",
        name: "格式化存储卡",
        description: "清除设备存储卡数据。",
        method: "POST",
        externalPath: "/openapi/v1/devices/{deviceId}/storage-cards/{cardId}/format",
        resourceType: "device",
        risk: "destructive",
        protocolVersions: ["2022"],
        standardRefs: ["GB/T 28181-2022 A.2.3.1.5"],
        requiresConfirmation: true,
        idempotencyMode: "required",
        idempotencyRequired: true
      },
      {
        scope: "custom:future",
        name: "未来操作",
        description: "由未知风险定义的操作。",
        method: "POST",
        externalPath: "/openapi/v1/future",
        resourceType: "device",
        risk: "future-risk",
        protocolVersions: [],
        standardRefs: [],
        requiresConfirmation: false,
        idempotencyMode: "none",
        idempotencyRequired: false
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
    expect(vm.filteredCapabilities.map((item: { scope: string }) => item.scope)).toEqual([
      "device:list",
      "ptz:preset:call",
      "storage:format",
      "custom:future"
    ]);
  });

  it("shows release-provided protocol and confirmation metadata and fails closed on unknown risk labels", () => {
    const wrapper = mountWorkbench();
    const vm = wrapper.vm as any;
    const format = groups[2].capabilities[0] as any;

    expect(vm.capabilityDescription(format)).toBe("清除设备存储卡数据。");
    expect(vm.protocolLabel(format)).toBe("GB/T 28181-2022");
    expect(vm.idempotencyLabel(format)).toBe("要求 Idempotency-Key");
    expect(vm.confirmationLabel(format)).toBe("调用时必须提交 confirmed=true");
    expect(vm.riskLabel(format.risk)).toBe("破坏性");
    expect(vm.riskColor(format.risk)).toBe("red");
    expect(vm.riskLabel("future-risk")).toBe("风险未知");
    expect(vm.riskColor("future-risk")).toBe("gray");
    expect(vm.idempotencyLabel({ ...format, idempotencyMode: "future-mode" })).toBe("幂等策略未知");
  });
});
