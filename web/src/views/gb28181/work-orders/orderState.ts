import type { WorkRecordingForm } from "@/api/gb28181-work-recording";

/**
 * 作业单状态的唯一文案来源。
 * 原先 `index.vue` / `WorkRecordingJobs.vue` / `WorkRecordingBatches.vue` 各写一份且文案不一致
 * （同一个 failed 一处「开始失败」一处「失败」），现统一到这里。
 */
const stateLabels: Record<string, string> = {
    idle: "未开始",
    starting: "开始中",
    recording: "录像中",
    stopping: "结束中",
    stopped: "已结束",
    failed: "失败",
    unknown: "待核实"
};

/**
 * 状态配色。每个状态必须两两可辨——曾经「录像中」与「失败」都是 red，
 * 列表里两者看上去一模一样。绿色沿用「云端录像」页 `正在录制` 的既有约定。
 */
const stateColors: Record<string, string> = {
    idle: "gray",
    starting: "orange",
    recording: "green",
    stopping: "gold",
    stopped: "blue",
    failed: "red",
    unknown: "orangered"
};

export function workOrderStateLabel(state: string): string {
    return stateLabels[state] || "待核实";
}

export function workOrderStateColor(state: string): string {
    return stateColors[state] || "gray";
}

/**
 * 未结算状态。多屏页据此决定按钮是「结束录像」还是「开始录像」，
 * 与后端 `BatchService.Active` 的筛选条件保持一致。
 */
export function isWorkOrderActive(state: string): boolean {
    return state === "starting" || state === "recording" || state === "stopping" || state === "unknown";
}

/** 作业单表单的字段分组，创建表单与只读展示共用同一份定义。 */
export const workOrderFormSections: Array<{ title: string; fields: Array<[keyof WorkRecordingForm, string]> }> = [
    {
        title: "基本信息",
        fields: [
            ["projectName", "项目名称"],
            ["major", "专业"],
            ["stationArea", "站区"],
            ["mileage", "里程"],
            ["anchorSectionNo", "锚段号"],
            ["workLeader", "作业负责人"],
            ["startAnchorPillarNo", "起锚支柱号"],
            ["endAnchorPillarNo", "落锚柱柱号"],
            ["workPersonnel", "作业人员"]
        ]
    },
    {
        title: "技术参数",
        fields: [
            ["tensionWireCarModel", "恒张力放线车型号"],
            ["tensionWireCarNo", "恒张力放线车编号"],
            ["setTension", "放线设定张力"],
            ["straightenerStatus", "校直器状况"],
            ["straightenerInspector", "校直器检查人"]
        ]
    },
    {
        title: "作业情况",
        fields: [
            ["wireLayingProcess", "放线过程情况"],
            ["remark", "备注"]
        ]
    }
];

/**
 * 必填字段，必须与后端 `workrecording.Form.missingRequiredFields()` 保持同步。
 * 不同步会让现场遇到「前端放过、后端拒绝」或反之。
 */
export const workOrderRequiredFields: Array<keyof WorkRecordingForm> = [
    "projectName",
    "stationArea",
    "anchorSectionNo",
    "workLeader",
    "workPersonnel"
];

/** 多行文本字段，用于决定渲染 a-input 还是 a-textarea。 */
export const workOrderLongFields: Array<keyof WorkRecordingForm> = ["wireLayingProcess", "remark"];

/**
 * 作业人员人数上限，必须与后端 `formPersonnelMax` 保持同步。
 * 前端提前拦下，现场才不会填完一长串名字才等到 400。
 */
export const workOrderPersonnelMax = 50;

export const workOrderFieldLabels: Record<string, string> = Object.fromEntries(
    workOrderFormSections.flatMap(section => section.fields.map(([key, label]) => [key, label]))
);

export function emptyWorkOrderForm(): WorkRecordingForm {
    return {
        projectName: "",
        major: "",
        stationArea: "",
        mileage: "",
        anchorSectionNo: "",
        startAnchorPillarNo: "",
        endAnchorPillarNo: "",
        workLeader: "",
        workPersonnel: [],
        tensionWireCarModel: "",
        tensionWireCarNo: "",
        setTension: "",
        straightenerStatus: "",
        straightenerInspector: "",
        wireLayingProcess: "",
        remark: ""
    };
}

