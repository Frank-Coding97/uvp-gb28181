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
 *
 * ── 2026-10-05 字典化(P3a)────────────────────────────────────────────────
 * 六个值域改由 `sys_dict` 驱动(见 `DICT_CODE_CHANNEL_*`),展示名现场可改。
 *
 * ⛔⛔ 两条语义**刻意留在本模块、不进字典**(字典表只有"所属字典/值/名字"三位,表达不了):
 *   1. `0` / `""` = "本次未上报"是**哨兵**,不是"值为 0" ⇒ 必须**先判哨兵、再查表**;
 *   2. `photoelectricImagingType` 是**多值**(标准允许 `1/2/3` 以半角 `/` 分隔)
 *      ⇒ 字典只管"单码 → 中文",拆段与拼回留在 `multiCodeText`。
 *
 * ⛔ `capturePositionType`(采集部位类型)**不做字典、也不加表**:标准只要求
 *    "应符合附录 O",没有权威中文名可搬 ⇒ 原样回显。
 *
 * ⭐ 这六项是**只读展示**(设备上报什么就显示什么,平台侧没有让用户选的入口,也没有下发路径)
 *    ⇒ 字典被改坏的最坏结果是该条显示 `未知(N)`,不会影响设备。
 *    ⚠️ 但**真实设备极少上报**这些属性,本批价值在"枚举口径统一",不在覆盖面。
 */
import type { DictLabelFallback } from "@/hooks/useDictOptions";

/** 属性未上报时的占位文案(与"值为 0"区分开)。 */
export const CHANNEL_ATTR_UNREPORTED = "未上报";

// ── 字典 code(开发库 `sys_dict`,直插开发库,仓库侧无 SQL)────────────────────

export const DICT_CODE_CHANNEL_ROOM_TYPE = "channel_room_type";
export const DICT_CODE_CHANNEL_SUPPLY_LIGHT_TYPE = "channel_supply_light_type";
export const DICT_CODE_CHANNEL_DIRECTION_TYPE = "channel_direction_type";
export const DICT_CODE_CHANNEL_POSITION_TYPE = "channel_position_type";
export const DICT_CODE_CHANNEL_USE_TYPE = "channel_use_type";
export const DICT_CODE_CHANNEL_PHOTOELECTRIC_IMAGING_TYPE = "channel_photoelectric_imaging_type";

// ── 兜底常量 = 协议值域(字典未加载时生效;单测逐条锁定)───────────────────────

/** 2016/2022 编码一致:1-室外 2-室内。 */
export const CHANNEL_ROOM_TYPE_LABEL_FALLBACK: DictLabelFallback = {
  "1": "室外",
  "2": "室内"
};

/**
 * 1-无补光 2-红外补光 3-白光补光 两版一致;4-激光补光 9-其他 为 2022 新增。
 * 用并集表即可 —— 2016 设备不可能报出 4/9,2022 设备报 1-3 时含义也相同。
 */
export const CHANNEL_SUPPLY_LIGHT_TYPE_LABEL_FALLBACK: DictLabelFallback = {
  "1": "无补光",
  "2": "红外补光",
  "3": "白光补光",
  "4": "激光补光",
  "9": "其他"
};

/** 2016/2022 编码一致:1-东 2-西 3-南 4-北 5-东南 6-东北 7-西南 8-西北。 */
export const CHANNEL_DIRECTION_TYPE_LABEL_FALLBACK: DictLabelFallback = {
  "1": "东",
  "2": "西",
  "3": "南",
  "4": "北",
  "5": "东南",
  "6": "东北",
  "7": "西南",
  "8": "西北"
};

/** 2016 独有:1-省际检查站 2-党政机关 3-车站码头 4-中心广场 5-体育场馆 6-商业中心 7-宗教场所 8-校园周边 9-治安复杂区域 10-交通干线。 */
export const CHANNEL_POSITION_TYPE_LABEL_FALLBACK: DictLabelFallback = {
  "1": "省际检查站",
  "2": "党政机关",
  "3": "车站码头",
  "4": "中心广场",
  "5": "体育场馆",
  "6": "商业中心",
  "7": "宗教场所",
  "8": "校园周边",
  "9": "治安复杂区域",
  "10": "交通干线"
};

