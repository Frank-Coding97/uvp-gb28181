/**
 * 对讲/广播的音频编码：G.711A（PCMA）/ G.711U（PCMU）。
 *
 * 由用户在设备编辑里手动维护（默认 G.711A）。为什么不能自动识别：
 * 设备的 SDP 是能力列表而不是「只认这一个」，同一个 m 行里可能同时列着
 * 两种 G.711，而平台的上行源在推流前就要定编码（ZLM 只按 PT 打包、不转码），
 * 所以必须由人先告诉平台「这台设备实际能收哪种」。
 *
 * 值域与后端 `models.NormalizeTalkAudioCodec` 逐字对齐（pcma / pcmu，小写）。
 */
export const TALK_AUDIO_CODECS = ["pcma", "pcmu"] as const;

export type TalkAudioCodec = (typeof TALK_AUDIO_CODECS)[number];

/** 新建 / 自动建档 / 历史数据（空串）的默认编码。 */
export const DEFAULT_TALK_AUDIO_CODEC: TalkAudioCodec = "pcma";

export const TALK_AUDIO_CODEC_OPTIONS: ReadonlyArray<{ label: string; value: TalkAudioCodec }> = [
  { label: "G.711A（PCMA，默认）", value: "pcma" },
  { label: "G.711U（PCMU）", value: "pcmu" }
];

// 与后端 `models.NormalizeTalkAudioCodec` 保持同一套别名：接口只发 pcma/pcmu，
// 但人手工填/老数据里可能出现 G711A 这类写法，前后端归一结果必须一致。
const TALK_AUDIO_CODEC_ALIASES: Record<string, TalkAudioCodec> = {
  pcma: "pcma",
  g711a: "pcma",
  g711_a: "pcma",
  "a-law": "pcma",
  alaw: "pcma",
  pcmu: "pcmu",
  g711u: "pcmu",
  g711_u: "pcmu",
  "u-law": "pcmu",
  ulaw: "pcmu"
};

/**
 * 归一化接口/存量数据里的编码值。
 *
 * 认不出来（含 null / 空串 / 历史脏数据）一律回落 G.711A：老库刚加列时全是默认值，
 * 这里再猜一次没有收益，反而可能把本来正常的设备切成另一种压扩表。
 */
export function normalizeTalkAudioCodec(raw?: string | null): TalkAudioCodec {
  const token = typeof raw === "string" ? raw.trim().toLowerCase() : "";
  return TALK_AUDIO_CODEC_ALIASES[token] || DEFAULT_TALK_AUDIO_CODEC;
}

/** 下拉框 / 提示文案用的完整名称。 */
export function talkAudioCodecLabel(raw?: string | null): string {
  return normalizeTalkAudioCodec(raw) === "pcmu" ? "G.711U（PCMU）" : "G.711A（PCMA）";
}
