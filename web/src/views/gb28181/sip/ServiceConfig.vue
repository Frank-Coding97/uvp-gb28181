<script setup lang="ts">
import { Message } from "@arco-design/web-vue";
import { Check, Pencil, X } from "lucide-vue-next";
import { computed, onMounted, reactive, ref } from "vue";
import { getDictItemsByDictCodeAPI } from "@/api/dictionary";
import {
    fetchPTZDefaultSpeedConfig,
    fetchDefaultChannelStreamTransportConfig,
    fetchDefaultPlaybackProtocolConfig,
    fetchGlobalSubscriptionConfig,
    fetchDefaultChannelAudioConfig,
    fetchPositionHistoryConfig,
    fetchSDPExtensionConfig,
    fetchSIPLogConfig,
    fetchSyncChannelsOnOnlineConfig,
    fetchOnlineOnHeartbeatConfig,
    fetchSaveAlarmMessagesConfig,
    fetchSIPCommandTimeoutConfig,
    fetchPreallocationModeConfig,
    fetchIgnoreChannelOfflineStatusNotifyConfig,
    updatePositionHistoryConfig,
    updatePTZDefaultSpeedConfig,
    updateDefaultChannelStreamTransportConfig,
    updateDefaultPlaybackProtocolConfig,
    updateGlobalSubscriptionConfig,
    updateDefaultChannelAudioConfig,
    updateSIPLogConfig,
    updateSDPExtensionConfig,
    updateSyncChannelsOnOnlineConfig,
    updateOnlineOnHeartbeatConfig,
    updateSaveAlarmMessagesConfig,
    updateSIPCommandTimeoutConfig,
    updatePreallocationModeConfig,
    updateIgnoreChannelOfflineStatusNotifyConfig,
    type PlaybackProtocol
} from "@/api/gb28181";
import { useDevicesSize } from "@/hooks/useDevicesSize";
import {
    PLAYBACK_PROTOCOL_DICT_CODE,
    playbackProtocolOptionsFromDictionary
} from "../playbackProtocol";
import { createStaticServiceConfigDraft } from "./serviceConfigState";

const { isMobile } = useDevicesSize();
const activeTab = ref("gb");
const draft = reactive(createStaticServiceConfigDraft());
const isEditing = ref(false);
const positionHistoryLoading = ref(true);
const positionHistorySaving = ref(false);
const positionHistoryReady = ref(false);
const sdpExtensionLoading = ref(true);
const sdpExtensionSaving = ref(false);
const sdpExtensionReady = ref(false);
const ptzDefaultSpeedLoading = ref(true);
const ptzDefaultSpeedSaving = ref(false);
const ptzDefaultSpeedReady = ref(false);
const defaultChannelStreamTransportLoading = ref(true);
const defaultChannelStreamTransportSaving = ref(false);
const defaultChannelStreamTransportReady = ref(false);
const defaultPlaybackProtocolLoading = ref(true);
const defaultPlaybackProtocolSaving = ref(false);
const defaultPlaybackProtocolReady = ref(false);
const globalSubscriptionLoading = ref(true);
const globalSubscriptionSaving = ref(false);
const globalSubscriptionReady = ref(false);
const defaultChannelAudioLoading = ref(true);
const defaultChannelAudioSaving = ref(false);
const defaultChannelAudioReady = ref(false);
const syncChannelsOnOnlineLoading = ref(true);
const syncChannelsOnOnlineSaving = ref(false);
const syncChannelsOnOnlineReady = ref(false);
const onlineOnHeartbeatLoading = ref(true);
const onlineOnHeartbeatSaving = ref(false);
const onlineOnHeartbeatReady = ref(false);
const saveAlarmMessagesLoading = ref(true);
const saveAlarmMessagesSaving = ref(false);
const saveAlarmMessagesReady = ref(false);
const sipCommandTimeoutLoading = ref(true);
const sipCommandTimeoutSaving = ref(false);
const sipCommandTimeoutReady = ref(false);
const preallocationModeLoading = ref(true);
const preallocationModeSaving = ref(false);
const preallocationModeReady = ref(false);
const ignoreChannelOfflineStatusNotifyLoading = ref(true);
const ignoreChannelOfflineStatusNotifySaving = ref(false);
const ignoreChannelOfflineStatusNotifyReady = ref(false);
const sipLogLoading = ref(true);
const sipLogSaving = ref(false);
const sipLogReady = ref(false);
const sipLogApplied = ref(true);
const savedPositionHistoryEnabled = ref(true);
const savedPositionHistoryRetentionDays = ref(7);
const savedSDPExtensionEnabled = ref(false);
const savedPTZDefaultSpeed = ref(6);
const savedDefaultChannelStreamTransport = ref<"UDP" | "TCP-Active" | "TCP-Passive">("TCP-Passive");
const savedDefaultPlaybackProtocol = ref<PlaybackProtocol>("ws-flv");
const playbackProtocolOptions = ref(playbackProtocolOptionsFromDictionary([]));
const savedGlobalSubscriptionItems = ref<Array<"catalog" | "mobile_position" | "alarm" | "ptz_precise_position">>([]);
const savedDefaultChannelAudioEnabled = ref(true);
const savedSyncChannelsOnOnline = ref(true);
const savedOnlineOnHeartbeat = ref(true);
const savedSaveAlarmMessages = ref(true);
const savedSIPCommandTimeoutSec = ref(10);
const savedPreallocationMode = ref(false);
const savedIgnoreChannelOfflineStatusNotify = ref(false);
const savedSIPLogEnabled = ref(false);
const positionHistoryChanged = computed(
    () =>
        draft.saveMobilePositionHistory !== savedPositionHistoryEnabled.value ||
        draft.positionHistoryRetentionDays !== savedPositionHistoryRetentionDays.value
);
const sdpExtensionChanged = computed(() => draft.sdpExtension !== savedSDPExtensionEnabled.value);
const ptzDefaultSpeedChanged = computed(() => draft.ptzSpeed !== savedPTZDefaultSpeed.value);
const defaultChannelStreamTransportChanged = computed(
    () => draft.defaultChannelStreamTransport !== savedDefaultChannelStreamTransport.value
);
const defaultPlaybackProtocolChanged = computed(
    () => draft.playback.defaultProtocol !== savedDefaultPlaybackProtocol.value
);
const globalSubscriptionChanged = computed(
    () => JSON.stringify(draft.globalSubscriptionItems) !== JSON.stringify(savedGlobalSubscriptionItems.value)
);
const defaultChannelAudioChanged = computed(
    () => draft.defaultChannelAudioEnabled !== savedDefaultChannelAudioEnabled.value
);
const syncChannelsOnOnlineChanged = computed(
    () => draft.syncChannelsOnOnline !== savedSyncChannelsOnOnline.value
);
const onlineOnHeartbeatChanged = computed(() => draft.onlineOnHeartbeat !== savedOnlineOnHeartbeat.value);
const saveAlarmMessagesChanged = computed(() => draft.saveAlarmMessages !== savedSaveAlarmMessages.value);
const sipCommandTimeoutChanged = computed(() => draft.sipTimeoutSec !== savedSIPCommandTimeoutSec.value);
const preallocationModeChanged = computed(() => draft.preallocationMode !== savedPreallocationMode.value);
const ignoreChannelOfflineStatusNotifyChanged = computed(
    () => draft.ignoreChannelOfflineStatusNotify !== savedIgnoreChannelOfflineStatusNotify.value
);
const sipLogChanged = computed(() => draft.sipLogEnabled !== savedSIPLogEnabled.value);
const hasChanges = computed(
    () =>
        positionHistoryChanged.value ||
        sdpExtensionChanged.value ||
        ptzDefaultSpeedChanged.value ||
        defaultChannelStreamTransportChanged.value ||
        defaultPlaybackProtocolChanged.value ||
        globalSubscriptionChanged.value ||
        defaultChannelAudioChanged.value ||
        syncChannelsOnOnlineChanged.value ||
        onlineOnHeartbeatChanged.value ||
        saveAlarmMessagesChanged.value ||
        sipCommandTimeoutChanged.value ||
        preallocationModeChanged.value ||
        ignoreChannelOfflineStatusNotifyChanged.value ||
        sipLogChanged.value
);
const configLoading = computed(
    () =>
        positionHistoryLoading.value ||
        sdpExtensionLoading.value ||
        ptzDefaultSpeedLoading.value ||
        defaultChannelStreamTransportLoading.value ||
        defaultPlaybackProtocolLoading.value ||
        globalSubscriptionLoading.value ||
        defaultChannelAudioLoading.value ||
        syncChannelsOnOnlineLoading.value ||
        onlineOnHeartbeatLoading.value ||
        saveAlarmMessagesLoading.value ||
        sipCommandTimeoutLoading.value ||
        preallocationModeLoading.value ||
        ignoreChannelOfflineStatusNotifyLoading.value ||
        sipLogLoading.value
);
const configSaving = computed(
    () =>
        positionHistorySaving.value ||
        sdpExtensionSaving.value ||
        ptzDefaultSpeedSaving.value ||
        defaultChannelStreamTransportSaving.value ||
        defaultPlaybackProtocolSaving.value ||
        globalSubscriptionSaving.value ||
        defaultChannelAudioSaving.value ||
        syncChannelsOnOnlineSaving.value ||
        onlineOnHeartbeatSaving.value ||
        saveAlarmMessagesSaving.value ||
        sipCommandTimeoutSaving.value ||
        preallocationModeSaving.value ||
        ignoreChannelOfflineStatusNotifySaving.value ||
        sipLogSaving.value
);
const configReady = computed(
    () =>
        positionHistoryReady.value &&
        sdpExtensionReady.value &&
        ptzDefaultSpeedReady.value &&
        defaultChannelStreamTransportReady.value &&
        defaultPlaybackProtocolReady.value &&
        globalSubscriptionReady.value &&
        defaultChannelAudioReady.value &&
        syncChannelsOnOnlineReady.value &&
        onlineOnHeartbeatReady.value &&
        saveAlarmMessagesReady.value &&
        sipCommandTimeoutReady.value &&
        preallocationModeReady.value &&
        ignoreChannelOfflineStatusNotifyReady.value &&
        sipLogReady.value
);
const formLayout = computed(() => (isMobile.value ? "vertical" : "horizontal"));

