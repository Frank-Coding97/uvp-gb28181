import { beforeEach, describe, expect, it, vi } from "vitest";

const request = vi.hoisted(() => vi.fn());

vi.mock("@/utils/http", () => ({ http: { request } }));
vi.mock("@/api/utils", () => ({ baseUrlApi: (path: string) => `/api/${path}` }));

import {
  createOpenAPIClient,
  disableOpenAPIClient,
  enableOpenAPIClient,
  getOpenAPIClient,
  getOpenAPIClientCapabilities,
  getOpenAPIClientRevocationStatus,
  listOpenAPIClientAudits,
  listOpenAPIClients,
  revokeOpenAPIClient,
  rotateOpenAPIClientSecret,
  updateOpenAPIClientScopes
} from "./gb28181-openapi";

describe("OpenAPI client management API contract", () => {
  beforeEach(() => request.mockReset().mockResolvedValue({ code: "OK", data: null }));

  it("uses the real paged management list and carries scoped department options", async () => {
    await listOpenAPIClients({ page: 2, pageSize: 20, ownerDeptId: 12 });

    expect(request).toHaveBeenLastCalledWith(
      "get",
      "/api/gb28181/openapi-clients",
      { params: { page: 2, pageSize: 20, ownerDeptId: 12 } }
    );
  });

  it("uses the real detail and capability endpoints", async () => {
    await getOpenAPIClientCapabilities();
    expect(request).toHaveBeenLastCalledWith("get", "/api/gb28181/openapi-clients/capabilities");

    await getOpenAPIClient(7);
    expect(request).toHaveBeenLastCalledWith("get", "/api/gb28181/openapi-clients/7");
  });

  it("keeps create and secret rotation payloads one-shot and server-shaped", async () => {
    await createOpenAPIClient({ name: "现场接入", ownerDeptId: 10, responsibleUserId: 7 });
    expect(request).toHaveBeenLastCalledWith(
      "post",
      "/api/gb28181/openapi-clients",
      { data: { name: "现场接入", ownerDeptId: 10, responsibleUserId: 7 } }
    );

    await rotateOpenAPIClientSecret(7, 3);
    expect(request).toHaveBeenLastCalledWith(
      "post",
      "/api/gb28181/openapi-clients/7/rotate-secret",
      { data: { rowVersion: 3 } }
    );
  });

  it("keeps scope and lifecycle mutations separate from UI button permissions", async () => {
    await updateOpenAPIClientScopes(7, { rowVersion: 4, scopes: ["device:list"] });
    expect(request).toHaveBeenLastCalledWith(
      "put",
      "/api/gb28181/openapi-clients/7/scopes",
      { data: { rowVersion: 4, scopes: ["device:list"] } }
    );

    await enableOpenAPIClient(7, 5);
    expect(request).toHaveBeenLastCalledWith(
      "post",
      "/api/gb28181/openapi-clients/7/enable",
      { data: { rowVersion: 5 } }
    );
    await disableOpenAPIClient(7, 6);
    expect(request).toHaveBeenLastCalledWith(
      "post",
      "/api/gb28181/openapi-clients/7/disable",
      { data: { rowVersion: 6 } }
    );
    await revokeOpenAPIClient(7, 7);
    expect(request).toHaveBeenLastCalledWith(
      "post",
      "/api/gb28181/openapi-clients/7/revoke",
      { data: { rowVersion: 7 } }
    );
  });

  it("loads audit and revocation progress through their dedicated endpoints", async () => {
    await listOpenAPIClientAudits(7);
    expect(request).toHaveBeenLastCalledWith("get", "/api/gb28181/openapi-clients/7/audits");

    await getOpenAPIClientRevocationStatus(7);
    expect(request).toHaveBeenLastCalledWith("get", "/api/gb28181/openapi-clients/7/revocation-status");
  });
});