/** 2016 独有:1-治安 2-交通 3-重点。 */
export const CHANNEL_USE_TYPE_LABEL_FALLBACK: DictLabelFallback = {
  "1": "治安",
  "2": "交通",
  "3": "重点"
};

/**
 * 2022 独有:1-可见光成像 2-热成像 3-雷达成像 4-X光成像 5-深度光场成像 9-其他。
 * 标准允许**多值**、以英文半角 "/" 分隔 → 逐段翻译后原样用 "/" 拼回去。
 */
export const CHANNEL_PHOTOELECTRIC_IMAGING_TYPE_LABEL_FALLBACK: DictLabelFallback = {
  "1": "可见光成像",
  "2": "热成像",
  "3": "雷达成像",
  "4": "X光成像",
  "5": "深度光场成像",
  "9": "其他"
};

/** 2022 独有:采集部位类型,取值见 2022 附录 O。标准只要求"应符合附录 O",此处不硬编码中文名。 */

// ── 注入用:六个值域的已合并查表 ───────────────────────────────────────────

export interface ChannelAttributeLabels {
  roomType?: DictLabelFallback;
  supplyLightType?: DictLabelFallback;
  directionType?: DictLabelFallback;
  positionType?: DictLabelFallback;
  useType?: DictLabelFallback;
  photoelectricImagingType?: DictLabelFallback;
}

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

/** 先查字典、再落兜底;**不改**哨兵与值域外的处理口径。 */
function lookup(fallback: DictLabelFallback, labels: DictLabelFallback | undefined, value: number): string | undefined {
  return labels?.[String(value)] ?? fallback[String(value)];
}

function codeText(
  fallback: DictLabelFallback,
  labels: DictLabelFallback | undefined,
  value: number | null | undefined
): { text: string; reported: boolean } {
  if (value === null || value === undefined || value === 0) {
    return { text: CHANNEL_ATTR_UNREPORTED, reported: false };
  }
  const text = lookup(fallback, labels, value);
  if (!text) {
    // 设备报了但值域外(厂商私扩):原样回显,别吞掉 —— 排障时要看得到原值。
    return { text: `未知(${value})`, reported: true };
  }
  return { text, reported: true };
}

/** 多值字段:按 "/" 拆分逐段翻译,原样保留分隔符;全空按未上报处理。 */
function multiCodeText(
  fallback: DictLabelFallback,
  labels: DictLabelFallback | undefined,
  raw: string | null | undefined
): { text: string; reported: boolean } {
  const trimmed = (raw ?? "").trim();
  if (!trimmed) {
    return { text: CHANNEL_ATTR_UNREPORTED, reported: false };
  }
  const parts = trimmed.split("/").map(part => {
    const value = Number(part.trim());
    if (!Number.isFinite(value)) return part.trim();
    return lookup(fallback, labels, value) ?? `未知(${value})`;
  });
  return { text: parts.filter(Boolean).join(" / "), reported: true };
}

function textOrUnreported(raw: string | null | undefined): { text: string; reported: boolean } {
  const trimmed = (raw ?? "").trim();
  return trimmed ? { text: trimmed, reported: true } : { text: CHANNEL_ATTR_UNREPORTED, reported: false };
}

export function roomTypeText(value: number | null | undefined, labels?: DictLabelFallback): string {
  return codeText(CHANNEL_ROOM_TYPE_LABEL_FALLBACK, labels, value).text;
}

export function supplyLightTypeText(value: number | null | undefined, labels?: DictLabelFallback): string {
  return codeText(CHANNEL_SUPPLY_LIGHT_TYPE_LABEL_FALLBACK, labels, value).text;
}

export function directionTypeText(value: number | null | undefined, labels?: DictLabelFallback): string {
  return codeText(CHANNEL_DIRECTION_TYPE_LABEL_FALLBACK, labels, value).text;
}