async function loadPositionHistoryConfig() {
    positionHistoryLoading.value = true;
    try {
        const response = await fetchPositionHistoryConfig();
        if (response.code !== 0) throw new Error(response.message || "加载配置失败");
        const retentionDays = response.data.retentionDays ?? 7;
        draft.saveMobilePositionHistory = response.data.enabled;
        draft.positionHistoryRetentionDays = retentionDays;
        savedPositionHistoryEnabled.value = response.data.enabled;
        savedPositionHistoryRetentionDays.value = retentionDays;
        positionHistoryReady.value = true;
    } catch (error: any) {
        Message.error(error?.message || "加载移动位置历史轨迹配置失败");
    } finally {
        positionHistoryLoading.value = false;
    }
}

async function loadSDPExtensionConfig() {
    sdpExtensionLoading.value = true;
    try {
        const response = await fetchSDPExtensionConfig();
        if (response.code !== 0) throw new Error(response.message || "加载配置失败");
        draft.sdpExtension = response.data.enabled;
        savedSDPExtensionEnabled.value = response.data.enabled;
        sdpExtensionReady.value = true;
    } catch (error: any) {
        Message.error(error?.message || "加载扩展 SDP 兼容模式失败");
    } finally {
        sdpExtensionLoading.value = false;
    }
}

async function loadPTZDefaultSpeedConfig() {
    ptzDefaultSpeedLoading.value = true;
    try {
        const response = await fetchPTZDefaultSpeedConfig();
        if (response.code !== 0) throw new Error(response.message || "加载配置失败");
        draft.ptzSpeed = response.data.level;
        savedPTZDefaultSpeed.value = response.data.level;
        ptzDefaultSpeedReady.value = true;
    } catch (error: any) {
        Message.error(error?.message || "加载云台默认速度失败");
    } finally {
        ptzDefaultSpeedLoading.value = false;
    }
}

async function loadDefaultChannelStreamTransportConfig() {
    defaultChannelStreamTransportLoading.value = true;
    try {
        const response = await fetchDefaultChannelStreamTransportConfig();
        if (response.code !== 0) throw new Error(response.message || "加载配置失败");
        draft.defaultChannelStreamTransport = response.data.transport;
        savedDefaultChannelStreamTransport.value = response.data.transport;
        defaultChannelStreamTransportReady.value = true;
    } catch (error: any) {
        Message.error(error?.message || "加载新通道默认流传输模式失败");
    } finally {
        defaultChannelStreamTransportLoading.value = false;
    }
}

