/**
 * 设备配置家族的分组与字段定义（GB/T 28181 DeviceConfig / A.2.3.2）
 *
 * ⛔ 分组**不是**照抄海康那套 ISP 参数（图像/曝光/背光/白平衡/补光灯）——
 *    GB28181 平台管不到摄像机的 ISP，那些要靠 ONVIF 或私有协议。
 *    这里借的是专业客户端的**形态**（左预览 / 中分组 / 右参数 + 滑杆微调），
 *    内容必须映射到标准的 ConfigType 家族，否则就是做了个点不动的假界面。
 *
 * `state` 口径：
 *   - `ready`  —— 后端已接通，可读可写（目前只有「视频参数属性」A-5）
 *   - `static` —— **仅静态形态**，控件齐全但未接后端；禁止让人误以为能下发，
 *                故界面统一标「未接入」并禁用交互。
 */

export type ConfigFieldKind = "select" | "slider" | "switch" | "text" | "coords" | "weekdays";

export interface ConfigSelectOption {
    value: string;
    label: string;
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
}

export interface ConfigSwitchField extends ConfigFieldBase {
    kind: "switch";
}

export interface ConfigTextField extends ConfigFieldBase {
    kind: "text";
    placeholder?: string;
    maxlength?: number;
}

/** 遮挡区这类矩形参数：X / Y / W / H 四个数。 */
export interface ConfigCoordsField extends ConfigFieldBase {
    kind: "coords";
}

/** 星期勾选（录像计划用）。 */
export interface ConfigWeekdaysField extends ConfigFieldBase {
    kind: "weekdays";
}

export type ConfigField =
    | ConfigSelectField
    | ConfigSliderField
    | ConfigSwitchField
    | ConfigTextField
    | ConfigCoordsField
    | ConfigWeekdaysField;

export type ConfigGroupState = "ready" | "static";

/** 各组字段值的联合类型（静态组用；接入后端后由真实回读值替换）。 */
export type FieldValue = string | number | boolean | number[] | string[];

export interface ConfigGroup {
    key: string;
    label: string;
    /** 标准条款出处，渲染在参数区标题右侧。 */
    std: string;
    state: ConfigGroupState;
    /** 一句话说明该组做什么（参数区顶部提示）。 */
    summary: string;
    fields: ConfigField[];
}

export const OSD_POSITION_OPTIONS: ConfigSelectOption[] = [
    { value: "0", label: "不使用" },
    { value: "1", label: "左上角" },
    { value: "2", label: "右上角" },
    { value: "3", label: "左下角" },
    { value: "4", label: "右下角" }
];

export const DATE_FORMAT_OPTIONS: ConfigSelectOption[] = [
    { value: "1", label: "YYYY-MM-DD" },
    { value: "2", label: "MM-DD-YYYY" },
    { value: "3", label: "DD-MM-YYYY" },
    { value: "4", label: "YYYY年MM月DD日" }
];

export const TIME_FORMAT_OPTIONS: ConfigSelectOption[] = [
    { value: "1", label: "24 小时制" },
    { value: "2", label: "12 小时制" }
];

export const MIRROR_OPTIONS: ConfigSelectOption[] = [
    { value: "0", label: "关闭" },
    { value: "1", label: "上下翻转" },
    { value: "2", label: "左右翻转" },
    { value: "3", label: "中心翻转" }
];

export const WEEKDAY_LABELS = ["一", "二", "三", "四", "五", "六", "日"] as const;

