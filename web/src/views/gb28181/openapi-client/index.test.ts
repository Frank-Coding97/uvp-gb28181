import { enableAutoUnmount, flushPromises, mount } from "@vue/test-utils";
import { Message, Modal, Table, TableColumn } from "@arco-design/web-vue";
import { defineComponent, h, KeepAlive, nextTick, reactive } from "vue";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import OpenAPIClientPage from "./index.vue";
import OpenAPIClientCreateDialog from "./OpenAPIClientCreateDialog.vue";

const userState = vi.hoisted(() => ({ account: { id: 7, permissions: [] as string[] } }));
const userStore = reactive(userState);
enableAutoUnmount(afterEach);
const api = vi.hoisted(() => ({
  list: vi.fn(),
  create: vi.fn(),
  rotate: vi.fn(),
  enable: vi.fn(),
  disable: vi.fn(),
  revoke: vi.fn()
}));
const routerState = vi.hoisted(() => ({ push: vi.fn() }));

vi.mock("@/store/modules/user", () => ({ useUserStoreHook: () => userStore }));
vi.mock("vue-router", async importOriginal => ({
  ...(await importOriginal<typeof import("vue-router")>()),
  useRouter: () => ({ push: routerState.push })
}));
// ⛔ 必须 spawn 真实模块的导出（`...actual`）：只列函数、不列常量，组件一 import
//    `OPENAPI_CLIENT_DEFAULT_DATA_SCOPE` 就会在 setup 阶段炸（ vitest 报
//    "No ... export is defined on the mock"），而且这种炸法会连坐整个文件的用例。
//    用 importOriginal 兜底，以后 api 模块再加常量也不会再次踩到。
vi.mock("@/api/gb28181-openapi", async importOriginal => {
  const actual = await importOriginal<typeof import("@/api/gb28181-openapi")>();
  return {
    ...actual,
    listOpenAPIClients: api.list,
    createOpenAPIClient: api.create,
    rotateOpenAPIClientSecret: api.rotate,
    enableOpenAPIClient: api.enable,
    disableOpenAPIClient: api.disable,
    revokeOpenAPIClient: api.revoke,
    isOpenAPISuccess: (response: { code?: string }) => response.code === "OK"
  };
});

const client = {
  id: 7,
  ak: "uvp_0123456789abcdef0123456789abcdef",
  name: "现场接入",
  ownerDeptId: 10,
  dataScope: 3,
  responsibleUserId: 7,
  status: "active",
  secretVersion: 1,
  authEpoch: 1,
  rateLimit: 10,
  burst: 20,
  viewerQuota: 10,
  rowVersion: 3,
  createdBy: 7,
  updatedBy: 7,
  createdAt: "2026-09-06T01:00:00Z",
  updatedAt: "2026-09-06T01:00:00Z"
};

function ok<T>(data: T) {
  return { code: "OK", message: "success", requestId: "req-1", data };
}

const pageStubs = {
  "s-layout-search": { template: "<section><slot name='fields'/><slot name='actions'/><slot name='extra'/></section>" },
  "a-input": { template: "<input />" },
  "a-select": { template: "<select><slot /></select>" },
  "a-option": { template: "<option><slot /></option>" },
  "a-button": { template: "<button :disabled='disabled'><slot name='icon'/><slot /></button>", props: ["disabled"] },
  "a-table": { template: "<div data-testid='client-table'><slot name='columns'/><slot name='empty'/></div>" },
  "a-table-column": { template: "<div />" },
  "a-modal": { template: "<div v-if='visible'><slot /></div>", props: ["visible"] },
  "a-form": { template: "<form><slot /></form>" },
  "a-form-item": { template: "<label><slot /></label>" },
  "a-tag": { template: "<span><slot /></span>" },
  "a-empty": { template: "<span>{{ description }}</span>", props: ["description"] },
  "a-alert": { template: "<div role='alert'><slot /></div>" },
  "a-tooltip": { template: "<span><slot /></span>" },
  "a-dropdown": { template: "<span><slot /><slot name='content' /></span>" },
  "a-doption": { template: "<button><slot /></button>" },
  "a-spin": { template: "<div><slot /></div>" },
  "a-checkbox-group": { template: "<div><slot /></div>" },
  "a-checkbox": { template: "<label><slot /></label>", props: ["value"] },
  OpenAPISecretDialog: { template: "<div />", props: ["visible"] },
  Search: true,
  RotateCcw: true,
  RefreshCw: true,
  Eye: true,
  Plus: true,
  KeyRound: true,
  ShieldCheck: true,
  ShieldOff: true,
  Ban: true,
  ScrollText: true
};

