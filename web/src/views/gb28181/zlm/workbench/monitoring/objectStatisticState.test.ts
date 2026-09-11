import { describe, expect, it } from "vitest";
import type { ZLMNodeRuntime, ZLMObjectStatistics } from "@/api/gb28181-zlm-runtime";

import {
  appendObjectStatisticSample,
  objectStatisticTrend,
  type ObjectStatisticSample
} from "./objectStatisticState";

const statistics = (value: number): ZLMObjectStatistics => ({
  mediaSource: value,
  multiMediaSourceMuxer: value + 1,
  tcpServer: value + 2,
  tcpSession: value + 3,
  udpServer: value + 4,
  udpSession: value + 5,
  tcpClient: value + 6,
  socket: value + 7,
  frameImp: value + 8,
  frame: value + 9,
  buffer: value + 10,
  bufferRaw: value + 11,
  bufferLikeString: value + 12,
  bufferList: value + 13,
  rtpPacket: value + 14,
  rtmpPacket: value + 15
});

const runtime = (index: number, complete = true): ZLMNodeRuntime => ({
  nodeId: 2,
  name: "zlm-a",
  state: "active",
  status: complete ? "fresh" : "partial",
  freshness: "fresh",
  asOf: new Date(Date.UTC(2026, 7, 30, 0, 0, index)).toISOString(),
  heartbeatFreshness: "fresh",
  metrics: {
    mediaSourceCount: 0,
    multiMediaSourceMuxerCount: 0,
    tcpServerCount: 0,
    tcpSessionCount: 0,
    udpServerCount: 0,
    udpSessionCount: 0,
    tcpClientCount: 0,
    socketCount: 0,
    networkSessionCount: 0,
    netThreadLoad: 0,
    workThreadLoad: 0,
    objectStatistics: complete ? statistics(index) : undefined
  },
  metricsComplete: complete,
  mediaFreshness: "fresh"
});

describe("object statistic history", () => {
  it("records only complete object-statistic samples and never fabricates zeroes", () => {
    const initial = appendObjectStatisticSample([], runtime(1));
    const afterPartial = appendObjectStatisticSample(initial, runtime(2, false));

    expect(afterPartial).toBe(initial);
    expect(objectStatisticTrend(afterPartial, "mediaSource")).toEqual([1]);
  });

  it("replaces duplicate timestamps and keeps the newest 60 samples", () => {
    let history: ObjectStatisticSample[] = [];
    for (let index = 0; index <= 60; index += 1) {
      history = appendObjectStatisticSample(history, runtime(index));
    }
    history = appendObjectStatisticSample(history, runtime(60));

    expect(history).toHaveLength(60);
    expect(history[0].statistics.socket).toBe(8);
    expect(history.at(-1)?.statistics.socket).toBe(67);
    expect(objectStatisticTrend(history, "socket")).toHaveLength(60);
  });
});