async function loadDefaultPlaybackProtocolConfig() {
    defaultPlaybackProtocolLoading.value = true;
    try {
        const response = await fetchDefaultPlaybackProtocolConfig();
        if (response.code !== 0) throw new Error(response.message || "加载配置失败");
        draft.playback.defaultProtocol = response.data.protocol;
        savedDefaultPlaybackProtocol.value = response.data.protocol;
        defaultPlaybackProtocolReady.value = true;
    } catch (error: any) {
        Message.error(error?.message || "加载默认播放协议失败");
    } finally {
        defaultPlaybackProtocolLoading.value = false;
    }
}

async function loadPlaybackProtocolOptions() {
    try {
        const response = await getDictItemsByDictCodeAPI(PLAYBACK_PROTOCOL_DICT_CODE);
        if (response.code === 0) {
            playbackProtocolOptions.value = playbackProtocolOptionsFromDictionary(response.data?.list);
        }
    } catch {
        playbackProtocolOptions.value = playbackProtocolOptionsFromDictionary([]);
    }
}

async function loadGlobalSubscriptionConfig() {
    globalSubscriptionLoading.value = true;
    try {
        const response = await fetchGlobalSubscriptionConfig();
        if (response.code !== 0) throw new Error(response.message || "加载配置失败");
        draft.globalSubscriptionItems = [...response.data.items];
        savedGlobalSubscriptionItems.value = [...response.data.items];
        globalSubscriptionReady.value = true;
    } catch (error: any) {
        Message.error(error?.message || "加载全局订阅项目失败");
    } finally {
        globalSubscriptionLoading.value = false;
    }
}

async function loadDefaultChannelAudioConfig() {
    defaultChannelAudioLoading.value = true;
    try {
        const response = await fetchDefaultChannelAudioConfig();
        if (response.code !== 0) throw new Error(response.message || "加载配置失败");
        draft.defaultChannelAudioEnabled = response.data.enabled;
        savedDefaultChannelAudioEnabled.value = response.data.enabled;
        defaultChannelAudioReady.value = true;
    } catch (error: any) {
        Message.error(error?.message || "加载全局通道音频配置失败");
    } finally {
        defaultChannelAudioLoading.value = false;
    }
}

async function loadSyncChannelsOnOnlineConfig() {
    syncChannelsOnOnlineLoading.value = true;
    try {
        const response = await fetchSyncChannelsOnOnlineConfig();
        if (response.code !== 0) throw new Error(response.message || "加载配置失败");
        draft.syncChannelsOnOnline = response.data.enabled;
        savedSyncChannelsOnOnline.value = response.data.enabled;
        syncChannelsOnOnlineReady.value = true;
    } catch (error: any) {
        Message.error(error?.message || "加载设备上线同步通道配置失败");
    } finally {
        syncChannelsOnOnlineLoading.value = false;
    }
}

async function loadOnlineOnHeartbeatConfig() {
    onlineOnHeartbeatLoading.value = true;
    try {
        const response = await fetchOnlineOnHeartbeatConfig();
        if (response.code !== 0) throw new Error(response.message || "加载配置失败");
        draft.onlineOnHeartbeat = response.data.enabled;
        savedOnlineOnHeartbeat.value = response.data.enabled;
        onlineOnHeartbeatReady.value = true;
    } catch (error: any) {
        Message.error(error?.message || "加载收到心跳恢复设备上线配置失败");
    } finally {
        onlineOnHeartbeatLoading.value = false;
    }
}

async function loadSaveAlarmMessagesConfig() {
    saveAlarmMessagesLoading.value = true;
    try {
        const response = await fetchSaveAlarmMessagesConfig();
        if (response.code !== 0) throw new Error(response.message || "加载配置失败");
        draft.saveAlarmMessages = response.data.enabled;
        savedSaveAlarmMessages.value = response.data.enabled;
        saveAlarmMessagesReady.value = true;
    } catch (error: any) {
        Message.error(error?.message || "加载报警消息存储配置失败");
    } finally {
        saveAlarmMessagesLoading.value = false;
    }
}

async function loadSIPCommandTimeoutConfig() {
    sipCommandTimeoutLoading.value = true;
    try {
        const response = await fetchSIPCommandTimeoutConfig();
        if (response.code !== 0) throw new Error(response.message || "加载配置失败");
        draft.sipTimeoutSec = response.data.timeoutSec;
        savedSIPCommandTimeoutSec.value = response.data.timeoutSec;
        sipCommandTimeoutReady.value = true;
    } catch (error: any) {
        Message.error(error?.message || "加载 SIP 命令超时时间失败");
    } finally {
        sipCommandTimeoutLoading.value = false;
    }
}

async function loadPreallocationModeConfig() {
    preallocationModeLoading.value = true;
    try {
        const response = await fetchPreallocationModeConfig();
        if (response.code !== 0) throw new Error(response.message || "加载配置失败");
        draft.preallocationMode = response.data.enabled;
        savedPreallocationMode.value = response.data.enabled;
        preallocationModeReady.value = true;
    } catch (error: any) {
        Message.error(error?.message || "加载预分配模式失败");
    } finally {
        preallocationModeLoading.value = false;
    }
}

async function loadIgnoreChannelOfflineStatusNotifyConfig() {
    ignoreChannelOfflineStatusNotifyLoading.value = true;
    try {
        const response = await fetchIgnoreChannelOfflineStatusNotifyConfig();
        if (response.code !== 0) throw new Error(response.message || "加载配置失败");
        draft.ignoreChannelOfflineStatusNotify = response.data.enabled;
        savedIgnoreChannelOfflineStatusNotify.value = response.data.enabled;
        ignoreChannelOfflineStatusNotifyReady.value = true;
    } catch (error: any) {
        Message.error(error?.message || "加载忽略通道离线/异常通知配置失败");
    } finally {
        ignoreChannelOfflineStatusNotifyLoading.value = false;
    }
}

async function loadSIPLogConfig() {
    sipLogLoading.value = true;
    try {
        const response = await fetchSIPLogConfig();
        if (response.code !== 0) throw new Error(response.message || "加载配置失败");
        draft.sipLogEnabled = response.data.enabled;
        savedSIPLogEnabled.value = response.data.enabled;
        sipLogApplied.value = response.data.applied;
        sipLogReady.value = true;
    } catch (error: any) {
        Message.error(error?.message || "加载 SIP 日志配置失败");
    } finally {
        sipLogLoading.value = false;
    }
}

