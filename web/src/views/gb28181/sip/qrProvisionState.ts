// 扫码回填 SIP 接入信息 —— 弹窗的纯逻辑.
// spec: wiki/projects/uvp-gb28181/specs/qr-sip-provisioning.md

// 平台引导页路径.系统相机扫码会 GET 这个地址,拿到一句话引导页;token 在 fragment 里不会发给服务端.
const QR_LANDING_PATH = "/gb28181/qr";

export function normalizeBaseUrl(input: string): string {
    return input.trim().replace(/\/+$/, "");
}

// 空串表示通过.校验失败时调用方不出码 —— 宁可不出码,不能出一个扫了会失败的码.
export function validateBaseUrl(input: string): string {
    const value = normalizeBaseUrl(input);
    if (!value) return "请填写平台访问地址";
    if (!/^https?:\/\//i.test(value)) return "必须以 http:// 或 https:// 开头";
    if (!/^https?:\/\/[^/\s]+/i.test(value)) return "地址格式不正确";
    return "";
}

// token 必须走 fragment(`#t=`)而不是 query(`?t=`):
// query 会随 GET 发到服务端,通用扫码 App 一扫就当场消费掉一次性 token.
export function buildQrUrl(baseUrl: string, token: string): string {
    return `${normalizeBaseUrl(baseUrl)}${QR_LANDING_PATH}#t=${token}`;
}

export function formatCountdown(secondsLeft: number): string {
    const total = Math.max(0, Math.floor(secondsLeft));
    const minutes = Math.floor(total / 60);
    const seconds = total % 60;
    return `${minutes}:${String(seconds).padStart(2, "0")}`;
}

// D10: 复用 gb28181:sip:config:view,不新增权限点 —— 能看明文密码的人才能生成接入码.
export function mayGenerateQr(permissions: string[]): boolean {
    return permissions.includes("*:*:*") || permissions.includes("gb28181:sip:config:view");
}
