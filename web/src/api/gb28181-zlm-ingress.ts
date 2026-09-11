import { http } from "@/utils/http";
import { baseUrlApi } from "./utils";
import type { BaseResult } from "./types";
import type {
  ZLMMediaIdentity,
  ZLMOwnershipImpact,
  ZLMOwnershipPreflight,
  ZLMOwnershipStatus,
  ZLMOwnershipTarget,
  ZLMPage,
  ZLMPageQuery
} from "./gb28181-zlm-runtime";

export type ZLMCapabilityState = "supported" | "unsupported" | "unknown";
export type ZLMProxyKind = "pull_proxy" | "push_proxy";

export interface ZLMURLSummary {
  scheme: string;
  host: string;
  port?: number;
  fingerprint: string;
  hasUserInfo: boolean;
  hasSensitiveQuery: boolean;
  display: string;
}

export interface ZLMProxy {
  nodeId: number;
  kind: ZLMProxyKind;
  key: string;
  media: ZLMMediaIdentity;
  online: boolean;
  status: number;
  statusText?: string;
  liveSecs: number;
  rePullCount: number;
  rePublishCount: number;
  totalReaderCount: number;
  bytesSpeed: number;
  totalBytes: number;
  source?: ZLMURLSummary;
  target?: ZLMURLSummary;
  capability: ZLMCapabilityState;
  managed: boolean;
  createdBy?: number;
  provenanceFingerprint?: string;
  provenanceSummary?: string;
}

export interface ZLMProxyPage extends ZLMPage<ZLMProxy> {
  capability: ZLMCapabilityState;
}

export interface ZLMPullProxyCreateRequest {
  media: ZLMMediaIdentity;
  sourceUrl: string;
  retryCount?: number;
  rtpType?: number;
  timeoutSec?: number;
}

export interface ZLMPushProxyCreateRequest {
  media: ZLMMediaIdentity;
  targetUrl: string;
  retryCount?: number;
  rtpType?: number;
  timeoutSec?: number;
}

export interface ZLMProxyDeleteRequest {
  nodeId: number;
  key: string;
  media: ZLMMediaIdentity;
  fingerprint?: string;
}

export interface ZLMProxyDeletePreflight {
  nodeId: number;
  kind: ZLMProxyKind;
  key: string;
  target: ZLMOwnershipTarget;
  fingerprint: string;
  status: ZLMOwnershipStatus;
  present: boolean;
  presenceKnown: boolean;
  impacts?: ZLMOwnershipImpact[];
}

export interface ZLMProxyDeleteResult {
  nodeId: number;
  kind: ZLMProxyKind;
  key: string;
  removed: boolean;
  alreadyAbsent: boolean;
  tombstoned: boolean;
}

export interface ZLMFFmpegSourceCreateRequest {
  templateKey: string;
  srcUrl: string;
  dstUrl: string;
  timeoutMs: number;
  enableHls: boolean;
  enableMp4: boolean;
}

export interface ZLMFFmpegURLView {
  summary: string;
  fingerprint: string;
}

export interface ZLMFFmpegSource {
  nodeId: number;
  key: string;
  templateKey: string;
  srcUrl: ZLMFFmpegURLView;
  dstUrl: ZLMFFmpegURLView;
  createdBy?: number;
  managed: boolean;
}

export interface ZLMFFmpegSourcePage extends ZLMPage<ZLMFFmpegSource> {
  capability: ZLMCapabilityState;
  templates: string[];
}

export interface ZLMFFmpegDeleteResult {
  nodeId: number;
  key: string;
  released: boolean;
  alreadyReleased: boolean;
  idempotent: boolean;
}

export interface ZLMRTPServerCreateRequest {
  vhost: string;
  app: string;
  stream: string;
  port: number;
  tcpMode: number;
  ssrc?: string;
  onlyTrack: number;
  localIp?: string;
  reuse: boolean;
}