function startEditing() {
    draft.saveMobilePositionHistory = savedPositionHistoryEnabled.value;
    draft.positionHistoryRetentionDays = savedPositionHistoryRetentionDays.value;
    draft.sdpExtension = savedSDPExtensionEnabled.value;
    draft.ptzSpeed = savedPTZDefaultSpeed.value;
    draft.defaultChannelStreamTransport = savedDefaultChannelStreamTransport.value;
    draft.playback.defaultProtocol = savedDefaultPlaybackProtocol.value;
    draft.globalSubscriptionItems = [...savedGlobalSubscriptionItems.value];
    draft.defaultChannelAudioEnabled = savedDefaultChannelAudioEnabled.value;
    draft.syncChannelsOnOnline = savedSyncChannelsOnOnline.value;
    draft.onlineOnHeartbeat = savedOnlineOnHeartbeat.value;
    draft.saveAlarmMessages = savedSaveAlarmMessages.value;
    draft.sipTimeoutSec = savedSIPCommandTimeoutSec.value;
    draft.preallocationMode = savedPreallocationMode.value;
    draft.ignoreChannelOfflineStatusNotify = savedIgnoreChannelOfflineStatusNotify.value;
    draft.sipLogEnabled = savedSIPLogEnabled.value;
    isEditing.value = true;
}

function cancelEditing() {
    draft.saveMobilePositionHistory = savedPositionHistoryEnabled.value;
    draft.positionHistoryRetentionDays = savedPositionHistoryRetentionDays.value;
    draft.sdpExtension = savedSDPExtensionEnabled.value;
    draft.ptzSpeed = savedPTZDefaultSpeed.value;
    draft.defaultChannelStreamTransport = savedDefaultChannelStreamTransport.value;
    draft.playback.defaultProtocol = savedDefaultPlaybackProtocol.value;
    draft.globalSubscriptionItems = [...savedGlobalSubscriptionItems.value];
    draft.defaultChannelAudioEnabled = savedDefaultChannelAudioEnabled.value;
    draft.syncChannelsOnOnline = savedSyncChannelsOnOnline.value;
    draft.onlineOnHeartbeat = savedOnlineOnHeartbeat.value;
    draft.saveAlarmMessages = savedSaveAlarmMessages.value;
    draft.sipTimeoutSec = savedSIPCommandTimeoutSec.value;
    draft.preallocationMode = savedPreallocationMode.value;
    draft.ignoreChannelOfflineStatusNotify = savedIgnoreChannelOfflineStatusNotify.value;
    draft.sipLogEnabled = savedSIPLogEnabled.value;
    isEditing.value = false;
}

