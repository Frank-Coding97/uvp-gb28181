import type { SipDeploymentMode, SipNetworkAddress } from "@/api/gb28181";

export function isConcreteIPv4(value: string): boolean {
  const parts = value.split(".");
  if (parts.length !== 4 || value === "0.0.0.0" || value.startsWith("127.")) return false;
  return parts.every(part => /^\d{1,3}$/.test(part) && Number(part) >= 0 && Number(part) <= 255 && String(Number(part)) === part);
}

export function deriveNetworkSelection(
  mode: SipDeploymentMode,
  listenIp: string,
  advertiseIp: string,
  _items: SipNetworkAddress[]
): { listenIp: string; advertiseIp: string; advertiseIpInferred: boolean } {
  if (mode === "public") return { listenIp, advertiseIp, advertiseIpInferred: false };
  if (listenIp !== "0.0.0.0") return { listenIp, advertiseIp: listenIp, advertiseIpInferred: false };
  return { listenIp, advertiseIp: "", advertiseIpInferred: false };
}

export function networkCanContinue(mode: SipDeploymentMode | "", listenIp: string, advertiseIp: string): boolean {
  if (!mode) return false;
  const listenValid = listenIp === "0.0.0.0" || isConcreteIPv4(listenIp);
  if (!listenValid) return false;
  if (mode === "lan") return true;
  return isConcreteIPv4(advertiseIp);
}

export function networkOptions(
  items: SipNetworkAddress[],
  _currentIp: string
): Array<SipNetworkAddress & { unavailable?: boolean }> {
  return items;
}

export function activeSipAddresses(
  mode: SipDeploymentMode | "",
  listenIp: string,
  advertiseIp: string,
  items: SipNetworkAddress[]
): string[] {
  if (mode === "public") return isConcreteIPv4(advertiseIp) ? [advertiseIp] : [];
  if (listenIp !== "0.0.0.0") return isConcreteIPv4(listenIp) ? [listenIp] : [];

  const addresses: string[] = [];
  const seen = new Set<string>();
  for (const item of items) {
    if (item.loopback || item.listenOnly || !isConcreteIPv4(item.ip) || seen.has(item.ip)) continue;
    seen.add(item.ip);
    addresses.push(item.ip);
  }
  return addresses;
}

export function deriveDomain(serverId: string): string {
  return /^\d{10,}$/.test(serverId) ? serverId.slice(0, 10) : "";
}

// ===== 密码强度 =====
// 规则跟后端 validator.go checkPasswordStrength 一致:
//   - 长度 >= 8
//   - 至少含大写/小写/数字/特殊字符中的 3 类
//   - 不在常见弱口令黑名单
//   - 不为纯顺序序列
//
// GB28181 SIP 注册暴力破解频发,弱口令是主要入口.

const WEAK_PASSWORDS = new Set([
  "12345678",
  "123456789",
  "1234567890",
  "12345678901",
  "123456789012",
  "password",
  "passw0rd",
  "password123",
  "qwerty",
  "qwerty123",
  "admin",
  "admin123",
  "admin1234",
  "administrator",
  "root",
  "root123",
  "sipserver",
  "gb28181",
  "12345678a",
  "abc12345",
  "111111111111",
  "000000000000",
  "aaaaaaaaaaaa",
  "iloveyou"
]);

function isSequential(password: string): boolean {
  if (password.length < 8) return false;
  let ascending = true;
  let descending = true;
  for (let i = 1; i < password.length; i++) {
    if (password.charCodeAt(i) !== password.charCodeAt(i - 1) + 1) ascending = false;
    if (password.charCodeAt(i) !== password.charCodeAt(i - 1) - 1) descending = false;
    if (!ascending && !descending) return false;
  }
  return true;
}

export interface PasswordStrength {
  // level: 0=空/太弱,1=弱,2=中,3=强.只有 3 允许通过校验.
  level: 0 | 1 | 2 | 3;
  label: string;
  reason: string; // 空字符串表示合规
}

// 一次算清所有"事实",强度分级与「格式提示清单」共用 ——
// ⛔ 两者各算一套就会漂移(清单全打勾、按钮却点不动)。
interface SipPasswordFacts {
  filled: boolean;
  longEnough: boolean;
  categories: number; // 大写/小写/数字/特殊 命中几类
  weak: boolean;
  sequential: boolean;
}

function sipPasswordFacts(password: string): SipPasswordFacts {
  let hasUpper = false,
    hasLower = false,
    hasDigit = false,
    hasSpecial = false;
  for (const ch of password) {
    const code = ch.charCodeAt(0);
    if (code >= 65 && code <= 90) hasUpper = true;
    else if (code >= 97 && code <= 122) hasLower = true;
    else if (code >= 48 && code <= 57) hasDigit = true;
    else if (code > 32 && code < 127) hasSpecial = true;
  }
  return {
    filled: password.length > 0,
    longEnough: password.length >= 8,
    categories: [hasUpper, hasLower, hasDigit, hasSpecial].filter(Boolean).length,
    weak: WEAK_PASSWORDS.has(password),
    sequential: isSequential(password)
  };
}

