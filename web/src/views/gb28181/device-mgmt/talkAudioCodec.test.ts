import { describe, expect, it } from "vitest";
import {
  DEFAULT_TALK_AUDIO_CODEC,
  normalizeTalkAudioCodec,
  TALK_AUDIO_CODEC_OPTIONS,
  TALK_AUDIO_CODECS,
  talkAudioCodecLabel
} from "./talkAudioCodec";

describe("对讲音频编码（pcma / pcmu）", () => {
  it("值域与默认值锁死：默认 G.711A", () => {
    expect(TALK_AUDIO_CODECS).toEqual(["pcma", "pcmu"]);
    expect(DEFAULT_TALK_AUDIO_CODEC).toBe("pcma");
    expect(TALK_AUDIO_CODEC_OPTIONS.map(option => option.value)).toEqual(["pcma", "pcmu"]);
  });

  it("大小写与空白都被归一", () => {
    expect(normalizeTalkAudioCodec("PCMU")).toBe("pcmu");
    expect(normalizeTalkAudioCodec(" G711U ")).toBe("pcmu");
    expect(normalizeTalkAudioCodec("pcma")).toBe("pcma");
  });

  it("认不出来的值回落 G.711A（不猜、不抛错）", () => {
    // 历史库/旧接口不会有这个字段，回落默认值才能保证老设备继续可用。
    for (const value of [undefined, null, "", "   ", "aac", "mpeg4-generic", "g729"]) {
      expect(normalizeTalkAudioCodec(value), String(value)).toBe("pcma");
    }
  });

  it("空值也有可展示的文案", () => {
    expect(talkAudioCodecLabel("pcmu")).toBe("G.711U（PCMU）");
    expect(talkAudioCodecLabel("pcma")).toBe("G.711A（PCMA）");
    expect(talkAudioCodecLabel(undefined)).toBe("G.711A（PCMA）");
  });
});
