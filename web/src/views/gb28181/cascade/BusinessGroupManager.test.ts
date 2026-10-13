import { flushPromises, mount } from "@vue/test-utils";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { CascadeBusinessGroup } from "@/api/gb28181";

const api = vi.hoisted(() => ({ get: vi.fn(), replace: vi.fn() }));
vi.mock("@/api/gb28181", () => ({
  getCascadeBusinessGroup: api.get,
  replaceCascadeBusinessGroup: api.replace
}));

import BusinessGroupManager from "./BusinessGroupManager.vue";

const root = {
  code: "34020000002150000001",
  name: "业务根",
  kind: "group" as const,
  parentCode: "",
  sort: 0
};
const baseData = (platformId = 7): CascadeBusinessGroup => ({
  platformId,
  revision: 4,
  nodes: [root],
  bindings: [],
  channels: [
    {
      sourceChannelId: 31,
      sourceDeviceId: 3,
      publishedChannelId: "34020000001320000031",
      name: "已共享通道",
      parentOverride: "",
      ptzAllowed: false,
      active: true
    }
  ]
});

function mountManager(props: Record<string, unknown> = {}) {
  return mount(BusinessGroupManager, {
    props: {
      visible: true,
      platformId: 7,
      platformName: "上级 A",
      localDeviceId: "34020000002000000001",
      canWrite: true,
      ...props
    },
    global: {
      stubs: {
        "a-modal": { template: "<div><slot /><slot name='footer' /></div>" },
        "a-spin": { template: "<div><slot /></div>" },
        "a-alert": { template: "<div><slot /></div>" },
        "a-empty": { template: "<div><slot /></div>" },
        "a-select": {
          props: ["modelValue", "disabled"],
          emits: ["update:modelValue"],
          template: `<select :value="modelValue" :disabled="disabled" :aria-label="$attrs['aria-label']" @change="$emit('update:modelValue', $event.target.value)"><slot /></select>`
        },
        "a-option": { props: ["value"], template: "<option :value='value'><slot /></option>" }
      }
    }
  });
}

