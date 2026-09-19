/**
 * 设备配置家族的分组与字段定义（GB/T 28181 DeviceConfig / A.2.3.2）
 *
 * ⛔ 分组**不是**照抄海康那套 ISP 参数（图像/曝光/背光/白平衡/补光灯）——
 *    GB28181 平台管不到摄像机的 ISP，那些要靠 ONVIF 或私有协议。
 *    这里借的是专业客户端的**形态**（左预览 / 中分组 / 右参数 + 滑杆微调），
 *    内容必须映射到标准的 ConfigType 家族，否则就是做了个点不动的假界面。
 *
 * ## 本文件的位置（改之前先读）
 *
 * 这里是**展示层的表单词汇表**：`key` 是表单字段 id，为了排版可以把一个协议元素
 * 拆成两个控件、也可以把四个协议字段并成一个矩形控件。它**不是**协议词汇表。
 *
 * ⛔ 协议词汇表（`payload_json` / 对账字段路径 / 读写接口的 `blocks`）在**后端**
 *    `manscdp` 结构的 json tag 上，键名 = 标准元素名的小驼峰。
 *    两张表之间的翻译**只准发生在 `deviceConfigPayload.ts` 一个地方**。
 *    把表单键直接当协议键用，等于让"4 个 maskN 对应一个 RegionList/Item 数组"
 *    这种事溢到接口上，然后两边迟早对不上。
 *
 * ## `state` 口径
 *
 *   - `ready`  —— 后端已接通，可读可写
 *   - `static` —— **仅静态形态**，控件齐全但未接后端；禁止让人误以为能下发，
 *                故界面统一标「未接入」并禁用交互。
 *
 * 现在 7 组全部 `ready`（视频参数属性走 `video-params` 独立通道，其余 6 组走
 * `device-configs` 通用通道）。`static` 这个态**保留着**：新增 ConfigType 时，
 * 先按静态形态把字段口径定型、再决定接不接后端，比"先接个半成品"安全。
 */

export type ConfigFieldKind = "select" | "slider" | "switch" | "text" | "coords" | "texts" | "schedules" | "mirror";

export interface ConfigSelectOption {
  value: string;
  label: string;
  /** 窄空间（如图标按钮）用的短标签；缺省时用 `label`。 */
  shortLabel?: string;
}

interface ConfigFieldBase {
  key: string;
  label: string;
  /** 行尾小字说明（专业客户端都有，用来交代取值出处/单位/约束）。 */
  hint?: string;
}

export interface ConfigSelectField extends ConfigFieldBase {
  kind: "select";
  options: ConfigSelectOption[];
}

export interface ConfigSliderField extends ConfigFieldBase {
  kind: "slider";
  min: number;
  max: number;
  step?: number;
  unit?: string;
  /**
   * 该值在协议里是**可选元素**（缺席 ≠ 0）时的说明。
   * 界面把空值表达成空输入框（滑杆会跳回下界，但那只是显示），
   * 下发时空值 = 不发这个元素 = 设备维持原值。
   */
  optional?: boolean;
}

export interface ConfigSwitchField extends ConfigFieldBase {
  kind: "switch";
}

export interface ConfigTextField extends ConfigFieldBase {
  kind: "text";
  placeholder?: string;
  maxlength?: number;
}

/**
 * 若干个整数一组的坐标控件。
 *
 * ⛔ `axes` 必须写清"这几个数各自是什么"，不能默认成 `X / Y / W / H`：
 *    遮挡区是**两个角点**（左X,左Y,右X,右Y），不是左上角 + 宽高。
 *    按宽高理解会画出偏移一整个宽高的假遮挡，而两侧都不报错。
 */
export interface ConfigCoordsField extends ConfigFieldBase {
  kind: "coords";
  axes: string[];
}

/**
 * 方向选择控件（目前只用于画面镜像）。
 *
 * ⛔ 镜像天然是图形（左右 / 上下 / 旋转 180°），下拉框把方向压成字符串，
 *    等于给「1=水平、2=上下」写反留门 —— 写反的后果是画面真翻了、回读对账
 *    却显示一致，只能靠人眼发现。用控件形态把方向摆出来，写错就是一眼的事。
 * ⛔ 但图标**不能替代文字**：对账比的是值、不是画面对不对。
 *    所以每个按钮必须带文字副标题（`shortLabel`），且 `value` 仍是唯一真源。
 */
export interface ConfigMirrorField extends ConfigFieldBase {
  kind: "mirror";
  options: ConfigSelectOption[];
}

/** 变长文本行（OSD 自由文本，标准上限 8 条）。 */
export interface ConfigTextsField extends ConfigFieldBase {
  kind: "texts";
  maxItems: number;
}

