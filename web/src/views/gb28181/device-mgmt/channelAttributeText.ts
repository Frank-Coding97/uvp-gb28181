/**
 * 设备上报的通道属性(GB/T 28181 附录 A / §9.3.1)在 UI 上的展示映射。
 *
 * 数据来自平台 `gb_channel` 表的十个属性列,由 Catalog 目录应答/NOTIFY 的
 * `Item` 层与 `<Info>` 容器解析落库。判定依据:
 * · 2016 附录 A:`<Info>` 内 PTZType(1-4)/PositionType/UseType/RoomType/
 *   SupplyLightType(1-3)/DirectionType/Resolution,`BusinessGroupID` 也在 `<Info>` 内。
 * · 2022 附录 A:PTZType 扩到 1-7 且类型从 integer 改 string,删掉 PositionType/UseType,
 *   新增 PhotoelectricImagingType/CapturePositionType/StreamNumberList/SSVCRatioSupportList,
 *   `BusinessGroupID` 提到 `Item` 层;SupplyLightType 新增 4-激光、9-其他。
 *
 * ⛔ 约定:0 / "" 一律表示"设备本次未上报该属性",**不是**"该属性为 0"。
 *    落库侧据此判断是否覆盖(见 `catalog/upsert.go`),展示侧据此显示"未上报"。
 * ⛔ RoomType / DirectionType 两版编码完全一致 → 共用一张表,不做版本分叉。
 *    SupplyLightType 的 1-3 两版一致,4/9 是 2022 新增 → 用并集表即可,不会误判。
 */

/** 属性未上报时的占位文案(与"值为 0"区分开)。 */
export const CHANNEL_ATTR_UNREPORTED = "未上报";

/** 2016/2022 编码一致:1-室外 2-室内。 */
const ROOM_TYPE_TEXT: Record<number, string> = {
    1: "室外",
    2: "室内",
};

/**
 * 1-无补光 2-红外补光 3-白光补光 两版一致;4-激光补光 9-其他 为 2022 新增。
 * 用并集表即可 —— 2016 设备不可能报出 4/9,2022 设备报 1-3 时含义也相同。
 */
const SUPPLY_LIGHT_TYPE_TEXT: Record<number, string> = {
    1: "无补光",
    2: "红外补光",
    3: "白光补光",
    4: "激光补光",
    9: "其他",
};

/** 2016/2022 编码一致:1-东 2-西 3-南 4-北 5-东南 6-东北 7-西南 8-西北。 */
const DIRECTION_TYPE_TEXT: Record<number, string> = {
    1: "东",
    2: "西",
    3: "南",
    4: "北",
    5: "东南",
    6: "东北",
    7: "西南",
    8: "西北",
};

/** 2016 独有:1-省际检查站 2-党政机关 3-车站码头 4-中心广场 5-体育场馆 6-商业中心 7-宗教场所 8-校园周边 9-治安复杂区域 10-交通干线。 */
const POSITION_TYPE_TEXT: Record<number, string> = {
    1: "省际检查站",
    2: "党政机关",
    3: "车站码头",
    4: "中心广场",
    5: "体育场馆",
    6: "商业中心",
    7: "宗教场所",
    8: "校园周边",
    9: "治安复杂区域",
    10: "交通干线",
};

/** 2016 独有:1-治安 2-交通 3-重点。 */
const USE_TYPE_TEXT: Record<number, string> = {
    1: "治安",
    2: "交通",
    3: "重点",
};

/**
 * 2022 独有:1-可见光成像 2-热成像 3-雷达成像 4-X光成像 5-深度光场成像 9-其他。
 * 标准允许**多值**、以英文半角 "/" 分隔 → 逐段翻译后原样用 "/" 拼回去。
 */
const PHOTOELECTRIC_IMAGING_TYPE_TEXT: Record<number, string> = {
    1: "可见光成像",
    2: "热成像",
    3: "雷达成像",
    4: "X光成像",
    5: "深度光场成像",
    9: "其他",
};

/** 2022 独有:采集部位类型,取值见 2022 附录 O。标准只要求"应符合附录 O",此处不硬编码中文名。 */

export interface ChannelAttributeSnapshot {
    roomType?: number | null;
    supplyLightType?: number | null;
    directionType?: number | null;
    resolution?: string | null;
    ipAddress?: string | null;
    port?: number | null;
    positionType?: number | null;
    useType?: number | null;
    photoelectricImagingType?: string | null;
    capturePositionType?: string | null;
}

export interface ChannelAttributeEntry {
    key: string;
    label: string;
    /** 已翻译的展示值;未上报时为 {@link CHANNEL_ATTR_UNREPORTED}。 */
    value: string;
    /** 设备是否真的上报了该属性。false 时 UI 可用弱化样式,并据此识别版本形态。 */
    reported: boolean;
    /** 该属性属于哪一版目录形态;两人共有为 "both"。 */
    scope: "both" | "2016" | "2022";
}

function codeText(table: Record<number, string>, value: number | null | undefined): { text: string; reported: boolean } {
    if (value === null || value === undefined || value === 0) {
        return { text: CHANNEL_ATTR_UNREPORTED, reported: false };
    }
    const text = table[value];
    if (!text) {
        // 设备报了但值域外(厂商私扩):原样回显,别吞掉 —— 排障时要看得到原值。
        return { text: `未知(${value})`, reported: true };
    }
    return { text, reported: true };
}

/** 多值字段:按 "/" 拆分逐段翻译,原样保留分隔符;全空按未上报处理。 */
function multiCodeText(table: Record<number, string>, raw: string | null | undefined): { text: string; reported: boolean } {
    const trimmed = (raw ?? "").trim();
    if (!trimmed) {
        return { text: CHANNEL_ATTR_UNREPORTED, reported: false };
    }
    const parts = trimmed.split("/").map(part => {
        const value = Number(part.trim());
        if (!Number.isFinite(value)) return part.trim();
        return table[value] ?? `未知(${value})`;
    });
    return { text: parts.filter(Boolean).join(" / "), reported: true };
}