export const CONFIG_GROUPS: ConfigGroup[] = [
    {
        key: "video-param",
        label: "视频参数属性",
        std: "A.2.3.2.5",
        state: "ready",
        summary: "改实际编码参数。下发应答没有回显，面板的权威值一律来自回读。",
        fields: []
    },
    {
        key: "osd",
        label: "图像叠加 OSD",
        std: "A.2.1.12",
        state: "static",
        summary: "时间与自定义字符叠加。坐标按播放窗口像素原点。",
        fields: [
            { kind: "switch", key: "timeShow", label: "时间显示" },
            {
                kind: "select",
                key: "dateFormat",
                label: "日期格式",
                options: DATE_FORMAT_OPTIONS
            },
            {
                kind: "select",
                key: "timeFormat",
                label: "时间格式",
                options: TIME_FORMAT_OPTIONS
            },
            {
                kind: "select",
                key: "timePosition",
                label: "时间位置",
                options: OSD_POSITION_OPTIONS
            },
            {
                kind: "slider",
                key: "timeSize",
                label: "时间字号",
                min: 1,
                max: 10,
                hint: "1~10 档"
            },
            {
                kind: "coords",
                key: "timeOffset",
                label: "时间偏移",
                hint: "X / Y，像素"
            },
            {
                kind: "text",
                key: "osdText",
                label: "叠加文字",
                placeholder: "如：厂区东门",
                maxlength: 32,
                hint: "最长 32 字符"
            }
        ]
    },
    {
        key: "picture",
        label: "画面处理",
        std: "A.2.1.17 / A.2.1.16",
        state: "static",
        summary: "画面翻转与隐私遮挡区。遮挡区最多 4 个，坐标按播放窗口像素原点。",
        fields: [
            {
                kind: "select",
                key: "mirror",
                label: "画面翻转",
                options: MIRROR_OPTIONS
            },
            { kind: "coords", key: "mask1", label: "遮挡区 1" },
            { kind: "coords", key: "mask2", label: "遮挡区 2" },
            { kind: "coords", key: "mask3", label: "遮挡区 3" },
            { kind: "coords", key: "mask4", label: "遮挡区 4" }
        ]
    },
    {
        key: "record-plan",
        label: "录像计划",
        std: "A.2.1.15",
        state: "static",
        summary: "定时录像计划的星期与时段。",
        fields: [
            { kind: "switch", key: "planEnabled", label: "启用计划" },
            { kind: "weekdays", key: "weekdays", label: "生效星期" },
            {
                kind: "text",
                key: "beginTime",
                label: "开始时刻",
                placeholder: "00:00",
                hint: "24 小时制 HH:MM"
            },
            { kind: "text", key: "endTime", label: "结束时刻", placeholder: "23:59" },
            {
                kind: "select",
                key: "recordStream",
                label: "录像码流",
                options: [
                    { value: "0", label: "主码流" },
                    { value: "1", label: "子码流 1" },
                    { value: "2", label: "子码流 2" }
                ]
            }
        ]
    },
    {
        key: "alarm-record",
        label: "报警录像",
        std: "A.2.1.16",
        state: "static",
        summary: "报警触发时的录像行为与时长。",
        fields: [
            { kind: "switch", key: "alarmRecordEnabled", label: "报警录像" },
            {
                kind: "select",
                key: "triggerEvent",
                label: "触发事件",
                options: [
                    { value: "1", label: "视频报警" },
                    { value: "2", label: "设备报警" },
                    { value: "3", label: "全部" }
                ]
            },
            {
                kind: "slider",
                key: "preRecord",
                label: "预录时长",
                min: 0,
                max: 60,
                unit: "秒"
            },
            {
                kind: "slider",
                key: "recordDuration",
                label: "录像时长",
                min: 10,
                max: 600,
                step: 10,
                unit: "秒"
            }
        ]
    },
    {
        key: "basic",
        label: "基本参数",
        std: "A.2.1.19",
        state: "static",
        summary: "设备名称与注册/心跳周期。改注册有效期会影响下次注册的 expires。",
        fields: [
            {
                kind: "text",
                key: "deviceName",
                label: "设备名称",
                placeholder: "设备上报的名称",
                maxlength: 128
            },
            {
                kind: "slider",
                key: "registerExpires",
                label: "注册有效期",
                min: 3600,
                max: 86400,
                step: 3600,
                unit: "秒",
                hint: "标准下限 3600"
            },
            {
                kind: "slider",
                key: "heartbeatInterval",
                label: "心跳间隔",
                min: 60,
                max: 600,
                step: 10,
                unit: "秒",
                hint: "标准下限 60"
            }
        ]
    },
    {
        key: "alarm-report",
        label: "报警上报",
        std: "A.2.1.18",
        state: "static",
        summary: "报警上报开关。标准只定义两个开关且均为必选 —— 只想改一个时按并集收。",
        fields: [
            { kind: "switch", key: "videoAlarmReport", label: "视频报警上报" },
            { kind: "switch", key: "deviceAlarmReport", label: "设备报警上报" }
        ]
    },
    {
        key: "svac",
        label: "SVAC 编解码",
        std: "A.2.1.10 / A.2.1.11",
        state: "static",
        summary: "SVAC 编解码配置。非 SVAC 设备通常不返回该配置类型。",
        fields: [
            { kind: "switch", key: "svacEnable", label: "SVAC 使能" },
            {
                kind: "select",
                key: "svacEncodeFormat",
                label: "编码格式",
                options: [
                    { value: "1", label: "SVAC 1.0" },
                    { value: "2", label: "SVAC 2.0" }
                ]
            },
            {
                kind: "slider",
                key: "svacBitRate",
                label: "编码码率",
                min: 0,
                max: 100000,
                unit: "kb/s"
            }
        ]
    }
];

/** 静态组的初值（演示形态用；接入后端后由真实回读值替换）。 */
export const CONFIG_GROUP_DEFAULTS: Record<string, Record<string, FieldValue>> = {
    osd: {
        timeShow: true,
        dateFormat: "1",
        timeFormat: "1",
        timePosition: "2",
        timeSize: 3,
        timeOffset: [10, 10],
        osdText: ""
    },
    picture: {
        mirror: "0",
        mask1: [0, 0, 0, 0],
        mask2: [0, 0, 0, 0],
        mask3: [0, 0, 0, 0],
        mask4: [0, 0, 0, 0]
    },
    "record-plan": {
        planEnabled: true,
        weekdays: ["一", "二", "三", "四", "五"],
        beginTime: "00:00",
        endTime: "23:59",
        recordStream: "0"
    },
    "alarm-record": {
        alarmRecordEnabled: true,
        triggerEvent: "3",
        preRecord: 5,
        recordDuration: 60
    },
    basic: {
        deviceName: "",
        registerExpires: 3600,
        heartbeatInterval: 60
    },
    "alarm-report": {
        videoAlarmReport: true,
        deviceAlarmReport: true
    },
    svac: {
        svacEnable: false,
        svacEncodeFormat: "1",
        svacBitRate: 2048
    }
};

export function findConfigGroup(key: string): ConfigGroup | undefined {
    return CONFIG_GROUPS.find((group) => group.key === key);
}