/** 周计划（录像计划）：7 天 × 若干时段。 */
export interface ConfigSchedulesField extends ConfigFieldBase {
  kind: "schedules";
}

export type ConfigField =
  | ConfigSelectField
  | ConfigSliderField
  | ConfigSwitchField
  | ConfigTextField
  | ConfigCoordsField
  | ConfigTextsField
  | ConfigSchedulesField
  | ConfigMirrorField;

export type ConfigGroupState = "ready" | "static";

/** 一条自由文本（映射 `OSDConfig/Item`）。 */
export interface ConfigTextItem {
  text: string;
  x: number;
  y: number;
}

/**
 * 一个时段。
 *
 * ⛔ 存**格式化到秒的字符串**而不是"起止两个 Date"或"H/M/S 六个数"：
 *    协议里六个整数都是必选，只让用户填 HH:MM 就等于每次下发都把秒清零 ——
 *    那是**静默改写**设备上已存在的计划。这里读写都保持 `HH:MM:SS`，
 *    用户填 `08:00` 解析成 08:00:00，填 `08:00:30` 就原样带走。
 */
export interface ConfigSegment {
  start: string;
  stop: string;
}

/** 一个星期的时段列表（映射 `RecordSchedule`）。没有计划的那天应保持空数组。 */
export interface ConfigScheduleDay {
  weekDayNum: number;
  segments: ConfigSegment[];
}

/** 各组字段值的联合类型（表单袋；具体形状由 `deviceConfigPayload.ts` 逐组收窄）。 */
export type FieldValue = string | number | boolean | number[] | ConfigTextItem[] | ConfigScheduleDay[];

export interface ConfigGroup {
  key: string;
  label: string;
  /** 标准条款出处，渲染在参数区标题右侧。 */
  std: string;
  state: ConfigGroupState;
  /**
   * 该分组在标准里的引入版本。
   *
   * 2016 只有 4 个 ConfigType：BasicParam / VideoParamOpt / SVACEncodeConfig / SVACDecodeConfig；
   * 其余（含本族这 8 组里的 6 组）都是 2022 新增。
   *
   * ⛔ 这个标记**只用来选提示措辞**（"2016 设备上这个类型不存在，回读会给出 type_absent"），
   *    **不参与任何门禁**：被登记成 2016 的真 2022 设备，不试一次就永远用不了这功能。
   *    同 `protocol.Capabilities` 那句 "capability hints must never become a sending gate"。
   */
  since: "2016" | "2022";
  /** 一句话说明该组做什么（参数区顶部提示）。 */
  summary: string;
  /**
   * 该组覆盖的标准 ConfigType 名（**读写/对账/absent 都按它算**）。
   *
   * ⛔ 是数组而不是单值：「画面处理」这一组天然覆盖两个 ConfigType
   *    （`FrameMirror` + `PictureMask`），而标准里它们就是两条独立配置。
   *    空数组 = 该组走独立通道（目前只有视频参数属性）。
   */
  configTypes: string[];
  fields: ConfigField[];
}

/** OSD 时间格式（`TimeType`）。空串 = **不下发该元素**（设备自己决定格式）。 */
export const OSD_TIME_TYPE_OPTIONS: ConfigSelectOption[] = [
  { value: "", label: "不指定（不下发该元素）" },
  { value: "0", label: "YYYY-MM-DD HH:MM:SS" },
  { value: "1", label: "YYYY年MM月DD日HH:MM:SS" }
];

/**
 * 画面镜像取值（FrameMirror 的 enumeration，抄自 A.2.1.23）。
 *
 * ⛔ 1/2 别写反：**1 = 水平镜像（左右翻转）**、2 = 上下镜像（上下翻转）。
 *    写反的后果是"下发上下翻转、画面左右翻"，回读对账还会显示一致
 *    （因为对账比的是值，不是画面对不对），只能靠人眼发现。
 */
export const MIRROR_OPTIONS: ConfigSelectOption[] = [
  { value: "0", label: "不启用镜像", shortLabel: "原图" },
  { value: "1", label: "水平镜像（左右翻转）", shortLabel: "左右" },
  { value: "2", label: "上下镜像（上下翻转）", shortLabel: "上下" },
  // ⛔ 「上下左右同时翻转」描述的是**过程**不是结果：值 3 的结果是旋转 180°。
  { value: "3", label: "中心镜像（旋转 180°）", shortLabel: "中心" }
];

/** 码流编号（0=主码流，1=子码流 1…）。 */
export const STREAM_NUMBER_OPTIONS: ConfigSelectOption[] = [
  { value: "0", label: "主码流" },
  { value: "1", label: "子码流 1" },
  { value: "2", label: "子码流 2" },
  { value: "3", label: "子码流 3" }
];

