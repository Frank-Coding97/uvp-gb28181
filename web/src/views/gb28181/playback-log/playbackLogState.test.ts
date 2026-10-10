import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";
import {
  createPlaybackLogQuery,
  PLAYBACK_LOG_PAGE_SIZE,
  playbackDictionaryLabel,
  playbackDictionaryOptions,
  playbackStateTone,
  PLAYBACK_CLIENT_STATE_DICT_CODE,
  PLAYBACK_EVENT_DICT_CODE,
  PLAYBACK_EVENT_SOURCE_DICT_CODE,
  PLAYBACK_FACT_STATE_DICT_CODE,
  PLAYBACK_LIFECYCLE_STATE_DICT_CODE,
  PLAYBACK_MEDIA_STATE_DICT_CODE,
  PLAYBACK_STAGE_DICT_CODE
} from "./playbackLogState";

describe("playback log state", () => {
  const seedRoot = resolve(process.cwd(), "../server/resource/database/baseline/seeds");
  const dictionaries = readFileSync(resolve(seedRoot, "sys_dict.jsonl"), "utf8")
    .split("\n")
    .filter(Boolean)
    .map(line => JSON.parse(line) as { id: number; code: string });
  const dictionaryItems = readFileSync(resolve(seedRoot, "sys_dict_item.jsonl"), "utf8")
    .split("\n")
    .filter(Boolean)
    .map(line => JSON.parse(line) as { name: string; value: string; status: number; dict_id: number });

  it("defaults pagination to ten rows", () => {
    expect(PLAYBACK_LOG_PAGE_SIZE).toBe(10);
    expect(createPlaybackLogQuery({}, 1, PLAYBACK_LOG_PAGE_SIZE)).toMatchObject({ page: 1, pageSize: 10 });
  });

  it("uses the shared system dictionary codes for every playback lifecycle enum", () => {
    expect([
      PLAYBACK_MEDIA_STATE_DICT_CODE,
      PLAYBACK_CLIENT_STATE_DICT_CODE,
      PLAYBACK_STAGE_DICT_CODE,
      PLAYBACK_LIFECYCLE_STATE_DICT_CODE,
      PLAYBACK_FACT_STATE_DICT_CODE,
      PLAYBACK_EVENT_SOURCE_DICT_CODE,
      PLAYBACK_EVENT_DICT_CODE
    ]).toEqual([
      "playback_media_state",
      "playback_client_state",
      "playback_stage",
      "playback_lifecycle_state",
      "playback_fact_state",
      "playback_event_source",
      "playback_event"
    ]);
  });

  it("seeds every lifecycle enum value in the system dictionary baseline", () => {
    const expectedValues: Record<string, string[]> = {
      [PLAYBACK_MEDIA_STATE_DICT_CODE]: ["ready", "failed", "stopped", "unknown"],
      [PLAYBACK_CLIENT_STATE_DICT_CODE]: ["first_frame", "failed", "unknown"],
      [PLAYBACK_STAGE_DICT_CODE]: [
        "unknown",
        "request",
        "validation",
        "node",
        "rtp",
        "invite",
        "media",
        "authorization",
        "client",
        "stop",
        "cleanup"
      ],
      [PLAYBACK_LIFECYCLE_STATE_DICT_CODE]: ["in_progress", "completed", "failed", "stale_in_progress"],
      [PLAYBACK_FACT_STATE_DICT_CODE]: ["unknown", "in_progress", "confirmed", "failed", "not_applicable"],
      [PLAYBACK_EVENT_SOURCE_DICT_CODE]: ["http", "play_service", "sip", "zlm_hook", "client", "retention"],
      [PLAYBACK_EVENT_DICT_CODE]: [
        "request_received",
        "validation_succeeded",
        "validation_failed",
        "node_selected",
        "node_selection_failed",
        "rtp_allocated",
        "rtp_allocation_failed",
        "rtp_not_applicable",
        "invite_sent",
        "invite_accepted",
        "invite_rejected",
        "invite_timeout",
        "invite_not_applicable",
        "media_ready",
        "reuse_media_ready",
        "media_timeout",
        "play_url_issued",
        "authorization_failed",
        "first_frame",
        "player_error",
        "stop_requested",
        "cleanup_completed",
        "cleanup_partial_failure",
        "stale_in_progress",
        "hook_stream_registered",
        "hook_stream_unregistered",
        "hook_flow_reported",
        "hook_none_reader",
        "hook_rtp_timeout"
      ]
    };
    const dictionaryIDByCode = new Map(dictionaries.map(dictionary => [dictionary.code, dictionary.id]));

    for (const [code, values] of Object.entries(expectedValues)) {
      const dictID = dictionaryIDByCode.get(code);
      expect(dictID, `dictionary ${code}`).toBeDefined();
      expect(
        dictionaryItems.filter(item => item.dict_id === dictID && item.status !== 0).map(item => item.value),
        `values for ${code}`
      ).toEqual(values);
    }
  });

  it("builds active dictionary options and preserves unknown values", () => {
    const items = [
      { name: "媒体已就绪", value: "ready", status: 1 },
      { name: "媒体失败", value: "failed", status: 1 },
      { name: "隐藏项", value: "hidden", status: 0 }
    ];
    expect(playbackDictionaryOptions(items)).toEqual([
      { label: "媒体已就绪", value: "ready" },
      { label: "媒体失败", value: "failed" }
    ]);
    expect(playbackDictionaryLabel(items, "ready")).toBe("媒体已就绪");
    expect(playbackDictionaryLabel(items, "future_state")).toBe("future_state");
  });

  it("aligns playback status tones with the media workbench palette", () => {
    expect(playbackStateTone("ready")).toBe("success");
    expect(playbackStateTone("first_frame")).toBe("success");
    expect(playbackStateTone("failed")).toBe("danger");
    expect(playbackStateTone("stopped")).toBe("neutral");
    expect(playbackStateTone("unknown")).toBe("neutral");
  });
});
