/**
 * 首页「流媒体运行态」网络会话类型的展示口径（纯函数模块，⛔ 不 import vue/pinia）。
 *
 * 值来自 ZLM 的 `typeId`，标准形态带命名空间，如 `mediakit::RtpSession`；也有的
 * 接口只给短名（`RtpSession`），所以先补 `mediakit::` 前缀再查表。
 *
 * 属**纯展示**：只影响表格里那一列怎么写，不参与判定、也不下发 ⇒ 不进只读名单。
 * ⛔ 未命中时**回显原值**（不是空）—— 排障时要看得见 ZLM 到底报了什么类型。
 */
export const DICT_CODE_ZLM_SESSION_TYPE = "zlm_session_type";

/** 兜底口径，按 ZLM `mediakit::*` 已知类型写死。 */
export const ZLM_SESSION_TYPE_LABEL_FALLBACK: Readonly<Record<string, string>> = {
  "mediakit::HttpSession": "HTTP 会话",
  "mediakit::RtspSession": "RTSP 会话",
  "mediakit::RtmpSession": "RTMP 会话",
  "mediakit::RtpSession": "RTP 会话",
  "mediakit::SrtSession": "SRT 会话",
  "mediakit::WebSocketSession": "WebSocket 会话",
  "mediakit::WebRtcSession": "WebRTC 会话",
  "mediakit::TcpSession": "TCP 会话",
  "mediakit::UdpSession": "UDP 会话"
};

/** 空值话术（后端没给类型时）。 */
export const ZLM_SESSION_TYPE_EMPTY_TEXT = "未知";

/**
 * 会话类型 → 展示名。
 * `labels` 由注入层从字典取（见 `useZLMSessionTypeDict.ts`），纯函数侧默认用兜底表。
 */
export function zlmSessionTypeLabel(
  value: string | null | undefined,
  labels: Readonly<Record<string, string>> = ZLM_SESSION_TYPE_LABEL_FALLBACK
): string {
  const typeId = (value || "").trim();
  if (!typeId) return ZLM_SESSION_TYPE_EMPTY_TEXT;
  const qualifiedTypeId = typeId.includes("::") ? typeId : `mediakit::${typeId}`;
  return labels[qualifiedTypeId] ?? typeId;
}