/** 遮挡区的坐标口径：**两个角点**（左上角 + 右下角），单位像素。 */
export const MASK_AXES = ["左X", "左Y", "右X", "右Y"];

/** 时间坐标只有两个数（X / Y）。 */
export const POINT_AXES = ["X", "Y"];

export const WEEKDAY_LABELS = ["一", "二", "三", "四", "五", "六", "日"] as const;

/** 标准上限：遮挡区最多 4 个（`PictureMask/RegionList/Num`）。 */
export const MAX_MASK_REGIONS = 4;

/** 标准上限：OSD 自由文本最多 8 条（`Item maxOccurs="8"`）。 */
export const MAX_OSD_TEXT_ITEMS = 8;

/** 标准上限：单条 OSD 文本 0~32 个**字符**（不是字节 —— 中文按 1 个算）。 */
export const MAX_OSD_TEXT_LENGTH = 32;

export const CONFIG_GROUPS: ConfigGroup[] = [
  {
    key: "video-param",
    label: "视频参数属性",
    std: "A.2.3.2.5",
    since: "2022",
    state: "ready",
    summary: "改实际编码参数。下发应答没有回显，面板的权威值一律来自回读。",
    configTypes: [],
    fields: []
  },
  {
    key: "osd",
    label: "图像叠加 OSD",
    std: "A.2.1.12",
    since: "2022",
    state: "ready",
    summary:
      "设备烧进视频流的叠加层（不是播放器里的界面叠层）。坐标是绝对像素、原点在左上角；Length/Width 是配置窗口的水平/垂直像素数。",
    configTypes: ["OSDConfig"],
    fields: [
      { kind: "switch", key: "timeEnable", label: "时间显示" },
      {
        kind: "select",
        key: "timeType",
        label: "时间格式",
        options: OSD_TIME_TYPE_OPTIONS,
        hint: "不指定则不发送该元素"
      },
      {
        kind: "slider",
        key: "length",
        label: "窗口长度",
        min: 1,
        max: 3840,
        unit: "像素",
        hint: "配置窗口长度（视频水平像素数），必须为正"
      },
      {
        kind: "slider",
        key: "width",
        label: "窗口宽度",
        min: 1,
        max: 2160,
        unit: "像素",
        hint: "配置窗口宽度（视频垂直像素数），必须为正"
      },
      { kind: "slider", key: "timeX", label: "时间 X", min: 0, max: 3840, unit: "像素" },
      { kind: "slider", key: "timeY", label: "时间 Y", min: 0, max: 2160, unit: "像素" },
      { kind: "switch", key: "textEnable", label: "文字显示" },
      {
        kind: "texts",
        key: "items",
        label: "叠加文字",
        maxItems: MAX_OSD_TEXT_ITEMS,
        hint: `最多 ${MAX_OSD_TEXT_ITEMS} 条，每条不超过 ${MAX_OSD_TEXT_LENGTH} 个字符`
      }
    ]
  },
  {
    key: "picture",
    label: "画面处理",
    std: "A.2.1.23 / A.2.1.17",
    since: "2022",
    state: "ready",
    summary:
      "画面镜像与隐私遮挡区。遮挡区最多 4 个，每个是左上角 + 右下角两个角点（不是位置 + 宽高）；四个坐标全 0 的区域视为未启用、不下发。",
    configTypes: ["FrameMirror", "PictureMask"],
    fields: [
      { kind: "mirror", key: "mirror", label: "画面镜像", options: MIRROR_OPTIONS, hint: "1=左右，2=上下" },
      { kind: "switch", key: "maskOn", label: "启用遮挡" },
      { kind: "coords", key: "mask1", label: "遮挡区 1", axes: MASK_AXES },
      { kind: "coords", key: "mask2", label: "遮挡区 2", axes: MASK_AXES },
      { kind: "coords", key: "mask3", label: "遮挡区 3", axes: MASK_AXES },
      { kind: "coords", key: "mask4", label: "遮挡区 4", axes: MASK_AXES }
    ]
  },
  {
    key: "record-plan",
    label: "录像计划",
    std: "A.2.1.15",
    since: "2022",
    state: "ready",
    summary:
      "定时录像计划。按「周几 → 时段」逐条配置（标准里周一=1 … 周日=7）；没有计划的那天整条不发，与「那天有一条 0 秒时段」是两件事。",
    configTypes: ["VideoRecordPlan"],
    fields: [
      { kind: "switch", key: "recordEnable", label: "启用计划" },
      { kind: "select", key: "streamNumber", label: "录像码流", options: STREAM_NUMBER_OPTIONS },
      { kind: "schedules", key: "schedules", label: "周计划", hint: "时间为 HH:MM:SS（秒可省略）" }
    ]
  },
  {
    key: "alarm-record",
    label: "报警录像",
    std: "A.2.1.16",
    since: "2022",
    state: "ready",
    summary:
      "报警触发时的录像行为。录像延时是报警时间点**之后**的时长，预录是之前的时长；两个都留空表示不下发这两项（设备维持原值）。",
    configTypes: ["VideoAlarmRecord"],
    fields: [
      { kind: "switch", key: "recordEnable", label: "启用报警录像" },
      { kind: "select", key: "streamNumber", label: "录像码流", options: STREAM_NUMBER_OPTIONS },
      {
        kind: "slider",
        key: "recordTime",
        label: "录像延时",
        min: 0,
        max: 86400,
        step: 5,
        unit: "秒",
        optional: true,
        hint: "报警后继续录多久；留空 = 不下发该元素"
      },
      {
        kind: "slider",
        key: "preRecordTime",
        label: "预录时长",
        min: 0,
        max: 86400,
        step: 5,
        unit: "秒",
        optional: true,
        hint: "报警前回溯多久；留空 = 不下发该元素"
      }
    ]
  },
  {
    key: "basic",
    label: "基本参数",
    std: "A.2.1.19",
    since: "2016",
    state: "ready",
    summary: "设备名称与注册/心跳周期。四项在标准里都是可选的：留空即不下发该元素，设备维持原值 —— 界面不会把「没配」显示成 0。",
    configTypes: ["BasicParam"],
    fields: [
      {
        kind: "text",
        key: "name",
        label: "设备名称",
        placeholder: "留空 = 不下发该元素",
        maxlength: 128
      },
      {
        kind: "slider",
        key: "expiration",
        label: "注册有效期",
        min: 1,
        max: 86400,
        step: 60,
        unit: "秒",
        optional: true,
        hint: "1~86400；留空 = 不下发该元素"
      },
      {
        kind: "slider",
        key: "heartBeatInterval",
        label: "心跳间隔",
        min: 1,
        max: 86400,
        step: 5,
        unit: "秒",
        optional: true,
        hint: "1~86400；留空 = 不下发该元素"
      },
      {
        kind: "slider",
        key: "heartBeatCount",
        label: "心跳超时次数",
        min: 1,
        max: 1000,
        unit: "次",
        optional: true,
        hint: "1~1000；留空 = 不下发该元素"
      }
    ]
  },
  {
    key: "alarm-report",
    label: "报警上报",
    std: "A.2.1.18",
    since: "2022",
    state: "ready",
    summary:
      "报警上报开关。标准只定义两个开关且**都是必选**，字段名就是标准原文的 MotionDetection / FieldDetection（不是业务语义的「视频/设备报警」）。",
    configTypes: ["AlarmReport"],
    fields: [
      { kind: "switch", key: "motionDetection", label: "移动侦测上报（MotionDetection）" },
      { kind: "switch", key: "fieldDetection", label: "区域入侵上报（FieldDetection）" }
    ]
  }
];

