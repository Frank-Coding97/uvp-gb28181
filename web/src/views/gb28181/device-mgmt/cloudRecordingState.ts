export type CloudRecordingState =
    | "disabled"
    | "starting"
    | "recording"
    | "waiting"
    | "stopping"
    | "failed"
    | string;

export type CloudRecordingTone = "neutral" | "success" | "warning" | "danger";

export interface CloudRecordingStateMeta {
    label: string;
    tone: CloudRecordingTone;
    loading: boolean;
    tooltip: string;
}

export function cloudRecordingStateMeta(state: CloudRecordingState | null | undefined, error = ""): CloudRecordingStateMeta {
    switch (state) {
        case "disabled":
            return { label: "已关闭", tone: "neutral", loading: false, tooltip: "" };
        case "starting":
            return { label: "启动中", tone: "warning", loading: true, tooltip: "" };
        case "recording":
            return { label: "录像中", tone: "success", loading: false, tooltip: "" };
        case "waiting":
            return { label: "等待设备/流", tone: "warning", loading: false, tooltip: error };
        case "stopping":
            return { label: "停止中", tone: "warning", loading: true, tooltip: "" };
        case "failed":
            return { label: "失败", tone: "danger", loading: false, tooltip: error };
        default:
            return { label: "未知状态", tone: "neutral", loading: false, tooltip: error };
    }
}
