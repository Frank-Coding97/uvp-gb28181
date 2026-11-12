/**
 * 设备详情「上报字符集」的展示口径（与后端 `manscdp.SniffIncomingCharset` 的语义一致）。
 *
 * 两个值的含义（后端 `gb_device` 表）：
 *   - `reportedCharset`：设备 MESSAGE 报文里 XML 声明的字符集（空 = 未声明或未上报过）；
 *   - `wireCharset`：平台按「内容优先」实际判定的解码字符集。
 *
 * ⚠️ 设备的字符集声明并不可信（线上有「声明 GB18030、内容 UTF-8」的报障设备，
 * 见后端 `charset_sniff.go`）—— 声明与实际不一致时**两个都展示**，让排障的人
 * 一眼看出这台设备在撒谎，而不是只给一个看起来正常的值。
 */
export function reportedCharsetText(declared?: string | null, wire?: string | null): string {
  const declaredText = (declared || "").trim();
  const wireText = (wire || "").trim();
  if (!declaredText && !wireText) return "未上报";
  if (!declaredText) return `未声明（实测 ${wireText}）`;
  if (wireText && wireText !== declaredText) return `${declaredText}（实测按 ${wireText} 解）`;
  return declaredText;
}
