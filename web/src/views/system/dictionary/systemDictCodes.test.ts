import { describe, expect, it } from "vitest";
import { SYSTEM_DICT_CODES, SYSTEM_DICT_READONLY_HINT, isSystemDict } from "./systemDictCodes";

describe("系统内置字典名单", () => {
  it("锁定名单：增删都要同步这个用例（防无意改动）", () => {
    expect([...SYSTEM_DICT_CODES].sort()).toEqual(
      [
        "ptz_type",
        "gb28181_playback_protocol",
        "video_format",
        "video_resolution",
        "bit_rate_type",
        "channel_room_type",
        "channel_supply_light_type",
        "channel_direction_type",
        "channel_position_type",
        "channel_use_type",
        "channel_photoelectric_imaging_type"
      ].sort()
    );
  });

  it("无重复项", () => {
    expect(new Set(SYSTEM_DICT_CODES).size).toBe(SYSTEM_DICT_CODES.length);
  });

  it("判定：名单内为 true，名单外为 false", () => {
    expect(isSystemDict("ptz_type")).toBe(true);
    expect(isSystemDict("channel_room_type")).toBe(true);
    expect(isSystemDict("gb28181_playback_protocol")).toBe(true);
    expect(isSystemDict("post")).toBe(false);
    expect(isSystemDict("my_custom_dict")).toBe(false);
  });

  it("入参宽松：非字符串一律 false（不许抛错）", () => {
    for (const bad of [undefined, null, 0, 1, true, {}, []]) {
      expect(isSystemDict(bad)).toBe(false);
    }
  });

  it("⛔ 刻意排除的 code 不得进名单（防误伤业务字典 / 纯展示字典）", () => {
    for (const code of [
      "post",
      "gender",
      "status",
      "taskStatus",
      "device_status",
      "media_node_state",
      "cascade_register_state",
      "playback_media_state"
    ]) {
      expect(isSystemDict(code)).toBe(false);
    }
  });

  it("提示文案非空", () => {
    expect(SYSTEM_DICT_READONLY_HINT.length).toBeGreaterThan(0);
  });
});
