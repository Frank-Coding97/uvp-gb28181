/**
 * 平台账号密码策略（前端唯一实现）。
 *
 * ⛔⛔ 后端同源实现在 `server/app/middleware/password.go` 的 PasswordValidatorMiddleware，
 *     改这里必须同步改后端，否则会出现「前端显示合规、后端却放过了弱密码」（或反过来）。
 *     与 SIP 接入密码（`views/gb28181/sip/sipSetupRules.ts`）是两套不同规则，别混用。
 *
 * 规则（三条，缺一不可）：
 *   ① 长度 ≥ 最少位数（取「系统配置 → 安全配置 → 密码最小长度」，默认 8）
 *   ② 同时包含大写和小写字母
 *   ③ 包含特殊字符（可在「安全配置 → 密码必须包含特殊字符」关闭）
 */

/** 配置未加载/未设置时使用的最小长度兜底值，与后端 config.example.yml 的默认值保持一致。 */
export const PASSWORD_MIN_LENGTH_FALLBACK = 8;

/** 平台要求的特殊字符集合，与后端 password.go 的正则保持一致。 */
export const PASSWORD_SPECIAL_CHARS = "!@#$%";

const SPECIAL_CHAR_PATTERN = /[!@#$%]/;
const UPPER_PATTERN = /[A-Z]/;
const LOWER_PATTERN = /[a-z]/;

export interface PasswordPolicyOptions {
  /** 最少位数；缺省或非正数时用 PASSWORD_MIN_LENGTH_FALLBACK。 */
  minLength?: number;
  /** 是否要求包含特殊字符；缺省视为要求。 */
  requireSpecialChar?: boolean;
}

export interface PasswordPolicyRule {
  /** 稳定标识，用于 v-for 的 :key 与单测断言。 */
  key: "length" | "case" | "special";
  /** 勾选清单里的正向描述。 */
  label: string;
  /** 未满足时的提示语，语义与后端返回的 message 一致。 */
  message: string;
  satisfied: boolean;
}

export function passwordMinLength(options: PasswordPolicyOptions = {}): number {
  const min = Number(options.minLength);
  return Number.isFinite(min) && min > 0 ? min : PASSWORD_MIN_LENGTH_FALLBACK;
}

export function buildPasswordPolicyRules(password: string, options: PasswordPolicyOptions = {}): PasswordPolicyRule[] {
  const min = passwordMinLength(options);
  const rules: PasswordPolicyRule[] = [
    {
      key: "length",
      label: `最少 ${min} 位`,
      message: `密码长度不能少于${min}位`,
      satisfied: password.length >= min
    },
    {
      key: "case",
      label: "包含大小写字母",
      message: "密码必须同时包含大写和小写字母",
      satisfied: UPPER_PATTERN.test(password) && LOWER_PATTERN.test(password)
    }
  ];
  if (options.requireSpecialChar !== false) {
    rules.push({
      key: "special",
      label: `包含特殊字符 ${PASSWORD_SPECIAL_CHARS}`,
      message: `密码必须包含特殊字符 ${PASSWORD_SPECIAL_CHARS}`,
      satisfied: SPECIAL_CHAR_PATTERN.test(password)
    });
  }
  return rules;
}

export function passwordPolicySatisfied(password: string, options: PasswordPolicyOptions = {}): boolean {
  return buildPasswordPolicyRules(password, options).every(rule => rule.satisfied);
}

/** 返回第一条未满足规则的提示语；全部满足时返回空字符串。 */
export function firstPasswordPolicyError(password: string, options: PasswordPolicyOptions = {}): string {
  return buildPasswordPolicyRules(password, options).find(rule => !rule.satisfied)?.message ?? "";
}