describe("cascade business-group manager", () => {
  beforeEach(() => {
    api.get.mockReset();
    api.replace.mockReset();
    api.get.mockResolvedValue(baseData());
    api.replace.mockImplementation(async (_id: number, request: any) => ({
      ...baseData(),
      revision: request.expectedProjectionRevision + 1,
      nodes: request.nodes,
      bindings: request.bindings
    }));
  });

  it("shows an explicit empty state and generates a 20-digit root only after the user asks", async () => {
    api.get.mockResolvedValue({ platformId: 7, revision: 0, nodes: [], bindings: [], channels: [] });
    const wrapper = mountManager();
    await flushPromises();

    expect(wrapper.text()).toContain("尚未配置业务分组");
    expect(wrapper.find("[data-action='create-root']").exists()).toBe(true);
    await wrapper.get("[data-action='create-root']").trigger("click");
    const code = (wrapper.get("[aria-label='节点国标编号']").element as HTMLInputElement).value;
    expect(code).toMatch(/^\d{20}$/);
    expect(code.slice(10, 13)).toBe("215");
    expect(wrapper.text()).toContain("固定 20 位");
    wrapper.unmount();
  });

  it("blocks writes in view-only mode while still showing the current platform tree", async () => {
    const wrapper = mountManager({ canWrite: false });
    await flushPromises();
    expect(wrapper.text()).toContain("业务根");
    expect(wrapper.get("[data-action='add-organization']").attributes("disabled")).toBeDefined();
    expect(wrapper.get("[data-action='save']").attributes("disabled")).toBeDefined();
    wrapper.unmount();
  });

  it("moves a shared channel to one node and never offers unshared resources", async () => {
    const data = baseData();
    data.nodes = [
      root,
      { ...root, code: "34020000002160000002", name: "运营一组", kind: "organization", parentCode: root.code, sort: 0 }
    ];
    data.bindings = [{ sourceChannelId: 31, nodeCode: data.nodes[1].code }];
    api.get.mockResolvedValue(data);
    const wrapper = mountManager();
    await flushPromises();

    await wrapper.get(`[data-code='${data.nodes[1].code}']`).trigger("click");
    const targets = wrapper.get("[aria-label='业务归属节点']").findAll("option");
    expect(targets.map(option => option.text())).toEqual(["运营一组"]);
    expect(wrapper.text()).toContain("已共享通道");
    expect(wrapper.get("[aria-label='已共享通道']").text()).toContain("已共享通道");
    await wrapper.get("[data-code='34020000002160000002']").trigger("click");
    await wrapper.get("[aria-label='已共享通道']").setValue("31");
    await wrapper.get("[aria-label='业务归属节点']").setValue("34020000002160000002");
    await wrapper.get("[data-action='add-binding']").trigger("click");
    expect(wrapper.findAll("[data-action='remove-binding']")).toHaveLength(1);
    await wrapper.get("[data-action='remove-binding']").trigger("click");
    expect(wrapper.text()).toContain("已共享通道");
    expect(wrapper.text()).toContain("仅移除业务挂载，不会取消共享");
    wrapper.unmount();
  });

  it("rejects deleting a populated node and excludes descendants from parent choices", async () => {
    const data = baseData();
    const organization = {
      ...root,
      code: "34020000002160000002",
      name: "运营一组",
      kind: "organization" as const,
      parentCode: root.code,
      sort: 0
    };
    data.nodes = [
      root,
      organization,
      { ...organization, code: "34020000002160000003", name: "摄像机组", parentCode: organization.code }
    ];
    api.get.mockResolvedValue(data);
    const wrapper = mountManager();
    await flushPromises();

    await wrapper.get(`[data-code='${organization.code}']`).trigger("click");
    expect(wrapper.get("[data-action='delete-node']").attributes("disabled")).toBeDefined();
    const parentOptions = wrapper
      .get("[aria-label='父节点']")
      .findAll("option")
      .map(option => option.element.value);
    expect(parentOptions).not.toContain(organization.code);
    expect(parentOptions).not.toContain("34020000002160000003");
    wrapper.unmount();
  });

  it("keeps edits when the server reports a revision conflict", async () => {
    api.replace.mockRejectedValue(new Error("资源状态冲突"));
    const wrapper = mountManager();
    await flushPromises();
    await wrapper.get("[aria-label='节点名称']").setValue("本地草稿");
    await wrapper.get("[data-action='save']").trigger("click");
    await flushPromises();

    expect(wrapper.text()).toContain("数据已被其他操作修改");
    expect((wrapper.get("[aria-label='节点名称']").element as HTMLInputElement).value).toBe("本地草稿");
    wrapper.unmount();
  });

  it("does not let a late response for one platform replace the currently selected platform", async () => {
    let resolveA!: (value: unknown) => void;
    api.get.mockImplementation((id: number) =>
      id === 7
        ? new Promise(resolve => (resolveA = resolve))
        : Promise.resolve({ ...baseData(8), nodes: [{ ...root, code: "34020000002150000008", name: "平台 B 根" }] })
    );
    const wrapper = mountManager();
    await flushPromises();
    await wrapper.setProps({ platformId: 8, platformName: "上级 B", localDeviceId: "34020000002000000008" });
    await flushPromises();
    resolveA(baseData(7));
    await flushPromises();

    expect(wrapper.text()).toContain("平台 B 根");
    expect((wrapper.get("[aria-label='节点国标编号']").element as HTMLInputElement).value).toBe("34020000002150000008");
    wrapper.unmount();
  });

  it("reports load failures with a retry action", async () => {
    api.get.mockRejectedValueOnce(new Error("offline"));
    const wrapper = mountManager();
    await flushPromises();
    expect(wrapper.text()).toContain("业务分组读取失败");
    await wrapper.get("[data-action='retry-load']").trigger("click");
    await flushPromises();
    expect(wrapper.text()).toContain("业务根");
    expect(api.get).toHaveBeenCalledTimes(2);
    wrapper.unmount();
  });

  it("prefers the backend message over the generic HTTP error when loading fails", async () => {
    api.get.mockRejectedValueOnce(
      Object.assign(new Error("Request failed with status code 403"), {
        response: { status: 403, data: { message: "无权查看该上级平台的业务分组" } }
      })
    );
    const wrapper = mountManager();
    await flushPromises();

    expect(wrapper.text()).toContain("业务分组读取失败：无权查看该上级平台的业务分组");
    expect(wrapper.text()).not.toContain("Request failed with status code 403");
    wrapper.unmount();
  });

  it("prefers the backend message over the generic HTTP error when saving fails", async () => {
    api.replace.mockRejectedValueOnce(
      Object.assign(new Error("Request failed with status code 400"), {
        response: { status: 400, data: { message: "业务组织父节点必须是业务根或虚拟组织" } }
      })
    );
    const wrapper = mountManager();
    await flushPromises();
    await wrapper.get("[data-action='save']").trigger("click");
    await flushPromises();

    expect(wrapper.text()).toContain("业务组织父节点必须是业务根或虚拟组织");
    expect(wrapper.text()).not.toContain("Request failed with status code 400");
    wrapper.unmount();
  });
});
