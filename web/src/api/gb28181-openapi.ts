import { http } from "@/utils/http";
import { baseUrlApi } from "./utils";

export type OpenAPIResultCode = "OK" | string;

export interface OpenAPIResponse<T> {
  code: OpenAPIResultCode;
  message: string;
  requestId?: string;
  data: T;
}

export interface OpenAPIManagedDepartment {
  id: number;
  name: string;
  parentId?: number | null;
  children?: OpenAPIManagedDepartment[];
}

export type OpenAPIClientStatus = "active" | "disabled" | "revoked";

export type OpenAPIClientDataScope = 3 | 4;

export interface OpenAPIClientDataScopeOption {
  value: OpenAPIClientDataScope;
  label: string;
  description: string;
}

export const OPENAPI_CLIENT_DEFAULT_DATA_SCOPE: OpenAPIClientDataScope = 3;

export const OPENAPI_CLIENT_DATA_SCOPE_OPTIONS: OpenAPIClientDataScopeOption[] = [
  { value: 3, label: "本部门", description: "仅访问归属部门的设备，不包含下级部门。" },
  { value: 4, label: "本部门及以下", description: "访问归属部门及其所有下级部门的设备。" }
];

export interface OpenAPIClientView {
  id: number;
  ak: string;
  name: string;
  ownerDeptId: number;
  dataScope: OpenAPIClientDataScope;
  responsibleUserId: number;
  status: OpenAPIClientStatus;
  secretVersion: number;
  authEpoch: number;
  rateLimit: number;
  burst: number;
  viewerQuota: number;
  rowVersion: number;
  createdBy: number;
  updatedBy: number;
  createdAt: string;
  updatedAt: string;
}

export interface OpenAPIScopeView {
  clientId: number;
  scope: string;
  enabled: boolean;
  scopeEpoch: number;
  updatedBy: number;
  updatedAt: string;
}

export interface OpenAPICapability {
  scope: string;
  name: string;
  method: string;
  externalPath: string;
  resourceType: string;
  risk: string;
  idempotencyRequired: boolean;
}

export interface OpenAPICapabilityGroup {
  code: string;
  name: string;
  capabilities: OpenAPICapability[];
}

export interface OpenAPIClientPage {
  items: OpenAPIClientView[];
  page: number;
  pageSize: number;
  total: number;
  ownerDepartments: OpenAPIManagedDepartment[];
}

export interface OpenAPIClientListParams {
  page: number;
  pageSize: number;
  ownerDeptId?: number;
}

export interface OpenAPIClientDetail {
  client: OpenAPIClientView;
  scopes: OpenAPIScopeView[];
}

export interface OpenAPIClientCreateInput {
  name: string;
  ownerDeptId: number;
  dataScope?: OpenAPIClientDataScope;
  responsibleUserId?: number;
}

export interface OpenAPIClientSecretResult {
  client: OpenAPIClientView;
  secretKey: string;
}

export interface OpenAPIClientStatusResult {
  client: OpenAPIClientView;
  revocationStatus?: "pending";
}

export interface OpenAPIClientScopesInput {
  rowVersion: number;
  scopes: string[];
}

export interface OpenAPIClientAudit {
  requestId: string;
  scope: string;
  resourceType: string;
  resourceId: string;
  result: string;
  reasonClass: string;
  source: string;
  latencyMs: number;
  createdAt: string;
}

export interface OpenAPIClientAuditPage {
  items: OpenAPIClientAudit[];
}

export type OpenAPIRevocationState = "pending" | "closed" | "unknown" | string;

export interface OpenAPIRevocationStatus {
  status: OpenAPIRevocationState;
  pending: number;
  closed: number;
}

export const OPENAPI_CLIENT_SCOPES = [
  "device:list",
  "device:detail",
  "device:status",
  "channel:list",
  "channel:detail",
  "channel:status",
  "play:live:apply"
] as const;

const path = "gb28181/openapi-clients";

export const listOpenAPIClients = (params: OpenAPIClientListParams) =>
  http.request<OpenAPIResponse<OpenAPIClientPage>>("get", baseUrlApi(path), { params });

export const getOpenAPIClientCapabilities = () =>
  http.request<OpenAPIResponse<string[]>>("get", baseUrlApi(`${path}/capabilities`));

export const getOpenAPIClientCapabilityCatalog = () =>
  http.request<OpenAPIResponse<{ groups: OpenAPICapabilityGroup[] }>>("get", baseUrlApi(`${path}/capabilities/catalog`));

export const getOpenAPIClient = (id: number) =>
  http.request<OpenAPIResponse<OpenAPIClientDetail>>("get", baseUrlApi(`${path}/${id}`));

export const createOpenAPIClient = (data: OpenAPIClientCreateInput) =>
  http.request<OpenAPIResponse<OpenAPIClientSecretResult>>("post", baseUrlApi(path), { data });

export const updateOpenAPIClientScopes = (id: number, data: OpenAPIClientScopesInput) =>
  http.request<OpenAPIResponse<OpenAPIClientView>>("put", baseUrlApi(`${path}/${id}/scopes`), { data });

export const rotateOpenAPIClientSecret = (id: number, rowVersion: number) =>
  http.request<OpenAPIResponse<OpenAPIClientSecretResult>>("post", baseUrlApi(`${path}/${id}/rotate-secret`), {
    data: { rowVersion }
  });

const statusMutation = (action: "enable" | "disable" | "revoke", id: number, rowVersion: number) =>
  http.request<OpenAPIResponse<OpenAPIClientStatusResult>>("post", baseUrlApi(`${path}/${id}/${action}`), {
    data: { rowVersion }
  });

export const enableOpenAPIClient = (id: number, rowVersion: number) => statusMutation("enable", id, rowVersion);
export const disableOpenAPIClient = (id: number, rowVersion: number) => statusMutation("disable", id, rowVersion);
export const revokeOpenAPIClient = (id: number, rowVersion: number) => statusMutation("revoke", id, rowVersion);

export const listOpenAPIClientAudits = (id: number) =>
  http.request<OpenAPIResponse<OpenAPIClientAuditPage>>("get", baseUrlApi(`${path}/${id}/audits`));

export const getOpenAPIClientRevocationStatus = (id: number) =>
  http.request<OpenAPIResponse<OpenAPIRevocationStatus>>("get", baseUrlApi(`${path}/${id}/revocation-status`));

export function isOpenAPISuccess<T>(
  response: OpenAPIResponse<T> | null | undefined
): response is OpenAPIResponse<T> & { code: "OK" } {
  return response?.code === "OK";
}