async function saveConfig() {
    if (!configReady.value || !isEditing.value) return;
    if (!hasChanges.value) {
        isEditing.value = false;
        return;
    }
    try {
        if (positionHistoryChanged.value) {
            positionHistorySaving.value = true;
            const response = await updatePositionHistoryConfig({
                enabled: draft.saveMobilePositionHistory,
                retentionDays: draft.positionHistoryRetentionDays
            });
            if (response.code !== 0) throw new Error(response.message || "保存配置失败");
            const retentionDays = response.data.retentionDays ?? draft.positionHistoryRetentionDays;
            draft.saveMobilePositionHistory = response.data.enabled;
            draft.positionHistoryRetentionDays = retentionDays;
            savedPositionHistoryEnabled.value = response.data.enabled;
            savedPositionHistoryRetentionDays.value = retentionDays;
            positionHistorySaving.value = false;
        }
        if (sdpExtensionChanged.value) {
            sdpExtensionSaving.value = true;
            const response = await updateSDPExtensionConfig(draft.sdpExtension);
            if (response.code !== 0) throw new Error(response.message || "保存配置失败");
            draft.sdpExtension = response.data.enabled;
            savedSDPExtensionEnabled.value = response.data.enabled;
            sdpExtensionSaving.value = false;
        }
        if (ptzDefaultSpeedChanged.value) {
            ptzDefaultSpeedSaving.value = true;
            const response = await updatePTZDefaultSpeedConfig(draft.ptzSpeed);
            if (response.code !== 0) throw new Error(response.message || "保存配置失败");
            draft.ptzSpeed = response.data.level;
            savedPTZDefaultSpeed.value = response.data.level;
            ptzDefaultSpeedSaving.value = false;
        }
        if (defaultChannelStreamTransportChanged.value) {
            defaultChannelStreamTransportSaving.value = true;
            const response = await updateDefaultChannelStreamTransportConfig(draft.defaultChannelStreamTransport);
            if (response.code !== 0) throw new Error(response.message || "保存配置失败");
            draft.defaultChannelStreamTransport = response.data.transport;
            savedDefaultChannelStreamTransport.value = response.data.transport;
            defaultChannelStreamTransportSaving.value = false;
        }
        if (defaultPlaybackProtocolChanged.value) {
            defaultPlaybackProtocolSaving.value = true;
            const response = await updateDefaultPlaybackProtocolConfig(draft.playback.defaultProtocol);
            if (response.code !== 0) throw new Error(response.message || "保存配置失败");
            draft.playback.defaultProtocol = response.data.protocol;
            savedDefaultPlaybackProtocol.value = response.data.protocol;
            defaultPlaybackProtocolSaving.value = false;
        }
        if (globalSubscriptionChanged.value) {
            globalSubscriptionSaving.value = true;
            const response = await updateGlobalSubscriptionConfig(draft.globalSubscriptionItems);
            if (response.code !== 0) throw new Error(response.message || "保存配置失败");
            draft.globalSubscriptionItems = [...response.data.items];
            savedGlobalSubscriptionItems.value = [...response.data.items];
            globalSubscriptionSaving.value = false;
        }
        if (defaultChannelAudioChanged.value) {
            defaultChannelAudioSaving.value = true;
            const response = await updateDefaultChannelAudioConfig(draft.defaultChannelAudioEnabled);
            if (response.code !== 0) throw new Error(response.message || "保存配置失败");
            draft.defaultChannelAudioEnabled = response.data.enabled;
            savedDefaultChannelAudioEnabled.value = response.data.enabled;
            defaultChannelAudioSaving.value = false;
        }
        if (syncChannelsOnOnlineChanged.value) {
            syncChannelsOnOnlineSaving.value = true;
            const response = await updateSyncChannelsOnOnlineConfig(draft.syncChannelsOnOnline);
            if (response.code !== 0) throw new Error(response.message || "保存配置失败");
            draft.syncChannelsOnOnline = response.data.enabled;
            savedSyncChannelsOnOnline.value = response.data.enabled;
            syncChannelsOnOnlineSaving.value = false;
        }
        if (onlineOnHeartbeatChanged.value) {
            onlineOnHeartbeatSaving.value = true;
            const response = await updateOnlineOnHeartbeatConfig(draft.onlineOnHeartbeat);
            if (response.code !== 0) throw new Error(response.message || "保存配置失败");
            draft.onlineOnHeartbeat = response.data.enabled;
            savedOnlineOnHeartbeat.value = response.data.enabled;
            onlineOnHeartbeatSaving.value = false;
        }
        if (saveAlarmMessagesChanged.value) {
            saveAlarmMessagesSaving.value = true;
            const response = await updateSaveAlarmMessagesConfig(draft.saveAlarmMessages);
            if (response.code !== 0) throw new Error(response.message || "保存配置失败");
            draft.saveAlarmMessages = response.data.enabled;
            savedSaveAlarmMessages.value = response.data.enabled;
            saveAlarmMessagesSaving.value = false;
        }
        if (sipCommandTimeoutChanged.value) {
            sipCommandTimeoutSaving.value = true;
            const response = await updateSIPCommandTimeoutConfig(draft.sipTimeoutSec);
            if (response.code !== 0) throw new Error(response.message || "保存配置失败");
            draft.sipTimeoutSec = response.data.timeoutSec;
            savedSIPCommandTimeoutSec.value = response.data.timeoutSec;
            sipCommandTimeoutSaving.value = false;
        }
        if (preallocationModeChanged.value) {
            preallocationModeSaving.value = true;
            const response = await updatePreallocationModeConfig(draft.preallocationMode);
            if (response.code !== 0) throw new Error(response.message || "保存配置失败");
            draft.preallocationMode = response.data.enabled;
            savedPreallocationMode.value = response.data.enabled;
            preallocationModeSaving.value = false;
        }
        if (ignoreChannelOfflineStatusNotifyChanged.value) {
            ignoreChannelOfflineStatusNotifySaving.value = true;
            const response = await updateIgnoreChannelOfflineStatusNotifyConfig(draft.ignoreChannelOfflineStatusNotify);
            if (response.code !== 0) throw new Error(response.message || "保存配置失败");
            draft.ignoreChannelOfflineStatusNotify = response.data.enabled;
            savedIgnoreChannelOfflineStatusNotify.value = response.data.enabled;
            ignoreChannelOfflineStatusNotifySaving.value = false;
        }
        if (sipLogChanged.value) {
            sipLogSaving.value = true;
            const response = await updateSIPLogConfig(draft.sipLogEnabled);
            if (response.code !== 0) throw new Error(response.message || "保存配置失败");
            draft.sipLogEnabled = response.data.enabled;
            savedSIPLogEnabled.value = response.data.enabled;
            sipLogApplied.value = response.data.applied;
            sipLogSaving.value = false;
            if (response.data.applied === false) {
                Message.error("SIP 日志配置已保存，但 SIP 服务重载失败，请检查服务状态");
                isEditing.value = false;
                return;
            }
        }
        isEditing.value = false;
        Message.success("国标服务配置已更新");
    } catch (error: any) {
        Message.error(error?.message || "保存国标服务配置失败");
    } finally {
        positionHistorySaving.value = false;
        sdpExtensionSaving.value = false;
        ptzDefaultSpeedSaving.value = false;
        defaultChannelStreamTransportSaving.value = false;
        defaultPlaybackProtocolSaving.value = false;
        globalSubscriptionSaving.value = false;
        defaultChannelAudioSaving.value = false;
        syncChannelsOnOnlineSaving.value = false;
        onlineOnHeartbeatSaving.value = false;
        saveAlarmMessagesSaving.value = false;
        sipCommandTimeoutSaving.value = false;
        preallocationModeSaving.value = false;
        ignoreChannelOfflineStatusNotifySaving.value = false;
        sipLogSaving.value = false;
    }
}

onMounted(() =>
    Promise.all([
        loadPositionHistoryConfig(),
        loadSDPExtensionConfig(),
        loadPTZDefaultSpeedConfig(),
        loadDefaultChannelStreamTransportConfig(),
        loadDefaultPlaybackProtocolConfig(),
        loadPlaybackProtocolOptions(),
        loadGlobalSubscriptionConfig(),
        loadDefaultChannelAudioConfig(),
        loadSyncChannelsOnOnlineConfig(),
        loadOnlineOnHeartbeatConfig(),
        loadSaveAlarmMessagesConfig(),
        loadSIPCommandTimeoutConfig(),
        loadPreallocationModeConfig(),
        loadIgnoreChannelOfflineStatusNotifyConfig(),
        loadSIPLogConfig()
    ])
);
</script>