export function evaluatePasswordStrength(password: string): PasswordStrength {
  if (!password) return { level: 0, label: "未填写", reason: "长度至少 8 位" };
  const facts = sipPasswordFacts(password);
  if (!facts.longEnough) return { level: 1, label: "太短", reason: "长度至少 8 位" };

  if (facts.categories < 3) {
    return { level: 1, label: "弱", reason: "需含大写字母、小写字母、数字、特殊字符中至少 3 类" };
  }
  if (facts.weak) {
    return { level: 1, label: "弱", reason: "该密码在常见弱口令列表中,易被暴力破解" };
  }
  if (facts.sequential) {
    return { level: 1, label: "弱", reason: "密码过于规律,请使用更随机的组合" };
  }
  // 满足所有强规则:进一步区分强/中,给用户视觉反馈.
  // 达到 4 类字符 + 长度 >=16 判"强";否则判"中"(依然合规,只是可以更好).
  if (facts.categories === 4 && password.length >= 16) {
    return { level: 3, label: "强", reason: "" };
  }
  return { level: 2, label: "合格", reason: "" };
}

export interface SipPasswordRule {
  /** 稳定标识,渲染成 data-rule,同时用作 v-for 的 :key。 */
  key: "length" | "categories" | "weak" | "sequence";
  label: string;
  satisfied: boolean;
}

/**
 * SIP 接入密码的「格式提示」清单 —— 与 `evaluatePasswordStrength` 共用同一份 facts,
 * 也和后端 `setup/validator.go checkPasswordStrength` 的分支一一对应。
 *
 * ⚠️ 密码框为空时**一律不点亮**(`filled && rule`):否则"不是弱口令"这种反向规则
 *    会在空框上先给一个绿对号,看起来像已经赢了。
 */
export function buildSipPasswordRules(password: string): SipPasswordRule[] {
  const facts = sipPasswordFacts(password);
  return [
    { key: "length", label: "最少 8 位", satisfied: facts.filled && facts.longEnough },
    {
      key: "categories",
      label: "包含大写字母、小写字母、数字、特殊字符中的至少 3 类",
      satisfied: facts.filled && facts.categories >= 3
    },
    { key: "weak", label: "不使用常见弱口令(如 12345678 / admin123)", satisfied: facts.filled && !facts.weak },
    { key: "sequence", label: "不是连续顺序字符(如 abcdefgh)", satisfied: facts.filled && !facts.sequential }
  ];
}

// 密码是否满足最低合规(能保存).合规 = level >= 2.
// 编辑场景下若用户未改密码(密码为空且已存在密码),则视为通过.
export function passwordAcceptable(password: string, hasExistingPassword: boolean): boolean {
  if (!password) return hasExistingPassword;
  const s = evaluatePasswordStrength(password);
  return s.level >= 2;
}

// ===== 字段级校验 =====
export interface FieldErrors {
  port?: string;
  serverId?: string;
  listenIp?: string;
  advertiseIp?: string;
  password?: string;
}

export function validateServerId(value: string): string {
  if (!value) return "必填";
  if (!/^\d{20}$/.test(value)) return "必须为 20 位数字";
  return "";
}

// 与后端 validator.go 的 `len(req.Domain) != 10 || !asciiDigits` 一一对应.
export function validateDomain(value: string): string {
  if (!value) return "必填";
  if (!/^\d{10}$/.test(value)) return "必须为 10 位数字";
  return "";
}

export function validatePort(value: number): string {
  if (!value || value < 1 || value > 65535) return "端口须在 1 - 65535 之间";
  return "";
}

export function validateAdvertiseIp(value: string, mode: SipDeploymentMode | ""): string {
  if (!value) return mode === "public" ? "请填写公网 IPv4 地址" : "请选择接入网卡";
  if (!isConcreteIPv4(value)) return "必须是合法的 IPv4 地址,不允许 0.0.0.0 或回环";
  return "";
}

export function identityCanContinue(
  port: number,
  serverId: string,
  domain: string,
  password: string,
  hasExistingPassword: boolean
): boolean {
  return (
    port >= 1 &&
    port <= 65535 &&
    /^\d{20}$/.test(serverId) &&
    /^\d{10}$/.test(domain) &&
    passwordAcceptable(password, hasExistingPassword)
  );
}

export function formatRegisterUri(serverId: string, advertiseIp: string, port: number): string {
  return serverId && isConcreteIPv4(advertiseIp) && port >= 1 && port <= 65535 ? `sip:${serverId}@${advertiseIp}:${port}` : "";
}
