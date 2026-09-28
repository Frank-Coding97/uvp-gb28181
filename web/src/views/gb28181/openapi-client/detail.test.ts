import { flushPromises, mount } from "@vue/test-utils";
import { reactive } from "vue";
import { beforeEach, describe, expect, it, vi } from "vitest";
import OpenAPIClientDetail from "./detail.vue";

const userStore = reactive({ account: { permissions: ["*:*:*"] as string[] } });
const routeState = reactive({ params: { id: "7" }, query: { tab: "capabilities" } as Record<string, string> });
const routerState = vi.hoisted(() => ({ push: vi.fn(), replace: vi.fn() }));
const api = vi.hoisted(() => ({
  get: vi.fn(),
  capabilities: vi.fn(),
  catalog: vi.fn(),
  scopes: vi.fn(),
  audits: vi.fn(),
  revocation: vi.fn(),
  rotate: vi.fn(),
  enable: vi.fn(),
  disable: vi.fn(),
  revoke: vi.fn()
}));

vi.mock("@/store/modules/user", () => ({ useUserStoreHook: () => userStore }));
vi.mock("vue-router", async importOriginal => ({
  ...(await importOriginal<typeof import("vue-router")>()),
  useRoute: () => routeState,
  useRouter: () => routerState,
  onBeforeRouteLeave: vi.fn()
}));
vi.mock("@/api/gb28181-openapi", async importOriginal => {
  const actual = await importOriginal<typeof import("@/api/gb28181-openapi")>();
  return {
    ...actual,
    getOpenAPIClient: api.get,
    getOpenAPIClientCapabilities: api.capabilities,
    getOpenAPIClientCapabilityCatalog: api.catalog,
    updateOpenAPIClientScopes: api.scopes,
    listOpenAPIClientAudits: api.audits,
    getOpenAPIClientRevocationStatus: api.revocation,
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
  }
];

function ok<T>(data: T) {
  return { code: "OK", message: "success", data };
}

function mountDetail() {
  return mount(OpenAPIClientDetail, {
    global: {
      stubs: {
        "a-button": { template: "<button><slot name='icon'/><slot /></button>" },
        "a-card": { template: "<section><slot name='extra'/><slot /></section>" },
        "a-tabs": { template: "<div><slot /></div>" },
        "a-tab-pane": { template: "<section><slot /></section>" },
        "a-descriptions": { template: "<dl><slot /></dl>" },
        "a-descriptions-item": { template: "<div><slot /></div>" },
        "a-tag": { template: "<span><slot /></span>" },
        "a-alert": { template: "<div><slot /></div>" },
        "a-spin": { template: "<div />" },
        "a-empty": { template: "<div>{{ description }}</div>", props: ["description"] },
        "a-select": { template: "<select><slot /></select>" },
        "a-option": { template: "<option><slot /></option>" },
        "a-table": { template: "<div><slot name='columns'/><slot name='empty'/></div>" },
        "a-table-column": { template: "<div />" },
        OpenAPICapabilityWorkbench: {
          template: "<div data-testid='workbench' />",
          props: ["groups", "enabledScopes", "editable", "saving"]
        },
        OpenAPISecretDialog: { template: "<div />" },
        ArrowLeft: true,
        KeyRound: true,
        RefreshCw: true,
        RotateCcw: true,
        ShieldOff: true
      }
    }
  });
}

describe("OpenAPI client detail workspace", () => {
  beforeEach(() => {
    userStore.account.permissions = ["*:*:*"];
    routeState.params.id = "7";
    routeState.query = { tab: "capabilities" };
    routerState.push.mockReset();
    routerState.replace.mockReset();
    api.get.mockReset().mockResolvedValue(
      ok({
        client,
        scopes: [{ clientId: 7, scope: "device:list", enabled: true, scopeEpoch: 1, updatedBy: 7, updatedAt: client.updatedAt }]
      })
    );
    api.capabilities.mockReset().mockResolvedValue(ok(["device:list"]));
    api.catalog.mockReset().mockResolvedValue(ok({ groups }));
    api.scopes.mockReset().mockResolvedValue(ok({ ...client, rowVersion: 4 }));
    api.audits.mockReset().mockResolvedValue(ok({ items: [] }));
    api.revocation.mockReset().mockResolvedValue(ok({ status: "closed", pending: 0, closed: 1 }));
    api.rotate.mockReset();
    api.enable.mockReset();
    api.disable.mockReset();
    api.revoke.mockReset();
  });

  it("loads detail and the published capability catalog before enabling authorization", async () => {
    const wrapper = mountDetail();
    await flushPromises();

    expect(api.get).toHaveBeenCalledWith(7);
    expect(api.capabilities).toHaveBeenCalledOnce();
    expect(api.catalog).toHaveBeenCalledOnce();
    expect((wrapper.vm as any).catalogReady).toBe(true);
    expect((wrapper.vm as any).editable).toBe(true);
  });

  it("keeps non-authorization detail available when the capability catalog request fails", async () => {
    api.catalog.mockRejectedValueOnce(new Error("catalog unavailable"));
    const wrapper = mountDetail();
    await flushPromises();

    expect((wrapper.vm as any).client).toEqual(client);
    expect((wrapper.vm as any).detailReady).toBe(true);
    expect((wrapper.vm as any).catalogReady).toBe(false);
    expect((wrapper.vm as any).scopeError).toContain("catalog unavailable");
    expect((wrapper.vm as any).error).toBe("");
  });

  it("saves the complete selected scope set with the current row version", async () => {
    const wrapper = mountDetail();
    await flushPromises();

    await (wrapper.vm as any).saveScopes(["device:list"]);

    expect(api.scopes).toHaveBeenCalledWith(7, { rowVersion: 3, scopes: ["device:list"] });
  });

  it("loads call records only in their dedicated workspace", async () => {
    const wrapper = mountDetail();
    await flushPromises();
    expect(api.audits).not.toHaveBeenCalled();

    (wrapper.vm as any).switchTab("logs");
    await flushPromises();

    expect(api.audits).toHaveBeenCalledWith(7);
    expect(routerState.replace).toHaveBeenCalledWith({ query: { tab: "logs" } });
  });

  it("clears client and audit data when read permission is lost", async () => {
    const wrapper = mountDetail();
    await flushPromises();
    (wrapper.vm as any).audits = [{ requestId: "old" }];

    userStore.account.permissions = [];
    await flushPromises();

    expect((wrapper.vm as any).client).toBeNull();
    expect((wrapper.vm as any).audits).toEqual([]);
  });
});
