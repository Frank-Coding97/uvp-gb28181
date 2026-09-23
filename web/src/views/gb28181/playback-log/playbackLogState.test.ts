import { describe, expect, it } from "vitest";
import { createPlaybackLogQuery, factStateLabel, PLAYBACK_LOG_PAGE_SIZE } from "./playbackLogState";

describe("playback log state", () => {
  it("defaults pagination to ten rows", () => {
    expect(PLAYBACK_LOG_PAGE_SIZE).toBe(10);
    expect(createPlaybackLogQuery({}, 1, PLAYBACK_LOG_PAGE_SIZE)).toMatchObject({ page: 1, pageSize: 10 });
  });

  it("keeps confirmed, failed, in progress, unknown and not applicable distinct", () => {
    expect(["confirmed", "failed", "in_progress", "unknown", "not_applicable"].map(factStateLabel)).toEqual([
      "已证实",
      "失败",
      "进行中",
      "未知",
      "不适用"
    ]);
  });
});
