/**
 * 实时日志控制台的「一行日志」呈现规则。
 *
 * 与后端 console 编码器（server/app/utils/logging/console_encoder.go）保持同样的取舍：
 * 标识类字段稳定排在前面，人眼扫读时位置固定；进程常量不显示；机器字段沉到最后并降低对比度。
 * 字段一个都不丢——完整结构仍可通过「复制完整 JSON」拿到，搜索也照常命中。
 *
 * 抽成纯函数是为了不依赖登录态就能验证排序与转义规则；组件只负责画。
 */

export interface DisplayField {
  key: string;
  display: string;
  muted: boolean;
}

export interface LogLineInput {
  /** 已格式化好的时间串（格式化交给调用方，本模块不引入日期库）。 */
  time: string;
  level: string;
  module?: string;
  message?: string;
  event?: string;
  stack?: string;
  fields?: Record<string, unknown>;
}

/**
 * 字段排序表，逐项对应后端 `consoleFieldOrder`
 * （server/app/utils/logging/console_encoder.go），并补上 camelCase 别名。
 * 两边的相对顺序必须一致，否则「页面看到的」和「文件里 grep 到的」会排版漂移。
 */
export const FIELD_ORDER = [
  "device_id", "deviceId",
  "channel_id", "channelId",
  "platform_id",
  "node_id", "nodeId",
  "stream_id", "streamId",
  "call_id", "callId",
  "request_id", "requestId",
  "client_request_id", "clientRequestId",
  "execution_id", "executionId",
  "operation_id", "operationId",
  "correlation_id", "correlationId",
  "sn",
  "event",
  "stage",
  "outcome",
  "reason_code", "reasonCode",
  "error", "error_class", "errorCode",
  "duration_ms", "durationMs",
  "transport"
];

/** 机器字段：只影响「是否渲染得更淡」，不改变顺序。 */
export const MACHINE_FIELDS = new Set([
  "event", "stage", "outcome", "reason_code", "reasonCode", "error_class", "errorCode",
  "transport", "cseq", "complete", "is_first"
]);

/** 进程常量：整条日志流里每行都一样，显示出来只是噪声。 */
export const CONSTANT_FIELDS = new Set(["service", "version", "instance"]);

const fieldRank = new Map(FIELD_ORDER.map((key, index) => [key, index]));

/** 含空白、引号、反斜杠或 `=` 的值需要加引号，否则复制出去无法解析。 */
export function needsQuote(value: string): boolean {
  return value === "" || /[\s"\\=]/.test(value);
}

export function formatFieldValue(value: unknown): string {
  if (value === null || value === undefined) return "";
  if (typeof value === "string") return value;
  if (typeof value === "object") return JSON.stringify(value);
  return String(value);
}

export function displayFields(fields?: Record<string, unknown>): DisplayField[] {
  if (!fields) return [];
  return Object.keys(fields)
    .filter(key => !CONSTANT_FIELDS.has(key))
    .map(key => {
      const value = formatFieldValue(fields[key]);
      return { key, display: needsQuote(value) ? JSON.stringify(value) : value, muted: MACHINE_FIELDS.has(key) };
    })
    .sort((left, right) => {
      const lr = fieldRank.get(left.key);
      const rr = fieldRank.get(right.key);
      if (lr !== undefined && rr !== undefined) return lr - rr;
      if (lr !== undefined) return -1;
      if (rr !== undefined) return 1;
      return left.key.localeCompare(right.key);
    });
}

export function fieldText(field: DisplayField): string {
  return `${field.key}=${field.display}`;
}

/**
 * 与后端 console 编码器同版式，保证「页面看到的」和「文件里 grep 到的」是同一条文本。
 * 组件内直接用同一份规则渲染 DOM，下载/复制也走这里，避免两处写法漂移。
 */
export function renderLogLine(input: LogLineInput): string {
  const fields = displayFields(input.fields).map(fieldText).join(" ");
  const head = `${input.time} ${input.level.toUpperCase().padEnd(5)} ${(input.module || "app").padEnd(20)} ${input.message || input.event || ""}`;
  const line = fields ? `${head}  ${fields}` : head;
  return input.stack ? `${line}\n${input.stack}` : line;
}
