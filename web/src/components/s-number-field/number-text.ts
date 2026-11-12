// 数值输入的纯校验逻辑(硬规则 2):不用 a-input-number 的静默 clamp,
// 用户输错时让错误可见,由调用方决定是否阻断保存。
export interface NumberFieldOptions {
  required: boolean;
  integer: boolean;
  min: number;
  max: number;
}

export function parseNumberText(raw: string, integer: boolean): number | null {
  const trimmed = raw.trim();
  if (!trimmed) return null;
  if (integer && !/^-?\d+$/.test(trimmed)) return null;
  const parsed = integer ? Number.parseInt(trimmed, 10) : Number(trimmed);
  return Number.isFinite(parsed) ? parsed : null;
}

export function validateNumberText(raw: string, options: NumberFieldOptions): string {
  const trimmed = raw.trim();
  if (!trimmed) return options.required ? "该项为必填项" : "";
  const parsed = parseNumberText(trimmed, options.integer);
  if (parsed === null) return options.integer ? "请输入整数" : "请输入数值";
  if (parsed < options.min || parsed > options.max) return `范围为 ${options.min} ~ ${options.max}`;
  return "";
}