export interface ZLMRTPServerCloseRequest {
  nodeId: number;
  vhost: string;
  app: string;
  stream: string;
}

export interface ZLMRTPServer {
  nodeId: number;
  key: string;
  vhost: string;
  app: string;
  stream: string;
  ssrc: string;
  port: number;
  tcpMode: number;
  onlyTrack: number;
  released: boolean;
  managed: boolean;
  createdBy?: number;
}

export interface ZLMRTPServerPage extends ZLMPage<ZLMRTPServer> {
  capability: ZLMCapabilityState;
}

export interface ZLMRTPServerCloseResult {
  nodeId: number;
  stream: string;
  released: boolean;
  alreadyReleased: boolean;
  idempotent: boolean;
  hit: boolean;
}

function pageOptions(page: ZLMPageQuery, signal?: AbortSignal) {
  const params = Object.fromEntries(Object.entries(page).filter(([, value]) => value !== undefined && value !== null));
  const options: { params?: Record<string, unknown>; signal?: AbortSignal } = {};
  if (Object.keys(params).length > 0) options.params = params;
  if (signal) options.signal = signal;
  return Object.keys(options).length > 0 ? options : undefined;
}

function encodedKey(key: string) {
  return encodeURIComponent(key);
}

function proxyDeleteBody(nodeId: number, key: string, request: ZLMProxyDeleteRequest): ZLMProxyDeleteRequest {
  return { ...request, nodeId, key };
}

export const listZLMPullProxies = (nodeId: number, page: ZLMPageQuery = {}, signal?: AbortSignal) =>
  http.request<BaseResult<ZLMProxyPage>>(
    "get",
    baseUrlApi(`gb28181/zlm/nodes/${nodeId}/proxies/pull`),
    pageOptions(page, signal)
  );

export const createZLMPullProxy = (nodeId: number, request: ZLMPullProxyCreateRequest) =>
  http.request<BaseResult<ZLMProxy>>("post", baseUrlApi(`gb28181/zlm/nodes/${nodeId}/proxies/pull`), {
    data: { nodeId, ...request }
  });

export const getZLMPullProxy = (nodeId: number, key: string, signal?: AbortSignal) =>
  http.request<BaseResult<ZLMProxy>>(
    "get",
    baseUrlApi(`gb28181/zlm/nodes/${nodeId}/proxies/pull/${encodedKey(key)}`),
    signal ? { signal } : undefined
  );

export const preflightDeleteZLMPullProxy = (nodeId: number, key: string, request: ZLMProxyDeleteRequest) =>
  http.request<BaseResult<ZLMProxyDeletePreflight>>(
    "post",
    baseUrlApi(`gb28181/zlm/nodes/${nodeId}/proxies/pull/${encodedKey(key)}/preflight`),
    { data: proxyDeleteBody(nodeId, key, request) }
  );

export const deleteZLMPullProxy = (nodeId: number, key: string, request: ZLMProxyDeleteRequest) =>
  http.request<BaseResult<ZLMProxyDeleteResult>>(
    "delete",
    baseUrlApi(`gb28181/zlm/nodes/${nodeId}/proxies/pull/${encodedKey(key)}`),
    { data: proxyDeleteBody(nodeId, key, request) }
  );

export const listZLMPushProxies = (nodeId: number, page: ZLMPageQuery = {}, signal?: AbortSignal) =>
  http.request<BaseResult<ZLMProxyPage>>(
    "get",
    baseUrlApi(`gb28181/zlm/nodes/${nodeId}/proxies/push`),
    pageOptions(page, signal)
  );

export const createZLMPushProxy = (nodeId: number, request: ZLMPushProxyCreateRequest) =>
  http.request<BaseResult<ZLMProxy>>("post", baseUrlApi(`gb28181/zlm/nodes/${nodeId}/proxies/push`), {
    data: { nodeId, ...request }
  });