export function positionTypeText(value: number | null | undefined, labels?: DictLabelFallback): string {
  return codeText(CHANNEL_POSITION_TYPE_LABEL_FALLBACK, labels, value).text;
}

export function useTypeText(value: number | null | undefined, labels?: DictLabelFallback): string {
  return codeText(CHANNEL_USE_TYPE_LABEL_FALLBACK, labels, value).text;
}

export function photoelectricImagingTypeText(raw: string | null | undefined, labels?: DictLabelFallback): string {
  return multiCodeText(CHANNEL_PHOTOELECTRIC_IMAGING_TYPE_LABEL_FALLBACK, labels, raw).text;
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
 *
 * @param channel 通道属性快照
 * @param labels  字典查表(`useChannelAttributeDict` 的产物);省略则纯用协议值域兜底
 */
export function channelAttributeEntries(
  channel: ChannelAttributeSnapshot,
  labels?: ChannelAttributeLabels
): ChannelAttributeEntry[] {
  const roomType = codeText(CHANNEL_ROOM_TYPE_LABEL_FALLBACK, labels?.roomType, channel.roomType);
  const supplyLightType = codeText(CHANNEL_SUPPLY_LIGHT_TYPE_LABEL_FALLBACK, labels?.supplyLightType, channel.supplyLightType);
  const directionType = codeText(CHANNEL_DIRECTION_TYPE_LABEL_FALLBACK, labels?.directionType, channel.directionType);
  const resolution = textOrUnreported(channel.resolution);
  const endpoint = channelEndpointText(channel.ipAddress, channel.port);
  const endpointReported = endpoint !== CHANNEL_ATTR_UNREPORTED;
  const positionType = codeText(CHANNEL_POSITION_TYPE_LABEL_FALLBACK, labels?.positionType, channel.positionType);
  const useType = codeText(CHANNEL_USE_TYPE_LABEL_FALLBACK, labels?.useType, channel.useType);
  const photoelectric = multiCodeText(
    CHANNEL_PHOTOELECTRIC_IMAGING_TYPE_LABEL_FALLBACK,
    labels?.photoelectricImagingType,
    channel.photoelectricImagingType
  );
  const capturePosition = textOrUnreported(channel.capturePositionType);

  return [
    { key: "roomType", label: "室内外", value: roomType.text, reported: roomType.reported, scope: "both" },
    { key: "supplyLightType", label: "补光方式", value: supplyLightType.text, reported: supplyLightType.reported, scope: "both" },
    { key: "directionType", label: "监视方位", value: directionType.text, reported: directionType.reported, scope: "both" },
    { key: "resolution", label: "分辨率", value: resolution.text, reported: resolution.reported, scope: "both" },
    { key: "endpoint", label: "通道地址", value: endpoint, reported: endpointReported, scope: "both" },
    { key: "positionType", label: "位置类型", value: positionType.text, reported: positionType.reported, scope: "2016" },
    { key: "useType", label: "用途", value: useType.text, reported: useType.reported, scope: "2016" },
    {
      key: "photoelectricImagingType",
      label: "光电成像类型",
      value: photoelectric.text,
      reported: photoelectric.reported,
      scope: "2022"
    },
    {
      key: "capturePositionType",
      label: "采集部位类型",
      value: capturePosition.text,
      reported: capturePosition.reported,
      scope: "2022"
    }
  ];
}

/**
 * 由上报到的版本独有属性反推设备用的是哪一版目录形态。
 *
 * ⛔ 这里刻意**不读**设备注册声明的 `effectiveGbVersion` —— 那一列有 `default:2016`,
 *    对 2022 设备会误判成 2016。目录报文里 PositionType/UseType(2016) 与
 *    PhotoelectricImagingType/CapturePositionType(2022) 在 XSD 上互斥,谁上报了就是谁。
 *
 * ⛔ 判定**只认码值**(`positionType === 0` 即未上报),与字典文案无关 ——
 *    现场把字典名改了(甚至改成别的写法)也不会影响本函数。
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
