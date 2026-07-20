export type GbCmdType =
    | "Catalog"
    | "DeviceStatus"
    | "DeviceInfo"
    | "Alarm"
    | "Keepalive"
    | "RecordInfo"
    | "DeviceControl"
    | "MobilePosition"
    | "Unknown";

export interface CatalogDeviceItem {
    deviceId: string;
    name: string;
    manufacturer: string;
    model: string;
    status: string;
    parental: string;
    parentId: string;
    civilCode: string;
}

export interface CatalogPayload {
    kind: "Catalog";
    sumNum: number;
    deviceList: CatalogDeviceItem[];
}

export interface DeviceStatusPayload {
    kind: "DeviceStatus";
    deviceId: string;
    online: boolean;
    status: string;
    result: string;
    encode: string;
    record: string;
    deviceTime: string;
}

export interface DeviceInfoPayload {
    kind: "DeviceInfo";
    deviceId: string;
    manufacturer: string;
    model: string;
    firmware: string;
    channel: string;
}

export interface AlarmPayload {
    kind: "Alarm";
    deviceId: string;
    alarmPriority: string;
    alarmMethod: string;
    alarmTime: string;
    alarmDescription: string;
}

export interface KeepalivePayload {
    kind: "Keepalive";
    deviceId: string;
    status: string;
}

export interface RawPayload {
    kind: "Raw";
    body: Record<string, string>;
}

export type GbXmlPayload =
    | CatalogPayload
    | DeviceStatusPayload
    | DeviceInfoPayload
    | AlarmPayload
    | KeepalivePayload
    | RawPayload;

export interface GbXmlParseResult {
    ok: boolean;
    error?: "xml-parse-failed" | "no-root";
    root?: string; // Notify / Query / Response / Control
    cmdType: GbCmdType;
    sn: string;
    deviceId: string;
    payload: GbXmlPayload;
}

const KNOWN_ROOTS = new Set(["Notify", "Query", "Response", "Control"]);

function firstText(el: Element | Document | null, tag: string): string {
    if (!el) return "";
    const node = el.querySelector(tag);
    return node?.textContent?.trim() ?? "";
}

function parseCatalog(root: Element): CatalogPayload {
    const items = Array.from(root.querySelectorAll("DeviceList > Item"));
    const deviceList: CatalogDeviceItem[] = items.map((item) => ({
        deviceId: firstText(item, "DeviceID"),
        name: firstText(item, "Name"),
        manufacturer: firstText(item, "Manufacturer"),
        model: firstText(item, "Model"),
        status: firstText(item, "Status"),
        parental: firstText(item, "Parental"),
        parentId: firstText(item, "ParentID"),
        civilCode: firstText(item, "CivilCode")
    }));
    return {
        kind: "Catalog",
        sumNum: Number.parseInt(firstText(root, "SumNum") || "0", 10) || deviceList.length,
        deviceList
    };
}

function parseDeviceStatus(root: Element): DeviceStatusPayload {
    return {
        kind: "DeviceStatus",
        deviceId: firstText(root, "DeviceID"),
        online: firstText(root, "Online").toUpperCase() === "ONLINE",
        status: firstText(root, "Status"),
        result: firstText(root, "Result"),
        encode: firstText(root, "Encode"),
        record: firstText(root, "Record"),
        deviceTime: firstText(root, "DeviceTime")
    };
}

function parseDeviceInfo(root: Element): DeviceInfoPayload {
    return {
        kind: "DeviceInfo",
        deviceId: firstText(root, "DeviceID"),
        manufacturer: firstText(root, "Manufacturer"),
        model: firstText(root, "Model"),
        firmware: firstText(root, "Firmware"),
        channel: firstText(root, "Channel")
    };
}

function parseAlarm(root: Element): AlarmPayload {
    return {
        kind: "Alarm",
        deviceId: firstText(root, "DeviceID"),
        alarmPriority: firstText(root, "AlarmPriority"),
        alarmMethod: firstText(root, "AlarmMethod"),
        alarmTime: firstText(root, "AlarmTime"),
        alarmDescription: firstText(root, "AlarmDescription")
    };
}

function parseKeepalive(root: Element): KeepalivePayload {
    return {
        kind: "Keepalive",
        deviceId: firstText(root, "DeviceID"),
        status: firstText(root, "Status")
    };
}

function fallbackPayload(root: Element): RawPayload {
    const body: Record<string, string> = {};
    for (const child of Array.from(root.children)) {
        if (child.children.length === 0) {
            body[child.tagName] = child.textContent?.trim() ?? "";
        }
    }
    return { kind: "Raw", body };
}

function normalizeCmdType(raw: string): GbCmdType {
    const known: GbCmdType[] = [
        "Catalog",
        "DeviceStatus",
        "DeviceInfo",
        "Alarm",
        "Keepalive",
        "RecordInfo",
        "DeviceControl",
        "MobilePosition"
    ];
    for (const c of known) {
        if (c.toLowerCase() === raw.toLowerCase()) return c;
    }
    return "Unknown";
}

/**
 * Parse GB28181 XML body extracted from a SIP MESSAGE / NOTIFY body.
 * Never throws — malformed input downgrades to `ok=false` + `error`.
 */
export function parseGbXml(xml: string | null | undefined): GbXmlParseResult {
    if (!xml || !xml.trim()) {
        return {
            ok: false,
            error: "xml-parse-failed",
            cmdType: "Unknown",
            sn: "",
            deviceId: "",
            payload: { kind: "Raw", body: {} }
        };
    }

    let doc: Document;
    try {
        doc = new DOMParser().parseFromString(xml, "application/xml");
    } catch {
        return {
            ok: false,
            error: "xml-parse-failed",
            cmdType: "Unknown",
            sn: "",
            deviceId: "",
            payload: { kind: "Raw", body: {} }
        };
    }

    if (doc.getElementsByTagName("parsererror").length > 0) {
        return {
            ok: false,
            error: "xml-parse-failed",
            cmdType: "Unknown",
            sn: "",
            deviceId: "",
            payload: { kind: "Raw", body: {} }
        };
    }

    const root = doc.documentElement;
    if (!root) {
        return {
            ok: false,
            error: "no-root",
            cmdType: "Unknown",
            sn: "",
            deviceId: "",
            payload: { kind: "Raw", body: {} }
        };
    }

    const cmdType = normalizeCmdType(firstText(root, "CmdType"));
    const sn = firstText(root, "SN");
    const deviceId = firstText(root, "DeviceID");

    let payload: GbXmlPayload;
    switch (cmdType) {
        case "Catalog":
            payload = parseCatalog(root);
            break;
        case "DeviceStatus":
            payload = parseDeviceStatus(root);
            break;
        case "DeviceInfo":
            payload = parseDeviceInfo(root);
            break;
        case "Alarm":
            payload = parseAlarm(root);
            break;
        case "Keepalive":
            payload = parseKeepalive(root);
            break;
        default:
            payload = fallbackPayload(root);
    }

    return {
        ok: true,
        root: KNOWN_ROOTS.has(root.tagName) ? root.tagName : root.tagName,
        cmdType,
        sn,
        deviceId,
        payload
    };
}