type FormSource = Partial<Record<keyof WorkRecordingForm, string | string[]>>;

/**
 * 把表单值规整成后端期望的形状：作业人员由顿号/逗号/换行分隔的文本转成数组，
 * 纯空白项丢弃；其余字段去除首尾空白。
 */
export function normalizeWorkOrderForm(source: FormSource): WorkRecordingForm {
    const form = emptyWorkOrderForm();
    for (const key of Object.keys(form) as Array<keyof WorkRecordingForm>) {
        const value = source[key];
        if (key === "workPersonnel") {
            const raw = Array.isArray(value) ? value : String(value ?? "").split(/[、,，\n]/);
            form.workPersonnel = raw.map(item => item.trim()).filter(Boolean);
            continue;
        }
        form[key] = String(value ?? "").trim() as never;
    }
    return form;
}

/** 返回缺失的必填项标签，供表单逐项提示，也用于提交前的最后一道拦截。 */
export function missingWorkOrderFields(form: WorkRecordingForm): string[] {
    return workOrderRequiredFields
        .filter(key => {
            if (key === "workPersonnel") return form.workPersonnel.filter(name => name.trim()).length === 0;
            return String(form[key] ?? "").trim() === "";
        })
        .map(key => workOrderFieldLabels[key] || String(key));
}

export function formatWorkOrderTime(value?: string | null): string {
    if (!value) return "—";
    const date = new Date(value);
    return Number.isNaN(date.getTime()) ? "—" : date.toLocaleString();
}

export function formatWorkOrderSize(bytes?: number | null): string {
    if (bytes == null) return "—";
    if (bytes < 1024) return `${bytes} B`;
    const units = ["KB", "MB", "GB", "TB"];
    let value = bytes / 1024;
    let index = 0;
    while (value >= 1024 && index < units.length - 1) {
        value /= 1024;
        index += 1;
    }
    return `${value.toFixed(1)} ${units[index]}`;
}

export function formatWorkOrderDuration(seconds?: number | null): string {
    if (seconds == null || seconds < 0) return "—";
    const total = Math.round(seconds);
    const hours = Math.floor(total / 3600);
    const minutes = Math.floor((total % 3600) / 60);
    const rest = total % 60;
    if (hours > 0) return `${hours}时${minutes}分`;
    if (minutes > 0) return `${minutes}分${rest}秒`;
    return `${rest}秒`;
}

interface CameraLike {
    channelId: number;
    channelName?: string;
    files?: Array<{ fileSize?: number | null; timeLen?: number | null }>;
}

export function workOrderCameraNames(snapshot: { cameras?: CameraLike[] }): string {
    return (snapshot.cameras || []).map(camera => camera.channelName || `通道 ${camera.channelId}`).join("、");
}

/** 通道行只需要身份信息，不需要分片，便于脱离快照单测。 */
interface CameraIdentityLike {
    channelId: number;
    channelCode?: string;
    channelName?: string;
    deviceId?: string;
    deviceName?: string;
    state?: string;
}

export interface WorkOrderChannelRow {
    key: string;
    deviceName: string;
    deviceId: string;
    channelName: string;
    channelId: string;
    state: string;
}

/**
 * 通道明细弹窗的行数据：设备名称 / 设备ID / 通道名称 / 通道ID。
 * 「通道ID」展示国标编码（运维在设备上看到的那个），编码缺失时才回退成内部编号。
 */
export function workOrderChannelRows(cameras: CameraIdentityLike[] = []): WorkOrderChannelRow[] {
    return cameras.map(camera => ({
        key: String(camera.channelId),
        deviceName: camera.deviceName || "—",
        deviceId: camera.deviceId || "—",
        channelName: camera.channelName || `通道 ${camera.channelId}`,
        channelId: camera.channelCode || String(camera.channelId),
        state: camera.state || "unknown"
    }));
}

export function workOrderSliceCount(snapshot: { cameras?: CameraLike[] }): number {
    return (snapshot.cameras || []).reduce((total, camera) => total + (camera.files || []).length, 0);
}

export function workOrderTotalBytes(snapshot: { cameras?: CameraLike[] }): number {
    return (snapshot.cameras || []).reduce(
        (total, camera) => total + (camera.files || []).reduce((inner, file) => inner + (file.fileSize || 0), 0),
        0
    );
}
