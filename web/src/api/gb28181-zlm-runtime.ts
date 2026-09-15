import { http } from "@/utils/http";
import { baseUrlApi } from "./utils";
import type { BaseResult } from "./types";

export interface ZLMPageQuery {
  page?: number;
  pageSize?: number;
}

export interface ZLMPage<T> {
  list: T[];
  total: number;
  page: number;
  pageSize: number;
  truncated: boolean;
}

export interface ZLMMediaIdentity {
  schema: string;
  vhost: string;
  app: string;
  stream: string;
}

export interface ZLMOwnershipTarget {
  nodeId: number;
  media: ZLMMediaIdentity;
}

export type ZLMRuntimeFreshness = "fresh" | "stale" | "unavailable" | "maintenance";
export type ZLMRuntimeStatus = "fresh" | "partial" | "unavailable" | "maintenance";
export type ZLMOwnershipType =
  | "realtime_playback"
  | "device_playback"
  | "talk"
  | "cascade"
  | "recording_plan"
  | "continuous_recording"
  | "recording_session"
  | "managed"
  | "unknown";
export type ZLMOwnershipConfidence = "proven" | "uncertain";
export type ZLMOwnershipStatus = "absent" | "managed" | "owned" | "unknown" | "conflicted";

export interface ZLMOwnershipEvidence {
  type: ZLMOwnershipType;
  resourceType?: string;
  key?: string;
  owner?: string;
  confidence: ZLMOwnershipConfidence;
  reason?: string;
}

export interface ZLMOwnershipImpact {
  resourceType?: string;
  resourceKey?: string;
  owner?: string;
  reason?: string;
  media?: ZLMMediaIdentity;
}

export interface ZLMOwnershipSnapshot {
  target: ZLMOwnershipTarget;
  present: boolean;
  presenceKnown: boolean;
  status: ZLMOwnershipStatus;
  owners?: ZLMOwnershipEvidence[];
  impacts?: ZLMOwnershipImpact[];
  fingerprint: string;
}

export interface ZLMOwnershipPreflight {
  target: ZLMOwnershipTarget;
  snapshot: ZLMOwnershipSnapshot;
  fingerprint: string;
}

export interface ZLMOwnershipBatchPreflight {
  targets: ZLMOwnershipTarget[];
  snapshots: ZLMOwnershipSnapshot[];
  fingerprint: string;
}

export interface ZLMRuntimeError {
  stage: string;
  code: string;
  message: string;
  retryable: boolean;
}

export interface ZLMNodeRuntimeMetrics {
  mediaSourceCount: number;
  multiMediaSourceMuxerCount: number;
  tcpServerCount: number;
  tcpSessionCount: number;
  udpServerCount: number;
  udpSessionCount: number;
  tcpClientCount: number;
  socketCount: number;
  networkSessionCount: number;
  netThreadLoad: number;
  workThreadLoad: number;
  eventThreadLoads?: ZLMThreadLoad[];
  objectStatistics?: ZLMObjectStatistics;
  upstreamBytesPerSecond?: number;
  downstreamBytesPerSecond?: number;
  mediaTrafficAvailable?: boolean;
}

export interface ZLMThreadLoad {
  name: string;
  load: number;
  fdCount: number;
  nodeId?: number;
}

export interface ZLMObjectStatistics {
  mediaSource: number;
  multiMediaSourceMuxer: number;
  tcpServer: number;
  tcpSession: number;
  udpServer: number;
  udpSession: number;
  tcpClient: number;
  socket: number;
  frameImp: number;
  frame: number;
  buffer: number;
  bufferRaw: number;
  bufferLikeString: number;
  bufferList: number;
  rtpPacket: number;
  rtmpPacket: number;
}

export interface ZLMRuntimeMedia {
  nodeId: number;
  media: ZLMMediaIdentity;
  online: boolean;
  aliveSecond: number;
  bytesSpeed: number;
  readerCount: number;
  totalReaderCount: number;
  originType: number;
  originTypeName?: string;
  recordingMp4: boolean;
  recordingHls: boolean;
  trackCount: number;
}