<template>
    <div class="snow-fill service-config-page">
        <a-tabs
                v-model:active-key="activeTab"
                class="uvp-system-tabs service-config-tabs"
                :animation="true"
                lazy-load
            >
                <template #extra>
                    <div class="service-config-tabs__actions">
                        <a-button
                            v-if="!isEditing"
                            type="primary"
                            :disabled="configLoading || !configReady"
                            @click="startEditing"
                        >
                            <template #icon><Pencil :size="15" /></template>
                            编辑配置
                        </a-button>
                        <template v-else>
                            <a-button :disabled="configSaving" @click="cancelEditing">
                                <template #icon><X :size="15" /></template>
                                取消
                            </a-button>
                            <a-button
                                type="primary"
                                :loading="configSaving"
                                :disabled="configSaving || !hasChanges"
                                @click="saveConfig"
                            >
                                <template #icon><Check :size="15" /></template>
                                保存配置
                            </a-button>
                        </template>
                    </div>
                </template>
                <a-tab-pane key="gb" title="国标相关">
                    <a-card :bordered="false" class="uvp-system-panel uvp-system-panel--dense mb-4">
                        <a-form class="uvp-system-form" :layout="formLayout" :model="draft" auto-label-width>
                            <a-row :gutter="24">
                                <a-col :span="isMobile ? 24 : 12">
                                    <a-form-item
                                        field="saveMobilePositionHistory"
                                        label="保存移动位置历史轨迹"
                                        tooltip="关闭后仍更新设备和通道的最新位置，不再新增轨迹点。"
                                    >
                                        <a-switch
                                            v-model="draft.saveMobilePositionHistory"
                                            :loading="positionHistoryLoading || positionHistorySaving"
                                            :disabled="!isEditing || positionHistoryLoading || positionHistorySaving || !positionHistoryReady"
                                        >
                                            <template #checked>开启</template>
                                            <template #unchecked>关闭</template>
                                        </a-switch>
                                    </a-form-item>
                                </a-col>
                                <a-col :span="isMobile ? 24 : 12">
                                    <a-form-item
                                        field="positionHistoryRetentionDays"
                                        label="位置历史保留天数（天）"
                                        tooltip="历史轨迹按接收时间自动清理，默认保留 7 天。"
                                    >
                                        <a-input-number
                                            v-model="draft.positionHistoryRetentionDays"
                                            class="service-config-number-input"
                                            :min="1"
                                            :max="365"
                                            :disabled="
                                                !isEditing ||
                                                positionHistoryLoading ||
                                                positionHistorySaving ||
                                                !positionHistoryReady ||
                                                !draft.saveMobilePositionHistory
                                            "
                                        />
                                    </a-form-item>
                                </a-col>
                                <a-col :span="isMobile ? 24 : 12">
                                    <a-form-item
                                        field="sdpExtension"
                                        label="扩展 SDP 兼容模式"
                                        tooltip="为部分兼容性设备在点播和录像回放请求中声明更多视频编码类型，一般设备无需开启。"
                                    >
                                        <a-switch
                                            v-model="draft.sdpExtension"
                                            :loading="sdpExtensionLoading || sdpExtensionSaving"
                                            :disabled="!isEditing || sdpExtensionLoading || sdpExtensionSaving || !sdpExtensionReady"
                                        >
                                            <template #checked>开启</template>
                                            <template #unchecked>关闭</template>
                                        </a-switch>
                                    </a-form-item>
                                </a-col>
                                <a-col :span="isMobile ? 24 : 12">
                                    <a-form-item
                                        field="ptzSpeed"
                                        label="云台默认速度"
                                        tooltip="作为播放弹窗和大屏云台控制的初始档位，可在控制面板临时调整。"
                                    >
                                        <div class="ptz-default-speed">
                                            <span class="ptz-default-speed__edge">慢</span>
                                            <a-slider
                                                v-model="draft.ptzSpeed"
                                                :min="1"
                                                :max="10"
                                                :step="1"
                                                show-ticks
                                                :disabled="!isEditing || ptzDefaultSpeedLoading || ptzDefaultSpeedSaving || !ptzDefaultSpeedReady"
                                                aria-label="云台默认速度"
                                            />
                                            <span class="ptz-default-speed__edge">快</span>
                                            <output class="ptz-default-speed__value">{{ draft.ptzSpeed }} 档</output>
                                        </div>
                                    </a-form-item>
                                </a-col>
                                <a-col :span="isMobile ? 24 : 12">
                                    <a-form-item
                                        field="defaultChannelStreamTransport"
                                        label="新通道默认流传输模式"
                                        tooltip="仅影响之后通过 Catalog 新发现的通道，现有通道保持不变；默认 TCP 被动。"
                                    >
                                        <a-select
                                            v-model="draft.defaultChannelStreamTransport"
                                            class="stream-transport-select"
                                            :disabled="
                                                !isEditing ||
                                                defaultChannelStreamTransportLoading ||
                                                defaultChannelStreamTransportSaving ||
                                                !defaultChannelStreamTransportReady
                                            "
                                        >
                                            <a-option value="UDP">UDP</a-option>
                                            <a-option value="TCP-Active">TCP 主动</a-option>
                                            <a-option value="TCP-Passive">TCP 被动</a-option>
                                        </a-select>
                                    </a-form-item>
                                </a-col>
                                <a-col :span="isMobile ? 24 : 12">
                                    <a-form-item
                                        field="defaultChannelAudioEnabled"
                                        label="全局通道开启音频"
                                        tooltip="仅影响之后通过 Catalog 新发现的通道，现有通道保持原音频设置；默认开启。"
                                    >
                                        <a-switch
                                            v-model="draft.defaultChannelAudioEnabled"
                                            :loading="defaultChannelAudioLoading || defaultChannelAudioSaving"
                                            :disabled="
                                                !isEditing ||
                                                defaultChannelAudioLoading ||
                                                defaultChannelAudioSaving ||
                                                !defaultChannelAudioReady
                                            "
                                        >
                                            <template #checked>开启</template>
                                            <template #unchecked>关闭</template>
                                        </a-switch>
                                    </a-form-item>
                                </a-col>
                                <a-col :span="isMobile ? 24 : 12">
                                    <a-form-item
                                        field="syncChannelsOnOnline"
                                        label="设备上线时同步通道"
                                        tooltip="设备首次上线或离线恢复时，自动向设备查询 Catalog 并同步通道。"
                                    >
                                        <a-switch
                                            v-model="draft.syncChannelsOnOnline"
                                            :loading="syncChannelsOnOnlineLoading || syncChannelsOnOnlineSaving"
                                            :disabled="!isEditing || syncChannelsOnOnlineLoading || syncChannelsOnOnlineSaving || !syncChannelsOnOnlineReady"
                                        >
                                            <template #checked>开启</template>
                                            <template #unchecked>关闭</template>
                                        </a-switch>
                                    </a-form-item>
                                </a-col>
                                <a-col :span="isMobile ? 24 : 12">
                                    <a-form-item
                                        field="sipLogEnabled"
                                        label="是否开启 SIP 日志"
                                        :tooltip="
                                            sipLogApplied
                                                ? '保存后会热重载 SIP 服务，采集原始信令并写入 Trace 存储；默认关闭。'
                                                : '保存后会热重载 SIP 服务，采集原始信令并写入 Trace 存储；默认关闭。配置尚未应用，SIP 服务当前状态与开关不一致。'
                                        "
                                    >
                                        <a-switch
                                            v-model="draft.sipLogEnabled"
                                            :loading="sipLogLoading || sipLogSaving"
                                            :disabled="!isEditing || sipLogLoading || sipLogSaving || !sipLogReady"
                                        >
                                            <template #checked>开启</template>
                                            <template #unchecked>关闭</template>
                                        </a-switch>
                                    </a-form-item>
                                </a-col>
                                <a-col :span="isMobile ? 24 : 12">
                                    <a-form-item
                                        field="ignoreChannelOfflineStatusNotify"
                                        label="忽略通道离线/异常通知"
                                        tooltip="开启后忽略 Catalog NOTIFY 上报的 OFF、VLOST、DEFECT，仅用于兼容错误状态消息；默认关闭。"
                                    >
                                        <a-switch
                                            v-model="draft.ignoreChannelOfflineStatusNotify"
                                            :loading="ignoreChannelOfflineStatusNotifyLoading || ignoreChannelOfflineStatusNotifySaving"
                                            :disabled="
                                                !isEditing ||
                                                ignoreChannelOfflineStatusNotifyLoading ||
                                                ignoreChannelOfflineStatusNotifySaving ||
                                                !ignoreChannelOfflineStatusNotifyReady
                                            "
                                        >
                                            <template #checked>开启</template>
                                            <template #unchecked>关闭</template>
                                        </a-switch>
                                    </a-form-item>
                                </a-col>
                                <a-col :span="isMobile ? 24 : 12">
                                    <a-form-item
                                        field="onlineOnHeartbeat"
                                        label="心跳恢复设备在线状态"
                                        tooltip="开启后，离线设备收到 Keepalive 会恢复为在线；关闭后仍记录最后心跳时间，但保持当前设备状态。默认开启。"
                                    >
                                        <a-switch
                                            v-model="draft.onlineOnHeartbeat"
                                            :loading="onlineOnHeartbeatLoading || onlineOnHeartbeatSaving"
                                            :disabled="!isEditing || onlineOnHeartbeatLoading || onlineOnHeartbeatSaving || !onlineOnHeartbeatReady"
                                        >
                                            <template #checked>开启</template>
                                            <template #unchecked>关闭</template>
                                        </a-switch>
                                    </a-form-item>
                                </a-col>
                                <a-col :span="isMobile ? 24 : 12">
                                    <a-form-item
                                        field="saveAlarmMessages"
                                        label="是否存储报警消息"
                                        tooltip="开启后将设备报警通知写入报警管理；关闭后仍接收和解析报警通知，但不写入报警记录。默认开启。"
                                    >
                                        <a-switch
                                            v-model="draft.saveAlarmMessages"
                                            :loading="saveAlarmMessagesLoading || saveAlarmMessagesSaving"
                                            :disabled="!isEditing || saveAlarmMessagesLoading || saveAlarmMessagesSaving || !saveAlarmMessagesReady"
                                        >
                                            <template #checked>开启</template>
                                            <template #unchecked>关闭</template>
                                        </a-switch>
                                    </a-form-item>
                                </a-col>
                                <a-col :span="isMobile ? 24 : 12">
                                    <a-form-item
                                        field="sipTimeoutSec"
                                        label="SIP 命令超时时间（秒）"
                                        tooltip="控制平台向设备发送 MESSAGE、SUBSCRIBE、直播、回放和对讲 INVITE 时等待响应的默认时长，默认 10 秒。"
                                    >
                                        <a-input-number
                                            v-model="draft.sipTimeoutSec"
                                            class="service-config-number-input"
                                            :min="1"
                                            :max="300"
                                            :disabled="!isEditing || sipCommandTimeoutLoading || sipCommandTimeoutSaving || !sipCommandTimeoutReady"
                                        />
                                    </a-form-item>
                                </a-col>
                                <a-col :span="isMobile ? 24 : 12">
                                    <a-form-item
                                        field="preallocationMode"
                                        label="预分配模式"
                                        tooltip="开启后，未知国标 ID 将被拒绝注册，需要先在设备列表新建设备；默认关闭。"
                                    >
                                        <a-switch
                                            v-model="draft.preallocationMode"
                                            :loading="preallocationModeLoading || preallocationModeSaving"
                                            :disabled="!isEditing || preallocationModeLoading || preallocationModeSaving || !preallocationModeReady"
                                        >
                                            <template #checked>开启</template>
                                            <template #unchecked>关闭</template>
                                        </a-switch>
                                    </a-form-item>
                                </a-col>
                                <a-col :span="24">
                                    <a-form-item
                                        field="globalSubscriptionItems"
                                        label="全局订阅项目"
                                        tooltip="设备下次上线时，为尚未单独配置的订阅项目应用默认值；设备已有订阅配置保持不变。默认不启用。"
                                    >
                                        <a-checkbox-group
                                            v-model="draft.globalSubscriptionItems"
                                            class="global-subscription-items"
                                            :disabled="
                                                !isEditing ||
                                                globalSubscriptionLoading ||
                                                globalSubscriptionSaving ||
                                                !globalSubscriptionReady
                                            "
                                        >
                                            <a-checkbox value="catalog">目录</a-checkbox>
                                            <a-checkbox value="alarm">报警</a-checkbox>
                                            <a-checkbox value="mobile_position">位置</a-checkbox>
                                            <a-checkbox value="ptz_precise_position">PTZ 精准位置变化（2022）</a-checkbox>
                                        </a-checkbox-group>
                                    </a-form-item>
                                </a-col>
                            </a-row>
                        </a-form>
                    </a-card>
                </a-tab-pane>

                <a-tab-pane key="playback" title="播放相关">
                    <a-card :bordered="false" class="uvp-system-panel uvp-system-panel--dense mb-4">
                        <a-form class="uvp-system-form" :layout="formLayout" :model="draft.playback" auto-label-width>
                            <a-row :gutter="24">
                                <a-col :span="isMobile ? 24 : 12">
                                    <a-form-item
                                        field="defaultProtocol"
                                        label="默认播放协议"
                                        tooltip="仅影响之后新建的实时播放和设备录像回放；当前正在播放的会话不切换。"
                                    >
                                        <a-select
                                            v-model="draft.playback.defaultProtocol"
                                            :loading="defaultPlaybackProtocolLoading || defaultPlaybackProtocolSaving"
                                            :disabled="!isEditing || defaultPlaybackProtocolLoading || defaultPlaybackProtocolSaving || !defaultPlaybackProtocolReady"
                                        >
                                            <a-option
                                                v-for="option in playbackProtocolOptions"
                                                :key="option.value"
                                                :value="option.value"
                                            >
                                                {{ option.label }}
                                            </a-option>
                                        </a-select>
                                    </a-form-item>
                                </a-col>
                                <a-col :span="isMobile ? 24 : 12">
                                    <a-form-item field="autoInvite" label="自动点播">
                                        <a-switch v-model="draft.playback.autoInvite" disabled />
                                    </a-form-item>
                                </a-col>
                                <a-col :span="isMobile ? 24 : 12">
                                    <a-form-item field="inviteTimeoutMs" label="点播超时时间（毫秒）">
                                        <a-input-number
                                            v-model="draft.playback.inviteTimeoutMs"
                                            class="service-config-number-input"
                                            :min="1000"
                                            :max="300000"
                                            disabled
                                        />
                                    </a-form-item>
                                </a-col>
                                <a-col :span="isMobile ? 24 : 12">
                                    <a-form-item field="recordPushStream" label="推流是否录制">
                                        <a-switch v-model="draft.playback.recordPushStream" disabled />
                                    </a-form-item>
                                </a-col>
                                <a-col :span="isMobile ? 24 : 12">
                                    <a-form-item field="cloudRecording" label="云端录像">
                                        <a-switch v-model="draft.playback.cloudRecording" disabled />
                                    </a-form-item>
                                </a-col>
                                <a-col :span="isMobile ? 24 : 12">
                                    <a-form-item field="stopWhenUnwatched" label="是否开启无人观看自动停止">
                                        <a-switch v-model="draft.playback.stopWhenUnwatched" disabled />
                                    </a-form-item>
                                </a-col>
                                <a-col :span="isMobile ? 24 : 12">
                                    <a-form-item field="pushAuth" label="推流鉴权">
                                        <a-switch v-model="draft.playback.pushAuth" disabled />
                                    </a-form-item>
                                </a-col>
                            </a-row>
                        </a-form>
                    </a-card>
                </a-tab-pane>

                <a-tab-pane key="cascade" title="国标级联相关">
                    <a-card :bordered="false" class="uvp-system-panel uvp-system-panel--dense mb-4">
                        <a-form class="uvp-system-form" :layout="formLayout" :model="draft.cascade" auto-label-width>
                            <a-row :gutter="24">
                                <a-col :span="isMobile ? 24 : 12">
                                    <a-form-item field="parentInviteTimeoutMs" label="上级平台点播超时时间">
                                        <a-input-number
                                            v-model="draft.cascade.parentInviteTimeoutMs"
                                            class="service-config-number-input"
                                            :min="1000"
                                            :max="600000"
                                            disabled
                                        />
                                    </a-form-item>
                                </a-col>
                                <a-col :span="isMobile ? 24 : 12">
                                    <a-form-item field="intercomStreamMode" label="国标级联对讲流模式">
                                        <a-select v-model="draft.cascade.intercomStreamMode" disabled>
                                            <a-option value="TCP被动">TCP被动</a-option>
                                        </a-select>
                                    </a-form-item>
                                </a-col>
                                <a-col :span="isMobile ? 24 : 12">
                                    <a-form-item field="sendStatusChangeMessages" label="设备/通道状态变化时发送消息">
                                        <a-switch v-model="draft.cascade.sendStatusChangeMessages" disabled />
                                    </a-form-item>
                                </a-col>
                                <a-col :span="isMobile ? 24 : 12">
                                    <a-form-item field="useCustomSsrc" label="是否使用自定义的ssrc">
                                        <a-switch v-model="draft.cascade.useCustomSsrc" disabled />
                                    </a-form-item>
                                </a-col>
                                <a-col :span="isMobile ? 24 : 12">
                                    <a-form-item field="offlineRetryIntervalSec" label="国标级联离线久重试间隔（秒）">
                                        <a-input-number
                                            v-model="draft.cascade.offlineRetryIntervalSec"
                                            class="service-config-number-input"
                                            :min="1"
                                            :max="3600"
                                            disabled
                                        />
                                    </a-form-item>
                                </a-col>
                                <a-col :span="isMobile ? 24 : 12">
                                    <a-form-item field="renewalMode" label="国标续订方式">
                                        <a-switch v-model="draft.cascade.renewalMode" disabled />
                                    </a-form-item>
                                </a-col>
                                <a-col :span="isMobile ? 24 : 12">
                                    <a-form-item field="usePushStatusAsChannelStatus" label="使用推流状态作为推流通道状态">
                                        <a-switch v-model="draft.cascade.usePushStatusAsChannelStatus" disabled />
                                    </a-form-item>
                                </a-col>
                            </a-row>
                        </a-form>
                    </a-card>
                </a-tab-pane>
        </a-tabs>
    </div>
