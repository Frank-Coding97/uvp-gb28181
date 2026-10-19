// 出厂占位值 —— 必须与 server/config/config.example.yml 的 system 段**逐字相同**。
//
// ⛔ 这两个常量不是"给客户看的文案"，而是**识别标记**：一旦后端下发的值等于它们，
//   就说明客户还没配自己的品牌，页脚/登录页改走干净的 UVP 兜底文案，
//   而不是把 "20xxxx" / "xICP备xxxxxxxx号" 这种占位直接展示给最终用户。
//
// ⛔⛔ 改了 yml 里的占位串，必须同步改这里（含空格与全角字符）：
//   判据是字符串全等，差一个空格就识别失败 ⇒ 占位会原样露在页面上。
export const SYSTEM_COPYRIGHT_DEFAULT = "Copyright © 20xxxx-20xxxx xxxxxxxx科技有限公司版权所有";
export const SYSTEM_RECORD_NO_DEFAULT = "xICP备xxxxxxxx号";

const normalizeText = (value?: string) => value?.trim() || "";

export const isSeedSystemCopyright = (value?: string) => normalizeText(value) === SYSTEM_COPYRIGHT_DEFAULT;
export const isSeedSystemRecordNo = (value?: string) => normalizeText(value) === SYSTEM_RECORD_NO_DEFAULT;

export const getDisplaySystemCopyright = (value?: string) => {
  const text = normalizeText(value);
  return isSeedSystemCopyright(text) ? "© 2026 UVP 统一视频接入平台" : text || "© 2026 UVP 统一视频接入平台";
};

export const getDisplaySystemRecordNo = (value?: string) => {
  const text = normalizeText(value);
  return isSeedSystemRecordNo(text) ? "" : text;
};
