export interface StaticServiceConfigDraft {
    saveMobilePositionHistory: boolean;
    positionHistoryRetentionDays: number;
    sdpExtension: boolean;
    ptzSpeed: number;
    syncChannelsOnOnline: boolean;
    sipLogEnabled: boolean;
    ignoreChannelOfflineStatusNotify: boolean;
    onlineOnHeartbeat: boolean;
    saveAlarmMessages: boolean;
    sipTimeoutSec: number;
    preallocationMode: boolean;
    playback: {
        autoInvite: boolean;
        inviteTimeoutMs: number;
        recordPushStream: boolean;
        cloudRecording: boolean;
        stopWhenUnwatched: boolean;
        pushAuth: boolean;
    };
    cascade: {
        parentInviteTimeoutMs: number;
        intercomStreamMode: string;
        sendStatusChangeMessages: boolean;
        useCustomSsrc: boolean;
        offlineRetryIntervalSec: number;
        renewalMode: boolean;
        usePushStatusAsChannelStatus: boolean;
    };
}

export const staticServiceConfigLabels = [
    "保存移动位置历史轨迹",
    "位置历史保留天数（天）",
    "扩展 SDP 兼容模式",
    "云台默认速度",
    "设备上线时同步通道",
    "是否开启SIP日志",
    "忽略通道离线/异常通知",
    "心跳恢复设备在线状态",
    "是否存储报警消息",
    "SIP 命令超时时间（秒）",
    "预分配模式"
] as const;

export const playbackServiceConfigLabels = [
    "自动点播",
    "点播超时时间（毫秒）",
    "推流是否录制",
    "云端录像",
    "是否开启无人观看自动停止",
    "推流鉴权"
] as const;

export const cascadeServiceConfigLabels = [
    "上级平台点播超时时间",
    "国标级联对讲流模式",
    "设备/通道状态变化时发送消息",
    "是否使用自定义的ssrc",
    "国标级联离线久重试间隔（秒）",
    "国标续订方式",
    "使用推流状态作为推流通道状态"
] as const;

export function createStaticServiceConfigDraft(): StaticServiceConfigDraft {
    return {
        saveMobilePositionHistory: true,
        positionHistoryRetentionDays: 7,
        sdpExtension: false,
        ptzSpeed: 6,
        syncChannelsOnOnline: true,
        sipLogEnabled: false,
        ignoreChannelOfflineStatusNotify: false,
        onlineOnHeartbeat: true,
        saveAlarmMessages: true,
        sipTimeoutSec: 10,
        preallocationMode: false,
        playback: {
            autoInvite: true,
            inviteTimeoutMs: 10000,
            recordPushStream: false,
            cloudRecording: false,
            stopWhenUnwatched: true,
            pushAuth: false
        },
        cascade: {
            parentInviteTimeoutMs: 60000,
            intercomStreamMode: "TCP被动",
            sendStatusChangeMessages: true,
            useCustomSsrc: false,
            offlineRetryIntervalSec: 60,
            renewalMode: true,
            usePushStatusAsChannelStatus: false
        }
    };
}