function mountPage() {
  return mount(OpenAPIClientPage, {
    global: { stubs: pageStubs }
  });
}

function mountKeepAlivePage() {
  const Host = defineComponent({
    data: () => ({ active: true }),
    render() {
      return h(KeepAlive, null, {
        default: () => (this.active ? h(OpenAPIClientPage) : null)
      });
    }
  });
  return mount(Host, { global: { stubs: pageStubs } });
}

function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (reason?: unknown) => void;
  const promise = new Promise<T>((resolvePromise, rejectPromise) => {
    resolve = resolvePromise;
    reject = rejectPromise;
  });
  return { promise, resolve, reject };
}

describe("OpenAPI client page", () => {
  beforeEach(() => {
    routerState.push.mockReset();
    userStore.account.id = 7;
    userStore.account.permissions = ["*:*:*"];
    api.list
      .mockReset()
      .mockResolvedValue(
        ok({ items: [client], page: 1, pageSize: 10, total: 1, ownerDepartments: [{ id: 10, name: "平台运维部" }] })
      );
    api.create.mockReset().mockResolvedValue(ok({ client, secretKey: "one-time-secret" }));
    api.rotate.mockReset().mockResolvedValue(ok({ client, secretKey: "rotated-secret" }));
    api.enable.mockReset().mockResolvedValue(ok({ client }));
    api.disable.mockReset().mockResolvedValue({ ...ok({ client, revocationStatus: "pending" }), status: 202 });
    api.revoke.mockReset().mockResolvedValue({ ...ok({ client, revocationStatus: "pending" }), status: 202 });
  });

  it("loads scoped ownerDepartments without preloading detail capability APIs", async () => {
    const wrapper = mountPage();
    await flushPromises();
    expect(api.list).toHaveBeenCalledWith({ page: 1, pageSize: 10 });
    expect((wrapper.vm as any).ownerDepartments).toEqual([{ id: 10, name: "平台运维部" }]);
    expect((wrapper.vm as any).clients).toEqual([client]);
  });

  it("renders real Arco table columns, client rows and row actions", async () => {
    const wrapper = mount(OpenAPIClientPage, {
      global: {
        components: { ATable: Table, ATableColumn: TableColumn },
        stubs: { ...pageStubs, "a-table": false, "a-table-column": false }
      }
    });
    await flushPromises();
    expect(wrapper.findAll("th").map(cell => cell.text())).toContain("归属部门与数据范围");
    // ⛔ 防回归：数据范围必须在这列里**看得见**（每个客户端各自配置的，
    //    不是全表统一的），否则操作员无从判断这个 AK 能碰到多少设备。
    //    以前这里写死"不含下级 / 共享设备"，对 dataScope=4 的客户端是错的。
    expect(wrapper.find("tbody").text()).toContain("本部门");
    expect(wrapper.find("tbody").text()).toContain(client.name);
    expect(wrapper.find("tbody").text()).toContain("详情");
    expect(wrapper.find("tbody").text()).toContain("配置能力");
    expect(wrapper.find("tbody").text()).toContain("调用记录");
  });

  it("opens each management task in the dedicated client workspace", async () => {
    const wrapper = mountPage();
    await flushPromises();

    (wrapper.vm as any).openWorkspace(client, "capabilities");
    expect(routerState.push).toHaveBeenLastCalledWith({
      path: `/gb28181/openapi-client/${client.id}`,
      query: { tab: "capabilities" }
    });

    (wrapper.vm as any).openWorkspace(client, "logs");
    expect(routerState.push).toHaveBeenLastCalledWith({ path: `/gb28181/openapi-client/${client.id}`, query: { tab: "logs" } });
  });

  it("does not call read APIs or render management content without read permission", async () => {
    userStore.account.permissions = [];
    const wrapper = mountPage();
    await flushPromises();
    expect(api.list).not.toHaveBeenCalled();
    expect(wrapper.text()).toContain("没有 OpenAPI 客户端查看权限");
  });

  it("keeps one-time secrets in transient page state only and clears them on close", async () => {
    const wrapper = mountPage();
    await flushPromises();
    await (wrapper.vm as any).performCreate({ name: "新客户端", ownerDeptId: 10, responsibleUserId: 7 });
    expect((wrapper.vm as any).secretPayload).toEqual({
      clientId: client.id,
      accessKey: client.ak,
      secretKey: "one-time-secret"
    });
    await (wrapper.vm as any).closeSecret();
    expect((wrapper.vm as any).secretPayload).toBeNull();
    expect(wrapper.text()).not.toContain("one-time-secret");
  });

  it("continues from a newly created secret to capability configuration", async () => {
    const wrapper = mountPage();
    await flushPromises();
    await (wrapper.vm as any).performCreate({ name: "新客户端", ownerDeptId: 10 });

    (wrapper.vm as any).configureCreatedClient();

    expect(routerState.push).toHaveBeenLastCalledWith({
      path: `/gb28181/openapi-client/${client.id}`,
      query: { tab: "capabilities" }
    });
    expect((wrapper.vm as any).secretPayload).toBeNull();
  });

  it("sends the selected client data scope when creating a client", async () => {
    const wrapper = mount(OpenAPIClientCreateDialog, {
      props: {
        visible: true,
        departments: [{ id: 10, name: "平台运维部" }],
        submitting: false,
        error: ""
      },
      global: {
        stubs: {
          "a-modal": { template: "<div><slot /></div>" },
          "a-alert": { template: "<div><slot /></div>" },
          "a-form": { template: "<form><slot /></form>" },
          "a-form-item": { template: "<label><slot /></label>" },
          "a-input": { template: "<input />" },
          "a-tree-select": { template: "<div />" },
          "a-select": { template: "<select><slot /></select>" },
          "a-option": { template: "<option><slot /></option>" }
        }
      }
    });

    const vm = wrapper.vm as any;
    expect(vm.form.dataScope).toBe(3);
    expect(wrapper.text()).toContain("本部门");
    vm.form.name = "下级部门接入";
    vm.form.ownerDeptId = 10;
    vm.form.dataScope = 4;
    await vm.submit();

    expect(wrapper.emitted("create")?.[0]).toEqual([{ name: "下级部门接入", ownerDeptId: 10, dataScope: 4 }]);
    expect(wrapper.text()).toContain("本部门及以下");
  });

  it("ignores a late create response after the modal is closed", async () => {
    const pending = deferred<ReturnType<typeof ok>>();
    api.create.mockReturnValueOnce(pending.promise);
    const success = vi.spyOn(Message, "success").mockImplementation(() => undefined as any);
    const wrapper = mountPage();
    await flushPromises();
    const createTask = (wrapper.vm as any).performCreate({ name: "新客户端", ownerDeptId: 10 });
    await flushPromises();
    await (wrapper.vm as any).closeCreate();
    pending.resolve(ok({ client, secretKey: "late-secret" }));
    await createTask;
    await flushPromises();
    expect((wrapper.vm as any).secretPayload).toBeNull();
    expect(success).not.toHaveBeenCalled();
    success.mockRestore();
  });

  it("ignores a late rotate response after unmount and does not show SK", async () => {
    const pending = deferred<ReturnType<typeof ok>>();
    api.rotate.mockReturnValueOnce(pending.promise);
    const success = vi.spyOn(Message, "success").mockImplementation(() => undefined as any);
    const wrapper = mountPage();
    await flushPromises();
    const rotateTask = (wrapper.vm as any).performRotate(client);
    await flushPromises();
    wrapper.unmount();
    pending.resolve(ok({ client, secretKey: "late-rotate-secret" }));
    await rotateTask;
    await flushPromises();
    expect((wrapper.vm as any).secretPayload).toBeNull();
    expect(success).not.toHaveBeenCalled();
    success.mockRestore();
  });

  it("updates the list row after a status change without opening extra detail UI", async () => {
    const disabledClient = { ...client, status: "disabled" as const, rowVersion: 4 };
    api.disable.mockResolvedValueOnce({ ...ok({ client: disabledClient, revocationStatus: "pending" }), status: 202 });
    const wrapper = mountPage();
    await flushPromises();
    (wrapper.vm as any).openCreate();
    await (wrapper.vm as any).closeCreate();
    await (wrapper.vm as any).performStatus("disable", client);
    await flushPromises();
    expect((wrapper.vm as any).createVisible).toBe(false);
    expect((wrapper.vm as any).clients).toEqual([disabledClient]);
  });

  it("accepts the enabled client wrapper returned by the status HTTP API", async () => {
    const wrapper = mountPage();
    const vm = wrapper.vm as any;
    await flushPromises();
    await vm.performStatus("enable", { ...client, status: "disabled" });
    expect(vm.clients).toEqual([client]);
  });

  it("refreshes the list after a rotate conflict", async () => {
    const conflict = Object.assign(new Error("conflict"), { response: { status: 409, data: { message: "conflict" } } });
    api.rotate.mockRejectedValueOnce(conflict);
    const wrapper = mountPage();
    await flushPromises();
    await (wrapper.vm as any).performRotate(client);
    await flushPromises();
    expect(api.list).toHaveBeenCalledTimes(2);
    expect((wrapper.vm as any).createVisible).toBe(false);
  });

  it("refreshes the list after a status conflict", async () => {
    const conflict = Object.assign(new Error("conflict"), { response: { status: 409, data: { message: "conflict" } } });
    api.disable.mockRejectedValueOnce(conflict);
    const wrapper = mountPage();
    await flushPromises();
    await (wrapper.vm as any).performStatus("disable", client);
    await flushPromises();
    expect(api.list).toHaveBeenCalledTimes(2);
    expect((wrapper.vm as any).createVisible).toBe(false);
  });

  it("clears SK and drops late mutations when a KeepAlive page is deactivated", async () => {
    const success = vi.spyOn(Message, "success").mockImplementation(() => undefined as any);
    const wrapper = mountKeepAlivePage();
    await flushPromises();
    const page = wrapper.findComponent(OpenAPIClientPage);
    const vm = page.vm as any;
    await vm.performRotate(client);
    await flushPromises();
    expect(vm.secretPayload).toEqual({ clientId: client.id, accessKey: client.ak, secretKey: "rotated-secret" });

    const pending = deferred<ReturnType<typeof ok>>();
    api.rotate.mockReturnValueOnce(pending.promise);
    success.mockClear();
    const rotateTask = vm.performRotate(client);
    await flushPromises();
    await wrapper.setData({ active: false });
    await nextTick();
    expect(vm.secretPayload).toBeNull();
    pending.resolve(ok({ client, secretKey: "late-deactivated-secret" }));
    await rotateTask;
    await flushPromises();
    expect(vm.secretPayload).toBeNull();
    expect(success).not.toHaveBeenCalled();

    const listCalls = api.list.mock.calls.length;
    await wrapper.setData({ active: true });
    await flushPromises();
    expect(api.list.mock.calls.length).toBeGreaterThan(listCalls);
    success.mockRestore();
  });

  it("requires confirmation before rotating an active SK", async () => {
    const confirm = vi.spyOn(Modal, "confirm").mockImplementation(() => ({ close: vi.fn() }) as any);
    const wrapper = mountPage();
    await flushPromises();
    await (wrapper.vm as any).requestRotate(client);
    expect(confirm).toHaveBeenCalledOnce();
    expect(api.rotate).not.toHaveBeenCalled();
    confirm.mockRestore();
  });

  it("guards every mutation behind its matching UI permission", async () => {
    const cases = [
      {
        permission: "gb28181:openapi:client:create",
        api: api.create,
        invoke: (vm: any) => vm.performCreate({ name: "新客户端", ownerDeptId: 10 })
      },
      { permission: "gb28181:openapi:client:rotate", api: api.rotate, invoke: (vm: any) => vm.performRotate(client) },
      { permission: "gb28181:openapi:client:status", api: api.disable, invoke: (vm: any) => vm.performStatus("disable", client) }
    ];
    for (const item of cases) {
      userStore.account.permissions = ["gb28181:openapi:client:read"];
      const wrapper = mountPage();
      await flushPromises();
      item.api.mockClear();
      await item.invoke(wrapper.vm as any);
      expect(item.api, item.permission).not.toHaveBeenCalled();
      wrapper.unmount();
    }
  });

  it("clears cached management data and SK immediately when read permission is lost", async () => {
    const wrapper = mountPage();
    const vm = wrapper.vm as any;
    await flushPromises();
    await vm.performRotate(client);
    expect(vm.secretPayload).not.toBeNull();
    userStore.account.permissions = [];
    await nextTick();
    expect(vm.secretPayload).toBeNull();
    expect(vm.clients).toEqual([]);
    expect(vm.ownerDepartments).toEqual([]);
    expect(vm.pagination.total).toBe(0);
    expect(vm.createVisible).toBe(false);
  });

  it("does not accept an in-flight secret after losing only rotate permission", async () => {
    const wrapper = mountPage();
    const vm = wrapper.vm as any;
    await flushPromises();
    const pending = deferred<ReturnType<typeof ok>>();
    api.rotate.mockReturnValueOnce(pending.promise);
    const operation = vm.performRotate(client);
    userStore.account.permissions = ["gb28181:openapi:client:read"];
    pending.resolve(ok({ client, secretKey: "revoked-permission-secret" }));
    await operation;
    await flushPromises();
    expect(vm.secretPayload).toBeNull();
  });

  it("keeps the one-time SK when an unchanged permission profile is refreshed", async () => {
    const wrapper = mountPage();
    const vm = wrapper.vm as any;
    await flushPromises();
    await vm.performRotate(client);
    userStore.account.permissions = [...userStore.account.permissions];
    await nextTick();
    expect(vm.secretPayload).toEqual({ clientId: client.id, accessKey: client.ak, secretKey: "rotated-secret" });
  });

  it.each(["list", "create", "rotate", "disable"])("invalidates cached access on a %s HTTP denial", async endpoint => {
    const wrapper = mountPage();
    const vm = wrapper.vm as any;
    await flushPromises();
    api[endpoint as keyof typeof api].mockRejectedValueOnce(Object.assign(new Error("denied"), { response: { status: 403 } }));
    if (endpoint === "list") await vm.load();
    else if (endpoint === "create") await vm.performCreate({ name: "接入", ownerDeptId: 10 });
    else if (endpoint === "rotate") await vm.performRotate(client);
    else if (endpoint === "disable") await vm.performStatus("disable", client);
    await flushPromises();
    expect(vm.clients).toEqual([]);
    expect(vm.secretPayload).toBeNull();
    expect(vm.error).toContain("访问");
  });

  it("invalidates cached data and pending list responses on account switch with identical permissions", async () => {
    const wrapper = mountPage();
    const vm = wrapper.vm as any;
    await flushPromises();
    const old = deferred<ReturnType<typeof ok>>();
    api.list.mockReturnValueOnce(old.promise);
    const oldLoad = vm.load();
    api.list.mockResolvedValueOnce(ok({ items: [], ownerDepartments: [], total: 0 }));
    userStore.account.id = 8;
    await flushPromises();
    old.resolve(ok({ items: [client], ownerDepartments: [{ id: 10, name: "旧范围" }], total: 1 }));
    await oldLoad;
    expect(vm.clients).toEqual([]);
    expect(vm.ownerDepartments).toEqual([]);
    expect(vm.pagination.total).toBe(0);
  });

  it("does not execute an old confirmation after the page is deactivated and reactivated", async () => {
    let confirmOptions: any;
    const confirm = vi.spyOn(Modal, "confirm").mockImplementation(options => {
      confirmOptions = options;
      return { close: vi.fn() } as any;
    });
    const wrapper = mountKeepAlivePage();
    await flushPromises();
    const vm = wrapper.findComponent(OpenAPIClientPage).vm as any;
    vm.requestRotate(client);
    await wrapper.setData({ active: false });
    await wrapper.setData({ active: true });
    await flushPromises();
    await confirmOptions.onOk();
    expect(api.rotate).not.toHaveBeenCalled();
    confirm.mockRestore();
  });
});