export interface ZLMNodeRuntime {
  nodeId: number;
  name: string;
  state: "active" | "maintenance" | "offline";
  status: ZLMRuntimeStatus;
  freshness: ZLMRuntimeFreshness;
  asOf: string;
  heartbeatAsOf?: string;
  heartbeatFreshness: ZLMRuntimeFreshness;
  metrics: ZLMNodeRuntimeMetrics;
  metricsComplete: boolean;
  mediaFreshness: ZLMRuntimeFreshness;
  streams?: ZLMRuntimeMedia[];
  error?: ZLMRuntimeError;
  errors?: ZLMRuntimeError[];
}

export interface ZLMOverviewMetrics {
  sampledNodeCount: number;
  mediaSourceCount: number;
  multiMediaSourceMuxerCount: number;
  tcpServerCount: number;
  tcpSessionCount: number;
  udpServerCount: number;
  udpSessionCount: number;
  tcpClientCount: number;
  socketCount: number;
  networkSessionCount: number;
  netThreadLoadAvg: number;
  workThreadLoadAvg: number;
  streamCount: number;
  objectStatistics?: ZLMObjectStatistics;
  upstreamBytesPerSecond?: number;
  downstreamBytesPerSecond?: number;
  mediaTrafficSampledNodes?: number;
}

export interface ZLMMediaRateSample {
  sampledAt: number;
  upstream: number;
  downstream: number;
}

export interface ZLMOverview {
  nodes: ZLMNodeRuntime[];
  streams: ZLMRuntimeMedia[];
  metrics: ZLMOverviewMetrics;
  mediaRateSamples?: ZLMMediaRateSample[];
  partial: boolean;
  asOf: string;
  metricsSampledNodeIds: number[];
  mediaSampledNodeIds: number[];
  successfulNodeIds: number[];
  failedNodeIds: number[];
  errors?: Array<{ nodeId: number; error: ZLMRuntimeError }>;
}

export interface ZLMStreamQuery extends ZLMPageQuery {
  nodeId?: number;
  schema?: string;
  vhost?: string;
  app?: string;
  stream?: string;
  originType?: number;
  recordingMp4?: boolean;
  recordingHls?: boolean;
}

export interface ZLMStreamTrack {
  codecId: number;
  codecIdName: string;
  ready: boolean;
  codecType: number;
  frames: number;
  duration: number;
  sampleRate: number;
  channels: number;
  sampleBit: number;
  width: number;
  height: number;
  fps: number;
  keyFrames: number;
  gopSize: number;
  gopIntervalMs: number;
  loss?: number;
}

export interface ZLMStreamOwnership {
  status: ZLMOwnershipStatus;
  present: boolean;
  presenceKnown: boolean;
  sources?: Array<{ type: ZLMOwnershipType; confidence: ZLMOwnershipConfidence }>;
  impacts?: Array<{ type: ZLMOwnershipType; count: number }>;
}

export interface ZLMStream {
  nodeId: number;
  nodeUuid: string;
  media: ZLMMediaIdentity;
  online: boolean;
  aliveSecond: number;
  bytesSpeed: number;
  totalBytes: number;
  readerCount: number;
  totalReaderCount: number;
  originType: number;
  originTypeName?: string;
  recordingMp4: boolean;
  recordingHls: boolean;
  trackCount: number;
  tracks?: ZLMStreamTrack[];
  ownership: ZLMStreamOwnership;
}

export interface ZLMStreamDetail extends ZLMStream {
  createStamp: number;
  currentStamp: number;
}

export interface ZLMStreamPage extends ZLMPage<ZLMStream> {
  partial: boolean;
  asOf: string;
  errors?: Array<{ nodeId: number; code: string; message: string; retryable: boolean }>;
}

export interface ZLMStreamViewer {
  nodeId: number;
  nodeUuid: string;
  media: ZLMMediaIdentity;
  identifier: string;
  peerIp: string;
  peerPort: number;
  localIp: string;
  localPort: number;
  typeId: string;
  kickable: boolean;
}