export const getZLMPushProxy = (nodeId: number, key: string, signal?: AbortSignal) =>
  http.request<BaseResult<ZLMProxy>>(
    "get",
    baseUrlApi(`gb28181/zlm/nodes/${nodeId}/proxies/push/${encodedKey(key)}`),
    signal ? { signal } : undefined
  );

export const preflightDeleteZLMPushProxy = (nodeId: number, key: string, request: ZLMProxyDeleteRequest) =>
  http.request<BaseResult<ZLMProxyDeletePreflight>>(
    "post",
    baseUrlApi(`gb28181/zlm/nodes/${nodeId}/proxies/push/${encodedKey(key)}/preflight`),
    { data: proxyDeleteBody(nodeId, key, request) }
  );

export const deleteZLMPushProxy = (nodeId: number, key: string, request: ZLMProxyDeleteRequest) =>
  http.request<BaseResult<ZLMProxyDeleteResult>>(
    "delete",
    baseUrlApi(`gb28181/zlm/nodes/${nodeId}/proxies/push/${encodedKey(key)}`),
    { data: proxyDeleteBody(nodeId, key, request) }
  );

export const listZLMFFmpegSources = (nodeId: number, page: ZLMPageQuery = {}, signal?: AbortSignal) =>
  http.request<BaseResult<ZLMFFmpegSourcePage>>(
    "get",
    baseUrlApi(`gb28181/zlm/nodes/${nodeId}/ffmpeg-sources`),
    pageOptions(page, signal)
  );

export const createZLMFFmpegSource = (nodeId: number, request: ZLMFFmpegSourceCreateRequest) =>
  http.request<BaseResult<ZLMFFmpegSource>>("post", baseUrlApi(`gb28181/zlm/nodes/${nodeId}/ffmpeg-sources`), { data: request });

export const preflightDeleteZLMFFmpegSource = (nodeId: number, key: string) =>
  http.request<BaseResult<ZLMOwnershipPreflight>>(
    "post",
    baseUrlApi(`gb28181/zlm/nodes/${nodeId}/ffmpeg-sources/${encodedKey(key)}/preflight`)
  );

export const deleteZLMFFmpegSource = (nodeId: number, key: string) =>
  http.request<BaseResult<ZLMFFmpegDeleteResult>>(
    "delete",
    baseUrlApi(`gb28181/zlm/nodes/${nodeId}/ffmpeg-sources/${encodedKey(key)}`)
  );

export const listZLMRTPServers = (nodeId: number, page: ZLMPageQuery = {}, signal?: AbortSignal) =>
  http.request<BaseResult<ZLMRTPServerPage>>(
    "get",
    baseUrlApi(`gb28181/zlm/nodes/${nodeId}/rtp-servers`),
    pageOptions(page, signal)
  );

export const createZLMRTPServer = (nodeId: number, request: ZLMRTPServerCreateRequest) =>
  http.request<BaseResult<ZLMRTPServer>>("post", baseUrlApi(`gb28181/zlm/nodes/${nodeId}/rtp-servers`), { data: request });

export const preflightCloseZLMRTPServer = (nodeId: number, request: ZLMRTPServerCloseRequest) =>
  http.request<BaseResult<ZLMOwnershipPreflight>>("post", baseUrlApi(`gb28181/zlm/nodes/${nodeId}/rtp-servers/close/preflight`), {
    data: { ...request, nodeId }
  });

export const closeZLMRTPServer = (nodeId: number, request: ZLMRTPServerCloseRequest) =>
  http.request<BaseResult<ZLMRTPServerCloseResult>>("post", baseUrlApi(`gb28181/zlm/nodes/${nodeId}/rtp-servers/close`), {
    data: { ...request, nodeId }
  });

export const forceCloseZLMRTPServer = (nodeId: number, request: ZLMRTPServerCloseRequest, reason: string) =>
  http.request<BaseResult<ZLMRTPServerCloseResult>>("post", baseUrlApi(`gb28181/zlm/nodes/${nodeId}/rtp-servers/force-close`), {
    data: { ...request, nodeId, reason }
  });
