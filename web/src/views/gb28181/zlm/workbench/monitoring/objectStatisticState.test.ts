import { describe, expect, it } from "vitest";
import type { ZLMObjectStatistics } from "@/api/gb28181-zlm-runtime";

import { objectStatisticTrend } from "./objectStatisticState";

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

describe("object statistic history", () => {
  it("reads object trends returned by the backend", () => {
    expect(objectStatisticTrend([
      { sampledAt: 1, objectStatistics: statistics(3) },
      { sampledAt: 2 },
      { sampledAt: 3, objectStatistics: statistics(5) }
    ], "mediaSource")).toEqual([3, 5]);
  });
});