export interface ZLMStreamViewerPage extends ZLMPage<ZLMStreamViewer> {
  nodeId: number;
  nodeUuid: string;
  target: ZLMMediaIdentity;
  asOf: string;
}

export interface ZLMNetworkSession {
  nodeId: number;
  nodeUuid: string;
  id: string;
  peerIp: string;
  peerPort: number;
  localIp: string;
  localPort: number;
  identifier: string;
  type: string;
  typeId: string;
}

export interface ZLMNetworkSessionPage extends ZLMPage<ZLMNetworkSession> {
  nodeId: number;
  nodeUuid: string;
  asOf: string;
}

export interface ZLMStreamClosePreflight {
  target: ZLMOwnershipTarget;
  snapshot: ZLMStreamOwnership;
  fingerprint: string;
  freshPresent: boolean;
}

export interface ZLMStreamCloseResult {
  target: ZLMOwnershipTarget;
  closed: boolean;
  alreadyAbsent: boolean;
  force: boolean;
  uncertain: boolean;
  retryable: boolean;
}

export interface ZLMPreviewGrant {
  nodeId: number;
  nodeUuid: string;
  media: ZLMMediaIdentity;
  protocol: string;
  hookSchema: string;
  token: string;
  url: string;
  expiresAt: string;
  jtiHash: string;
}

export type ZLMRecorderType = 0 | 1;

export interface ZLMRecordingRequest {
  target: ZLMOwnershipTarget;
  type: ZLMRecorderType;
  maxSecond?: number;
  fingerprint?: string;
}

export interface ZLMRecordingResult {
  target: ZLMOwnershipTarget;
  type: ZLMRecorderType;
  state: "unknown" | "starting" | "recording" | "stopping" | "stopped";
  externalState: "unknown" | "recording" | "stopped";
  rollbackUncertain?: boolean;
  recording: boolean;
  retryable: boolean;
  leaseId?: string;
  ownership: ZLMOwnershipSnapshot;
  reason?: string;
}

export interface ZLMRecordingPreflight {
  target: ZLMOwnershipTarget;
  type: ZLMRecorderType;
  leaseId?: string;
  snapshot: ZLMOwnershipSnapshot;
  fingerprint: string;
}

function compactParams(values: object) {
  return Object.fromEntries(Object.entries(values).filter(([, value]) => value !== undefined && value !== null && value !== ""));
}

function getOptions(signal?: AbortSignal, params?: object) {
  const options: { params?: Record<string, unknown>; signal?: AbortSignal } = {};
  if (params && Object.keys(params).length > 0) options.params = compactParams(params);
  if (signal) options.signal = signal;
  return Object.keys(options).length > 0 ? options : undefined;
}

function mediaParams(media: ZLMMediaIdentity) {
  return { schema: media.schema, vhost: media.vhost, app: media.app, stream: media.stream };
}

function target(nodeId: number, media: ZLMMediaIdentity): ZLMOwnershipTarget {
  return { nodeId, media };
}

export const getZLMOverview = (signal?: AbortSignal) =>
  http.request<BaseResult<ZLMOverview>>("get", baseUrlApi("gb28181/zlm/overview"), getOptions(signal));

export const getZLMNodeRuntime = (nodeId: number, signal?: AbortSignal) =>
  http.request<BaseResult<ZLMNodeRuntime>>("get", baseUrlApi(`gb28181/zlm/nodes/${nodeId}/runtime`), getOptions(signal));

export const listZLMStreams = (query: ZLMStreamQuery = {}, signal?: AbortSignal) =>
  http.request<BaseResult<ZLMStreamPage>>("get", baseUrlApi("gb28181/zlm/streams"), getOptions(signal, query));

export const listZLMNodeStreams = (nodeId: number, query: Omit<ZLMStreamQuery, "nodeId"> = {}, signal?: AbortSignal) =>
  http.request<BaseResult<ZLMStreamPage>>("get", baseUrlApi(`gb28181/zlm/nodes/${nodeId}/streams`), getOptions(signal, query));