/**
 * 表单初值（**空白模板**，不是"默认要下发的值"）。
 *
 * 语义是"设备还没回读这一组时表单长什么样"。接上后端后，回读到的值会覆盖它；
 * 回读缺席时保持空白 —— 空白在下发侧就是"不下发该项"，而不是"下发 0"。
 */
export const CONFIG_GROUP_DEFAULTS: Record<string, Record<string, FieldValue>> = {
  osd: {
    timeEnable: true,
    timeType: "",
    // ⛔ 滑杆承载的字段一律存**字符串**：滑杆清空时发的是空串，
    //    而"空串 = 不下发该元素"是这套表单的核心语义。存数字就表达不了它，
    //    而且会让回读值（字符串）与初值（数字）在脏值统计里被算成"已改"。
    length: "1920",
    width: "1080",
    timeX: "10",
    timeY: "10",
    textEnable: true,
    items: []
  },
  picture: {
    mirror: "0",
    maskOn: false,
    mask1: [0, 0, 0, 0],
    mask2: [0, 0, 0, 0],
    mask3: [0, 0, 0, 0],
    mask4: [0, 0, 0, 0]
  },
  "record-plan": {
    recordEnable: true,
    streamNumber: "0",
    schedules: []
  },
  "alarm-record": {
    recordEnable: true,
    streamNumber: "0",
    recordTime: "",
    preRecordTime: ""
  },
  basic: {
    name: "",
    expiration: "",
    heartBeatInterval: "",
    heartBeatCount: ""
  },
  "alarm-report": {
    motionDetection: true,
    fieldDetection: true
  }
};

export function findConfigGroup(key: string): ConfigGroup | undefined {
  return CONFIG_GROUPS.find(group => group.key === key);
}

export function configGroupDefaults(key: string): Record<string, FieldValue> {
  return { ...(CONFIG_GROUP_DEFAULTS[key] ?? {}) };
}
