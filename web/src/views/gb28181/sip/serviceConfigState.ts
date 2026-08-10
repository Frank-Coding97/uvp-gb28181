export interface StaticServiceConfigDraft {
    defaultChannelStreamTransport: "UDP" | "TCP-Active" | "TCP-Passive";
    globalSubscriptionItems: Array<"catalog" | "mobile_position" | "alarm" | "ptz_precise_position">;
    defaultChannelAudioEnabled: boolean;
    saveMobilePositionHistory: boolean;
    positionHistoryRetentionDays: number;
    sdpExtension: boolean;
    ptzSpeed: number;
    syncChannelsOnOnline: boolean;
    sipLogEnabled: boolean;
    sipLogRetentionDays: number;
    ignoreChannelOfflineStatusNotify: boolean;
    onlineOnHeartbeat: boolean;
    saveAlarmMessages: boolean;
    sipTimeoutSec: number;
    preallocationMode: boolean;
    playback: {
        defaultProtocol: "ws-flv" | "http-flv" | "hls" | "webrtc";
        autoInvite: boolean;
        playTimeoutMs: number;
        cloudRecordingEnabled: boolean;
        onDemandLive: boolean;
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
    "新通道默认流传输模式",
    "全局订阅项目",
    "全局通道开启音频",
    "保存移动位置历史轨迹",
    "位置历史保留天数（天）",
    "扩展 SDP 兼容模式",
    "云台默认速度",
    "设备上线时同步通道",
    "是否开启SIP日志",
    "SIP 日志保留天数（天）",
    "忽略通道离线/异常通知",
    "心跳恢复设备在线状态",
    "是否存储报警消息",
    "SIP 命令超时时间（秒）",
    "预分配模式"
] as const;

export const playbackServiceConfigLabels = [
    "默认播放协议",
    "自动点播",
    "点播超时时间（毫秒）",
    "云端录像",
    "按需直播"
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
        defaultChannelStreamTransport: "TCP-Passive",
        globalSubscriptionItems: [],
        defaultChannelAudioEnabled: true,
        saveMobilePositionHistory: true,
        positionHistoryRetentionDays: 7,
        sdpExtension: false,
        ptzSpeed: 6,
        syncChannelsOnOnline: true,
        sipLogEnabled: false,
        sipLogRetentionDays: 7,
        ignoreChannelOfflineStatusNotify: false,
        onlineOnHeartbeat: true,
        saveAlarmMessages: true,
        sipTimeoutSec: 10,
        preallocationMode: false,
        playback: {
            defaultProtocol: "ws-flv",
            autoInvite: true,
            playTimeoutMs: 10000,
            cloudRecordingEnabled: false,
            onDemandLive: true
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