export const getZLMStreamDetail = (nodeId: number, media: ZLMMediaIdentity, signal?: AbortSignal) =>
  http.request<BaseResult<ZLMStreamDetail>>(
    "get",
    baseUrlApi(`gb28181/zlm/nodes/${nodeId}/streams/detail`),
    getOptions(signal, mediaParams(media))
  );

export const listZLMStreamViewers = (nodeId: number, media: ZLMMediaIdentity, page: ZLMPageQuery = {}, signal?: AbortSignal) =>
  http.request<BaseResult<ZLMStreamViewerPage>>(
    "get",
    baseUrlApi(`gb28181/zlm/nodes/${nodeId}/streams/viewers`),
    getOptions(signal, { ...mediaParams(media), ...page })
  );

export const issueZLMPreviewGrant = (
  nodeId: number,
  media: ZLMMediaIdentity,
  protocol: string,
  options: { clientIp?: string; bindClientIp?: boolean } = {}
) =>
  http.request<BaseResult<ZLMPreviewGrant>>("post", baseUrlApi(`gb28181/zlm/nodes/${nodeId}/streams/playback-grant`), {
    data: { nodeId, media, protocol, ...options }
  });

export function snapshotZLMStreamURL(nodeId: number, media: ZLMMediaIdentity) {
  const params = new URLSearchParams(mediaParams(media));
  return `${baseUrlApi(`gb28181/zlm/nodes/${nodeId}/streams/snapshot`)}?${params.toString()}`;
}

export const fetchZLMStreamSnapshot = (nodeId: number, media: ZLMMediaIdentity, signal?: AbortSignal) =>
  http.request<Blob>("get", baseUrlApi(`gb28181/zlm/nodes/${nodeId}/streams/snapshot`), {
    params: mediaParams(media),
    responseType: "blob",
    ...(signal ? { signal } : {})
  });

export const preflightCloseZLMStream = (nodeId: number, media: ZLMMediaIdentity) =>
  http.request<BaseResult<ZLMStreamClosePreflight>>("post", baseUrlApi(`gb28181/zlm/nodes/${nodeId}/streams/close/preflight`), {
    data: target(nodeId, media)
  });

export const closeZLMStream = (nodeId: number, media: ZLMMediaIdentity, fingerprint: string) =>
  http.request<BaseResult<ZLMStreamCloseResult>>("post", baseUrlApi(`gb28181/zlm/nodes/${nodeId}/streams/close`), {
    data: { target: target(nodeId, media), fingerprint }
  });

export const forceCloseZLMStream = (nodeId: number, media: ZLMMediaIdentity, fingerprint: string, reason: string) =>
  http.request<BaseResult<ZLMStreamCloseResult>>("post", baseUrlApi(`gb28181/zlm/nodes/${nodeId}/streams/force-close`), {
    data: { target: target(nodeId, media), fingerprint, reason }
  });

export const preflightCloseZLMStreams = (targets: ZLMOwnershipTarget[]) =>
  http.request<BaseResult<ZLMOwnershipBatchPreflight>>(
    "post",
    baseUrlApi(`gb28181/zlm/nodes/${targets[0]?.nodeId ?? 0}/streams/close/batch/preflight`),
    { data: { targets } }
  );

export const closeZLMStreams = (preflight: ZLMOwnershipBatchPreflight) =>
  http.request<BaseResult<{ results: ZLMStreamCloseResult[]; closed: number; alreadyAbsent: number; partial: boolean; uncertain: boolean }>>(
    "post",
    baseUrlApi(`gb28181/zlm/nodes/${preflight.targets[0]?.nodeId ?? 0}/streams/close/batch`),
    { data: preflight }
  );

export interface ZLMNetworkSessionQuery extends ZLMPageQuery {
  localPort?: number;
  peerIp?: string;
}

export const listZLMNetworkSessions = (nodeId: number, query: ZLMNetworkSessionQuery = {}, signal?: AbortSignal) =>
  http.request<BaseResult<ZLMNetworkSessionPage>>(
    "get",
    baseUrlApi(`gb28181/zlm/nodes/${nodeId}/sessions/network`),
    getOptions(signal, query)
  );

