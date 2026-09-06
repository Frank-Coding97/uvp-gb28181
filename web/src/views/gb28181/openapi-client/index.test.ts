import { flushPromises, mount } from "@vue/test-utils";
import { beforeEach, describe, expect, it, vi } from "vitest";
import OpenAPIClientPage from "./index.vue";

const userStore = vi.hoisted(() => ({ account: { permissions: [] as string[] } }));
const api = vi.hoisted(() => ({
  list: vi.fn(),
  capabilities: vi.fn(),
  get: vi.fn(),
  create: vi.fn(),
  scopes: vi.fn(),
  rotate: vi.fn(),
  enable: vi.fn(),
  disable: vi.fn(),
  revoke: vi.fn(),
  audits: vi.fn(),
  revocation: vi.fn()
}));

vi.mock("@/store/modules/user", () => ({ useUserStoreHook: () => userStore }));
vi.mock("@/api/gb28181-openapi", () => ({
  listOpenAPIClients: api.list,
  getOpenAPIClientCapabilities: api.capabilities,
  getOpenAPIClient: api.get,
  createOpenAPIClient: api.create,
  updateOpenAPIClientScopes: api.scopes,
  rotateOpenAPIClientSecret: api.rotate,
  enableOpenAPIClient: api.enable,
  disableOpenAPIClient: api.disable,
  revokeOpenAPIClient: api.revoke,
  listOpenAPIClientAudits: api.audits,
  getOpenAPIClientRevocationStatus: api.revocation,
  isOpenAPISuccess: (response: { code?: string }) => response.code === "OK"
}));

const client = {
  id: 7,
  ak: "uvp_0123456789abcdef0123456789abcdef",
  name: "现场接入",
  ownerDeptId: 10,
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

function mountPage() {
  return mount(OpenAPIClientPage, {
    global: {
      stubs: {
        "s-layout-search": { template: "<section><slot name='fields'/><slot name='actions'/><slot name='extra'/></section>" },
        "a-input": { template: "<input />" },
        "a-select": { template: "<select><slot /></select>" },
        "a-option": { template: "<option><slot /></option>" },
        "a-button": { template: "<button :disabled='disabled'><slot name='icon'/><slot /></button>", props: ["disabled"] },
        "a-table": { template: "<div data-testid='client-table'><slot name='columns'/><slot name='empty'/></div>" },
        "a-table-column": { template: "<div />" },
        "a-drawer": { template: "<div v-if='visible'><slot /></div>", props: ["visible"] },
        "a-modal": { template: "<div v-if='visible'><slot /></div>", props: ["visible"] },
        "a-form": { template: "<form><slot /></form>" },
        "a-form-item": { template: "<label><slot /></label>" },
        "a-tag": { template: "<span><slot /></span>" },
        "a-empty": { template: "<span>{{ description }}</span>", props: ["description"] },
        "a-alert": { template: "<div role='alert'><slot /></div>" },
        "a-tooltip": { template: "<span><slot /></span>" },
        "a-spin": { template: "<div><slot /></div>" },
        "a-checkbox-group": { template: "<div><slot /></div>" },
        "a-checkbox": { template: "<label><slot /></label>", props: ["value"] },
        "OpenAPIClientDrawer": { template: "<div />", props: ["visible"] },
        "OpenAPISecretDialog": { template: "<div />", props: ["visible"] },
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
      }
    }
  });
}

describe("OpenAPI client page", () => {
  beforeEach(() => {
    userStore.account.permissions = ["*:*:*"];
    api.list.mockReset().mockResolvedValue(ok({ items: [client], page: 1, pageSize: 20, total: 1, ownerDepartments: [{ id: 10, name: "平台运维部" }] }));
    api.capabilities.mockReset().mockResolvedValue(ok(["device:list", "play:live:apply"]));
    api.get.mockReset().mockResolvedValue(ok({ client, scopes: [] }));
    api.create.mockReset().mockResolvedValue(ok({ client, secretKey: "one-time-secret" }));
    api.scopes.mockReset().mockResolvedValue(ok(client));
    api.rotate.mockReset().mockResolvedValue(ok({ client, secretKey: "rotated-secret" }));
    api.enable.mockReset().mockResolvedValue(ok(client));
    api.disable.mockReset().mockResolvedValue({ ...ok({ client, revocationStatus: "pending" }), status: 202 });
    api.revoke.mockReset().mockResolvedValue({ ...ok({ client, revocationStatus: "pending" }), status: 202 });
    api.audits.mockReset().mockResolvedValue(ok({ items: [] }));
    api.revocation.mockReset().mockResolvedValue(ok({ status: "closed", pending: 0, closed: 2 }));
  });

  it("loads the scoped ownerDepartments metadata and capabilities from the real APIs", async () => {
    const wrapper = mountPage();
    await flushPromises();
    expect(api.list).toHaveBeenCalledWith({ page: 1, pageSize: 20 });
    expect(api.capabilities).toHaveBeenCalledOnce();
    expect((wrapper.vm as any).ownerDepartments).toEqual([{ id: 10, name: "平台运维部" }]);
    expect((wrapper.vm as any).clients).toEqual([client]);
  });

  it("does not call read APIs or render management content without read permission", async () => {
    userStore.account.permissions = [];
    const wrapper = mountPage();
    await flushPromises();
    expect(api.list).not.toHaveBeenCalled();
    expect(api.capabilities).not.toHaveBeenCalled();
    expect(wrapper.text()).toContain("没有 OpenAPI 客户端查看权限");
  });

  it("keeps one-time secrets in transient page state only and clears them on close", async () => {
    const wrapper = mountPage();
    await flushPromises();
    await (wrapper.vm as any).performCreate({ name: "新客户端", ownerDeptId: 10, responsibleUserId: 7 });
    expect((wrapper.vm as any).secretPayload).toEqual({ accessKey: client.ak, secretKey: "one-time-secret" });
    await (wrapper.vm as any).closeSecret();
    expect((wrapper.vm as any).secretPayload).toBeNull();
    expect(wrapper.text()).not.toContain("one-time-secret");
  });

  it("reloads the row after a 409 and keeps the conflict tied to rowVersion", async () => {
    const conflict = Object.assign(new Error("conflict"), { response: { status: 409, data: { message: "conflict" } } });
    api.scopes.mockRejectedValueOnce(conflict);
    const wrapper = mountPage();
    await flushPromises();
    await (wrapper.vm as any).saveScopes(client.id, ["device:list"], client.rowVersion);
    await flushPromises();
    expect(api.get).toHaveBeenCalledWith(client.id);
    expect((wrapper.vm as any).drawerError).toContain("rowVersion=3");
  });

  it("keeps authentication status separate from revocation progress and does not fake completion on 503", async () => {
    api.revocation.mockResolvedValueOnce(ok({ status: "closed", pending: 0, closed: 2 }));
    const wrapper = mountPage();
    await flushPromises();
    await (wrapper.vm as any).openDetail(client);
    await flushPromises();
    expect((wrapper.vm as any).revocationStatus).toEqual(expect.objectContaining({ status: "closed" }));
    api.revocation.mockRejectedValueOnce(Object.assign(new Error("暂不可用"), { response: { status: 503 } }));
    await (wrapper.vm as any).refreshRevocation();
    await flushPromises();
    expect((wrapper.vm as any).revocationError).toContain("撤销清退进度暂不可用");
    expect((wrapper.vm as any).authStatus).toBe("active");
    expect((wrapper.vm as any).revocationStatus).toBeNull();
  });
});