</template>

<style lang="scss" scoped>
.service-config-page {
    overflow-y: auto;
}

.service-config-tabs {
    :deep(.arco-tabs-nav) {
        display: flex;
        align-items: flex-start;
        gap: 12px;
    }

    :deep(.arco-tabs-nav-tab) {
        flex: 0 0 auto;
    }

    :deep(.arco-tabs-nav-extra) {
        display: flex;
        align-items: center;
        padding-right: 0;
        margin-left: auto;
    }
}

.service-config-tabs__actions {
    display: flex;
    align-items: center;
    gap: 8px;
}

:deep(.service-config-number-input) {
    width: min(100%, 160px);
}

.ptz-default-speed {
    display: grid;
    grid-template-columns: auto minmax(88px, 1fr) auto 48px;
    gap: 8px;
    align-items: center;
    width: min(100%, 360px);
}

.ptz-default-speed__edge {
    color: var(--color-text-3);
    font-size: 12px;
    white-space: nowrap;
}

.ptz-default-speed__value {
    color: var(--color-text-1);
    font-variant-numeric: tabular-nums;
    text-align: right;
    white-space: nowrap;
}

:deep(.stream-transport-select) {
    width: 160px !important;
    max-width: 100%;
}

.global-subscription-items {
    display: grid;
    grid-template-columns: minmax(0, 1fr);
    gap: 4px;
    min-height: 32px;
    align-items: center;
    width: min(100%, 360px);

    :deep(.arco-checkbox) {
        min-width: 0;
        margin-right: 0;
        white-space: normal;
    }

    :deep(.arco-checkbox-label) {
        line-height: 20px;
    }
}

.mb-4 {
    margin-bottom: 16px;
}

@media (max-width: 640px) {
    :deep(.service-config-number-input),
    :deep(.stream-transport-select) {
        width: 100%;
    }

    .service-config-tabs {
        :deep(.arco-tabs-nav) {
            position: relative;
            padding-bottom: 48px;
        }

        :deep(.arco-tabs-nav-tab) {
            flex: 1 1 auto;
            max-width: 100%;
        }

        :deep(.arco-tabs-tab) {
            min-width: 0;
            padding-right: 8px;
            padding-left: 8px;
            margin: 0;
            font-size: 13px;
        }

        :deep(.arco-tabs-nav-extra) {
            position: absolute;
            right: 0;
            bottom: 0;
        }
    }
}
</style>