export const listZLMMediaViewers = (nodeId: number, media: ZLMMediaIdentity, page: ZLMPageQuery = {}, signal?: AbortSignal) =>
  http.request<BaseResult<ZLMStreamViewerPage>>(
    "get",
    baseUrlApi(`gb28181/zlm/nodes/${nodeId}/sessions/viewers`),
    getOptions(signal, { ...mediaParams(media), ...page })
  );

export const kickZLMSession = (nodeId: number, media: ZLMMediaIdentity, identifier: string) =>
  http.request<BaseResult<{ nodeId: number; media: ZLMMediaIdentity; identifier: string; kicked: boolean; alreadyDisconnected: boolean; uncertain: boolean; retryable: boolean }>>(
    "post",
    baseUrlApi(`gb28181/zlm/nodes/${nodeId}/sessions/kick`),
    { data: { nodeId, media, identifier } }
  );

export const getZLMRecordingStatus = (nodeId: number, media: ZLMMediaIdentity, type: ZLMRecorderType, signal?: AbortSignal) =>
  http.request<BaseResult<ZLMRecordingResult>>(
    "get",
    baseUrlApi(`gb28181/zlm/nodes/${nodeId}/recordings/runtime/status`),
    getOptions(signal, { ...mediaParams(media), type })
  );

function recordingBody(nodeId: number, request: Omit<ZLMRecordingRequest, "target"> & { media: ZLMMediaIdentity }) {
  const { media, ...rest } = request;
  return { target: target(nodeId, media), ...rest };
}

export const preflightStartZLMRecording = (nodeId: number, request: Omit<ZLMRecordingRequest, "target"> & { media: ZLMMediaIdentity }) =>
  http.request<BaseResult<ZLMRecordingPreflight>>("post", baseUrlApi(`gb28181/zlm/nodes/${nodeId}/recordings/runtime/start/preflight`), {
    data: recordingBody(nodeId, request)
  });

export const startZLMRecording = (nodeId: number, request: Omit<ZLMRecordingRequest, "target"> & { media: ZLMMediaIdentity }) =>
  http.request<BaseResult<ZLMRecordingResult>>("post", baseUrlApi(`gb28181/zlm/nodes/${nodeId}/recordings/runtime/start`), {
    data: recordingBody(nodeId, request)
  });

export const preflightStopZLMRecording = (nodeId: number, request: Omit<ZLMRecordingRequest, "target"> & { media: ZLMMediaIdentity }) =>
  http.request<BaseResult<ZLMRecordingPreflight>>("post", baseUrlApi(`gb28181/zlm/nodes/${nodeId}/recordings/runtime/stop/preflight`), {
    data: recordingBody(nodeId, request)
  });

export const stopZLMRecording = (nodeId: number, request: Omit<ZLMRecordingRequest, "target"> & { media: ZLMMediaIdentity }) =>
  http.request<BaseResult<ZLMRecordingResult>>("post", baseUrlApi(`gb28181/zlm/nodes/${nodeId}/recordings/runtime/stop`), {
    data: recordingBody(nodeId, request)
  });

export const forceStopZLMRecording = (
  nodeId: number,
  request: Omit<ZLMRecordingRequest, "target"> & { media: ZLMMediaIdentity; reason: string }
) => http.request<BaseResult<ZLMRecordingResult>>("post", baseUrlApi(`gb28181/zlm/nodes/${nodeId}/recordings/runtime/force-stop`), {
  data: recordingBody(nodeId, request)
});

export const preflightForceStopZLMRecording = (
  nodeId: number,
  request: Omit<ZLMRecordingRequest, "target"> & { media: ZLMMediaIdentity; reason: string }
) => http.request<BaseResult<ZLMRecordingPreflight>>(
  "post",
  baseUrlApi(`gb28181/zlm/nodes/${nodeId}/recordings/runtime/force-stop/preflight`),
  { data: recordingBody(nodeId, request) }
);
