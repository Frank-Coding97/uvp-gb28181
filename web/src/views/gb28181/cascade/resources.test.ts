import { flushPromises, mount } from "@vue/test-utils";
import { reactive } from "vue";
import { beforeEach, describe, expect, it, vi } from "vitest";

const mocks = vi.hoisted(() => ({
  get: vi.fn(),
  push: vi.fn(),
  permission: vi.fn(),
  leave: vi.fn(),
  update: vi.fn(),
  exit: vi.fn()
}));
let route = reactive({ params: { id: "7" } });
vi.mock("vue-router", () => ({
  useRoute: () => route,
  useRouter: () => ({ push: mocks.push }),
  onBeforeRouteLeave: mocks.leave,
  onBeforeRouteUpdate: mocks.update
}));
vi.mock("@/api/gb28181", () => ({ getCascadePlatform: mocks.get }));
vi.mock("@/hooks/usePermission", () => ({ hasPermission: mocks.permission }));
vi.mock("./BusinessGroupManager.vue", () => ({
  default: { name: "BusinessGroupManager", template: "<div />" }
}));
import Resources from "./resources.vue";

const manager = {
  name: "BusinessGroupManager",
  props: ["platformId", "platformName", "canWrite", "canManage"],
  emits: ["update:visible", "publishGroupUpdated"],
  setup: () => ({ requestExit: mocks.exit }),
  template: "<div>resource-workbench</div>"
};
const render = () =>
  mount(Resources, {
    global: {
      stubs: {
        BusinessGroupManager: manager,
        "a-spin": { template: "<div><slot /></div>" },
        "a-alert": { template: "<div><slot /></div>" }
      }
    }
  });

describe("cascade resource page", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    route = reactive({ params: { id: "7" } });
    mocks.permission.mockReturnValue(true);
    mocks.get.mockResolvedValue({ id: 7, name: "上级 A", localDeviceId: "34020000002000000001" });
    mocks.exit.mockResolvedValue(true);
  });

  it("loads the platform and forwards existing sharing and management permissions", async () => {
    mocks.permission.mockImplementation((key: string) => key !== "gb28181:cascade:share");
    const wrapper = render();
    await flushPromises();
    expect(mocks.get).toHaveBeenCalledWith(7);
    expect(wrapper.find(".cascade-resources-header").exists()).toBe(false);
    const child = wrapper.findComponent({ name: "BusinessGroupManager" });
    expect(child.props()).toMatchObject({ platformId: 7, platformName: "上级 A", canWrite: false, canManage: true });
    wrapper.unmount();
  });

  it("does not fetch or mount resources without view permission", async () => {
    mocks.permission.mockReturnValue(false);
    const wrapper = render();
    await flushPromises();
    expect(mocks.get).not.toHaveBeenCalled();
    expect(wrapper.text()).toContain("无权查看");
    expect(wrapper.findComponent({ name: "BusinessGroupManager" }).exists()).toBe(false);
    wrapper.unmount();
  });

  it("rejects invalid route IDs without an API request", async () => {
    route.params.id = "invalid";
    const wrapper = render();
    await flushPromises();
    expect(mocks.get).not.toHaveBeenCalled();
    expect(wrapper.text()).toContain("平台编号无效");
    wrapper.unmount();
  });

  it("delegates leave and same-page platform changes to draft protection", async () => {
    const wrapper = render();
    await flushPromises();
    mocks.exit.mockResolvedValue(false);
    expect(await mocks.leave.mock.calls[0][0]()).toBe(false);
    expect(await mocks.update.mock.calls[0][0]()).toBe(false);
    mocks.exit.mockResolvedValue(true);
    expect(await mocks.leave.mock.calls[0][0]()).toBe(true);
    expect(mocks.exit).toHaveBeenCalledTimes(3);
    wrapper.unmount();
  });

  it("ignores a previous platform response after the route changes", async () => {
    let finish!: (value: unknown) => void;
    mocks.get.mockImplementationOnce(
      () =>
        new Promise(resolve => {
          finish = resolve;
        })
    );
    const wrapper = render();
    route.params.id = "8";
    mocks.get.mockResolvedValue({ id: 8, name: "上级 B", localDeviceId: "34020000002000000002" });
    await flushPromises();
    finish({ id: 7, name: "上级 A" });
    await flushPromises();
    expect(wrapper.findComponent({ name: "BusinessGroupManager" }).props("platformName")).toBe("上级 B");
    wrapper.unmount();
  });
});
