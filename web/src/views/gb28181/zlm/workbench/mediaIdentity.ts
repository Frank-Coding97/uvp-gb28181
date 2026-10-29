import type { ZLMRuntimeMedia } from "@/api/gb28181-zlm-runtime";

/** ZLM exposes one logical source once per protocol; KPI counts collapse those variants. */
export function logicalMediaCount(streams: readonly ZLMRuntimeMedia[]): number {
  const identities = new Set(
    streams.map(stream => JSON.stringify([stream.nodeId, stream.media.vhost, stream.media.app, stream.media.stream]))
  );
  return identities.size;
}
