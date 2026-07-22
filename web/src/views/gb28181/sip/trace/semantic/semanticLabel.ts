import { parseGbXml, type GbCmdType } from "./xmlParser";

export type SemanticCategory =
    | "register"
    | "keepalive"
    | "catalog"
    | "device-info"
    | "device-status"
    | "alarm"
    | "ptz"
    | "record-info"
    | "invite-play"
    | "invite-playback"
    | "invite-download"
    | "bye"
    | "subscribe"
    | "notify"
    | "other";

export interface SemanticLabel {
    category: SemanticCategory;
    label: string;
    cmdType?: GbCmdType;
    sessionKind?: "Play" | "Playback" | "Download";
}

const CATEGORY_LABEL: Record<SemanticCategory, string> = {
    register: "设备注册",
    keepalive: "心跳",
    catalog: "目录",
    "device-info": "设备信息",
    "device-status": "设备状态",
    alarm: "报警上报",
    ptz: "云台控制",
    "record-info": "录像查询",
    "invite-play": "实时点播",
    "invite-playback": "历史回放",
    "invite-download": "录像下载",
    bye: "会话终止",
    subscribe: "订阅",
    notify: "通知",
    other: "其他"
};

export interface SemanticInput {
    method?: string;
    body?: string | null;
}

function pickSessionKind(sdp: string): "Play" | "Playback" | "Download" | null {
    const match = sdp.match(/^\s*s\s*=\s*(.+)$/m);
    if (!match) return null;
    const s = match[1].trim();
    if (/^Playback$/i.test(s)) return "Playback";
    if (/^Download$/i.test(s)) return "Download";
    if (/^Play$/i.test(s)) return "Play";
    return null;
}

function looksLikeGbXml(body: string): boolean {
    return /<\s*(Notify|Query|Response|Control)[\s>]/i.test(body);
}

/**
 * Map a SIP message to a business-facing semantic label.
 * Precedence:
 *   1. method-shortcut for REGISTER / BYE / SUBSCRIBE
 *   2. MESSAGE / NOTIFY → parse GB28181 XML CmdType
 *   3. INVITE / ACK → look at SDP `s=` line for Play / Playback / Download
 *   4. fallback: `other`
 */
export function classifyMessage(input: SemanticInput): SemanticLabel {
    const method = (input.method || "").toUpperCase();
    const body = input.body ?? "";

    switch (method) {
        case "REGISTER":
            return { category: "register", label: CATEGORY_LABEL.register };
        case "BYE":
            return { category: "bye", label: CATEGORY_LABEL.bye };
        case "SUBSCRIBE":
            return { category: "subscribe", label: CATEGORY_LABEL.subscribe };
    }

    if ((method === "MESSAGE" || method === "NOTIFY") && body && looksLikeGbXml(body)) {
        const parsed = parseGbXml(body);
        const cmdType = parsed.cmdType;
        switch (cmdType) {
            case "Keepalive":
                return { category: "keepalive", label: CATEGORY_LABEL.keepalive, cmdType };
            case "Catalog":
                return { category: "catalog", label: CATEGORY_LABEL.catalog, cmdType };
            case "DeviceInfo":
                return { category: "device-info", label: CATEGORY_LABEL["device-info"], cmdType };
            case "DeviceStatus":
                return { category: "device-status", label: CATEGORY_LABEL["device-status"], cmdType };
            case "Alarm":
                return { category: "alarm", label: CATEGORY_LABEL.alarm, cmdType };
            case "DeviceControl":
                return { category: "ptz", label: CATEGORY_LABEL.ptz, cmdType };
            case "RecordInfo":
                return { category: "record-info", label: CATEGORY_LABEL["record-info"], cmdType };
        }
        if (method === "NOTIFY") return { category: "notify", label: CATEGORY_LABEL.notify };
    }

    if (method === "INVITE" || method === "ACK") {
        const kind = body ? pickSessionKind(body) : null;
        if (kind === "Playback") return { category: "invite-playback", label: CATEGORY_LABEL["invite-playback"], sessionKind: kind };
        if (kind === "Download") return { category: "invite-download", label: CATEGORY_LABEL["invite-download"], sessionKind: kind };
        if (kind === "Play") return { category: "invite-play", label: CATEGORY_LABEL["invite-play"], sessionKind: kind };
        // 非合规 SDP 或缺 s= 时,默认按实时点播兜底(spec §6 已声明可能误标)
        return { category: "invite-play", label: CATEGORY_LABEL["invite-play"] };
    }

    if (method === "NOTIFY") return { category: "notify", label: CATEGORY_LABEL.notify };

    return { category: "other", label: method || CATEGORY_LABEL.other };
}

export function labelForCategory(cat: SemanticCategory): string {
    return CATEGORY_LABEL[cat];
}