function textOrUnreported(raw: string | null | undefined): { text: string; reported: boolean } {
    const trimmed = (raw ?? "").trim();
    return trimmed ? { text: trimmed, reported: true } : { text: CHANNEL_ATTR_UNREPORTED, reported: false };
}

export function roomTypeText(value: number | null | undefined): string {
    return codeText(ROOM_TYPE_TEXT, value).text;
}

export function supplyLightTypeText(value: number | null | undefined): string {
    return codeText(SUPPLY_LIGHT_TYPE_TEXT, value).text;
}

export function directionTypeText(value: number | null | undefined): string {
    return codeText(DIRECTION_TYPE_TEXT, value).text;
}

export function positionTypeText(value: number | null | undefined): string {
    return codeText(POSITION_TYPE_TEXT, value).text;
}

export function useTypeText(value: number | null | undefined): string {
    return codeText(USE_TYPE_TEXT, value).text;
}

export function photoelectricImagingTypeText(raw: string | null | undefined): string {
    return multiCodeText(PHOTOELECTRIC_IMAGING_TYPE_TEXT, raw).text;
}

/** 通道取流地址:设备声明的 `IP:Port`;只报了其中一个时也照实展示。 */
export function channelEndpointText(ip: string | null | undefined, port: number | null | undefined): string {
    const host = (ip ?? "").trim();
    const hasPort = port !== null && port !== undefined && port !== 0;
    if (!host && !hasPort) return CHANNEL_ATTR_UNREPORTED;
    if (!host) return `:${port}`;
    if (!hasPort) return host;
    return `${host}:${port}`;
}

/**
 * 组装通道详情面板要展示的属性条目(顺序即 UI 顺序)。
 * ⛔ 顺序刻意把两版共有项排在前、版本独有项排在后 —— 让"2016 设备 vs 2022 设备"
 *    一眼能看出来:前四项两版都有值,后四项只有对应版本才可能有值。
 */
export function channelAttributeEntries(channel: ChannelAttributeSnapshot): ChannelAttributeEntry[] {
    const roomType = codeText(ROOM_TYPE_TEXT, channel.roomType);
    const supplyLightType = codeText(SUPPLY_LIGHT_TYPE_TEXT, channel.supplyLightType);
    const directionType = codeText(DIRECTION_TYPE_TEXT, channel.directionType);
    const resolution = textOrUnreported(channel.resolution);
    const endpoint = channelEndpointText(channel.ipAddress, channel.port);
    const endpointReported = endpoint !== CHANNEL_ATTR_UNREPORTED;
    const positionType = codeText(POSITION_TYPE_TEXT, channel.positionType);
    const useType = codeText(USE_TYPE_TEXT, channel.useType);
    const photoelectric = multiCodeText(PHOTOELECTRIC_IMAGING_TYPE_TEXT, channel.photoelectricImagingType);
    const capturePosition = textOrUnreported(channel.capturePositionType);

    return [
        { key: "roomType", label: "室内外", value: roomType.text, reported: roomType.reported, scope: "both" },
        { key: "supplyLightType", label: "补光方式", value: supplyLightType.text, reported: supplyLightType.reported, scope: "both" },
        { key: "directionType", label: "监视方位", value: directionType.text, reported: directionType.reported, scope: "both" },
        { key: "resolution", label: "分辨率", value: resolution.text, reported: resolution.reported, scope: "both" },
        { key: "endpoint", label: "通道地址", value: endpoint, reported: endpointReported, scope: "both" },
        { key: "positionType", label: "位置类型", value: positionType.text, reported: positionType.reported, scope: "2016" },
        { key: "useType", label: "用途", value: useType.text, reported: useType.reported, scope: "2016" },
        { key: "photoelectricImagingType", label: "光电成像类型", value: photoelectric.text, reported: photoelectric.reported, scope: "2022" },
        { key: "capturePositionType", label: "采集部位类型", value: capturePosition.text, reported: capturePosition.reported, scope: "2022" },
    ];
}

/**
 * 由上报到的版本独有属性反推设备用的是哪一版目录形态。
 *
 * ⛔ 这里刻意**不读**设备注册声明的 `effectiveGbVersion` —— 那一列有 `default:2016`,
 *    对 2022 设备会误判成 2016。目录报文里 PositionType/UseType(2016) 与
 *    PhotoelectricImagingType/CapturePositionType(2022) 在 XSD 上互斥,谁上报了就是谁。
 *
 * @returns "2016" / "2022" / "both"(两组都上报,可能是把 2022 报文与 2016 属性混发的
 *          非合规设备)/ "unknown"(两组都没上报,无法判断)。
 */
export function catalogShapeFromAttributes(channel: ChannelAttributeSnapshot): "2016" | "2022" | "both" | "unknown" {
    const has2016 = Boolean(channel.positionType) || Boolean(channel.useType);
    const has2022 = Boolean((channel.photoelectricImagingType ?? "").trim()) || Boolean((channel.capturePositionType ?? "").trim());
    if (has2016 && has2022) return "both";
    if (has2016) return "2016";
    if (has2022) return "2022";
    return "unknown";
}

export function catalogShapeText(shape: ReturnType<typeof catalogShapeFromAttributes>): string {
    switch (shape) {
        case "2016":
            return "GB/T 28181-2016 形态";
        case "2022":
            return "GB/T 28181-2022 形态";
        case "both":
            return "两版属性混发";
        default:
            return "未上报版本独有属性";
    }
}
