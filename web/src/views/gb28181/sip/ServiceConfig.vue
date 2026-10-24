<script setup lang="ts">
import { Message } from "@arco-design/web-vue";
import { Check, RotateCcw } from "lucide-vue-next";
import { computed, nextTick, onMounted, reactive, ref, watch } from "vue";
import { useRoute } from "vue-router";
import { getDictItemsByDictCodeAPI } from "@/api/dictionary";
import {
  fetchServiceConfig,
  updateServiceConfig,
  type PlaybackProtocol,
  type FixedAddressPlaybackConfig,
  type PlayAuthConfig,
  type CascadeSettingsConfig,
  type PlaybackSettingsConfig,
  type DeviceAccessConfig
} from "@/api/gb28181";
import { useDevicesSize } from "@/hooks/useDevicesSize";
import SNumberField from "@/components/s-number-field/index.vue";
import { PLAYBACK_PROTOCOL_DICT_CODE, playbackProtocolOptionsFromDictionary } from "../playbackProtocol";
import {
  createCascadeSettingsDraft,
  createStaticServiceConfigDraft,
  normalizePlayAuthConfig,
  validateCascadeSettings
} from "./serviceConfigState";

const { isMobile } = useDevicesSize();
const activeTab = ref("gb");
const draft = reactive(createStaticServiceConfigDraft());
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
const playbackSettingsLoading = ref(true);
const playbackSettingsSaving = ref(false);
const playbackSettingsReady = ref(false);
const fixedAddressPlaybackLoading = ref(true);
const fixedAddressPlaybackSaving = ref(false);
const fixedAddressPlaybackReady = ref(false);
const playAuthLoading = ref(true);
const playAuthSaving = ref(false);
const playAuthReady = ref(false);
const playAuthRequiredByOpenAPI = ref(false);
const playAuthConfigConflict = ref(false);
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
const cloudRecordingRetentionLoading = ref(true);
const cloudRecordingRetentionSaving = ref(false);
const aggregateLoading = ref(true);
const aggregateReady = ref(false);
const aggregateSaving = ref(false);
const cloudRecordingRetentionReady = ref(false);
const savedPositionHistoryEnabled = ref(true);
const savedPositionHistoryRetentionDays = ref(7);
const savedSDPExtensionEnabled = ref(false);
const savedPTZDefaultSpeed = ref(6);
const savedDefaultChannelStreamTransport = ref<"UDP" | "TCP-Active" | "TCP-Passive">("TCP-Passive");
const savedDefaultPlaybackProtocol = ref<PlaybackProtocol>("ws-flv");
const savedPlaybackSettings = reactive<PlaybackSettingsConfig>({
  playTimeoutMs: 10000,
  onDemandLive: true,
  cloudRecordingEnabled: false
});
const savedFixedAddressPlayback = reactive<FixedAddressPlaybackConfig>({
  fixedAddressEnabled: false,
  autoOnDemandEnabled: false
});
const savedPlayAuth = reactive<PlayAuthConfig>({
  authEnabled: false,
  authBindClientIP: false,
  authTTLSeconds: 120
});
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
const savedCloudRecordingRetentionDays = ref(7);
const savedRecordCacheTailToleranceSec = ref(2);
// 设备接入地址（扫码配网 / 抓拍回传）：必须是**设备能访问到**的地址，
// 不是浏览器打开平台的地址。留空=未配置，是合法状态（后端有回退/拒绝策略）。
const savedQRProvisionBaseURL = ref("");
const savedSnapshotUploadBaseURL = ref("");
const savedCascadeSettings = reactive(createCascadeSettingsDraft());
const cascadeChanged = computed(() => JSON.stringify(draft.cascade) !== JSON.stringify(savedCascadeSettings));
const cascadeErrors = computed(() => validateCascadeSettings(draft.cascade));
const cascadeValid = computed(() => Object.keys(cascadeErrors.value).length === 0);
type NumberFieldInstance = InstanceType<typeof SNumberField>;
const positionHistoryRetentionField = ref<NumberFieldInstance | null>(null);
const cloudRecordingRetentionField = ref<NumberFieldInstance | null>(null);
const sipTimeoutField = ref<NumberFieldInstance | null>(null);
const playAuthTTLField = ref<NumberFieldInstance | null>(null);
const playTimeoutField = ref<NumberFieldInstance | null>(null);
const recordCacheTailToleranceField = ref<NumberFieldInstance | null>(null);
const numberFields = computed(() => [
  positionHistoryRetentionField,
  cloudRecordingRetentionField,
  sipTimeoutField,
  playAuthTTLField,
  playTimeoutField,
  recordCacheTailToleranceField
]);
const numberFieldsValid = computed(() => numberFields.value.every(field => !field.value?.error));
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
const defaultPlaybackProtocolChanged = computed(() => draft.playback.defaultProtocol !== savedDefaultPlaybackProtocol.value);
const playbackSettingsChanged = computed(
  () =>
    draft.playback.playTimeoutMs !== savedPlaybackSettings.playTimeoutMs ||
    draft.playback.onDemandLive !== savedPlaybackSettings.onDemandLive ||
    draft.playback.cloudRecordingEnabled !== savedPlaybackSettings.cloudRecordingEnabled
);
const fixedAddressPlaybackChanged = computed(
  () =>
    draft.playback.fixedAddressEnabled !== savedFixedAddressPlayback.fixedAddressEnabled ||
    draft.playback.autoOnDemandEnabled !== savedFixedAddressPlayback.autoOnDemandEnabled
);
const playAuthChanged = computed(
  () =>
    draft.playback.authEnabled !== savedPlayAuth.authEnabled ||
    draft.playback.authBindClientIP !== savedPlayAuth.authBindClientIP ||
    draft.playback.authTTLSeconds !== savedPlayAuth.authTTLSeconds
);
const globalSubscriptionChanged = computed(
  () => JSON.stringify(draft.globalSubscriptionItems) !== JSON.stringify(savedGlobalSubscriptionItems.value)
);
const defaultChannelAudioChanged = computed(() => draft.defaultChannelAudioEnabled !== savedDefaultChannelAudioEnabled.value);
const syncChannelsOnOnlineChanged = computed(() => draft.syncChannelsOnOnline !== savedSyncChannelsOnOnline.value);
const onlineOnHeartbeatChanged = computed(() => draft.onlineOnHeartbeat !== savedOnlineOnHeartbeat.value);
const saveAlarmMessagesChanged = computed(() => draft.saveAlarmMessages !== savedSaveAlarmMessages.value);
const sipCommandTimeoutChanged = computed(() => draft.sipTimeoutSec !== savedSIPCommandTimeoutSec.value);
const preallocationModeChanged = computed(() => draft.preallocationMode !== savedPreallocationMode.value);
const ignoreChannelOfflineStatusNotifyChanged = computed(
  () => draft.ignoreChannelOfflineStatusNotify !== savedIgnoreChannelOfflineStatusNotify.value
);
const sipLogChanged = computed(() => draft.sipLogEnabled !== savedSIPLogEnabled.value);
const cloudRecordingRetentionChanged = computed(
  () => draft.cloudRecordingRetentionDays !== savedCloudRecordingRetentionDays.value
);
const recordCacheTailToleranceChanged = computed(
  () => draft.recordCacheTailToleranceSec !== savedRecordCacheTailToleranceSec.value
);

// 设备接入地址：只做「明显的格式错」这一层拦截，真正的闸门在后端（复用
// EffectiveBaseURL 的同一套规则）。前端多拦只会出现「页面过了后端拒」的两套口径。
function isDeviceAccessURLInputValid(value: string): boolean {
  const trimmed = value.trim();
  if (!trimmed) return true;
  if (/\s/.test(trimmed)) return false;
  return /^https?:\/\/[^/?#]+(?:[/?#]\S*)?$/i.test(trimmed);
}
const qrProvisionBaseURLValid = computed(() => isDeviceAccessURLInputValid(draft.qrProvisionBaseUrl));
const snapshotUploadBaseURLValid = computed(() => isDeviceAccessURLInputValid(draft.snapshotUploadBaseUrl));
const deviceAccessChanged = computed(
  () =>
    draft.qrProvisionBaseUrl.trim() !== savedQRProvisionBaseURL.value ||
    draft.snapshotUploadBaseUrl.trim() !== savedSnapshotUploadBaseURL.value
);
const cloudRecordingRetentionValid = computed(
  () =>
    Number.isInteger(draft.cloudRecordingRetentionDays) &&
    draft.cloudRecordingRetentionDays >= 1 &&
    draft.cloudRecordingRetentionDays <= 365
);
const recordCacheTailToleranceValid = computed(
  () =>
    Number.isInteger(draft.recordCacheTailToleranceSec) &&
    draft.recordCacheTailToleranceSec >= 0 &&
    draft.recordCacheTailToleranceSec <= 30
);
const playTimeoutValid = computed(
  () =>
    Number.isInteger(draft.playback.playTimeoutMs) &&
    draft.playback.playTimeoutMs >= 1000 &&
    draft.playback.playTimeoutMs <= 300000
);
const playAuthTTLValid = computed(
  () =>
    Number.isInteger(draft.playback.authTTLSeconds) &&
    draft.playback.authTTLSeconds >= 60 &&
    draft.playback.authTTLSeconds <= 3600
);
const hasChanges = computed(
  () =>
    positionHistoryChanged.value ||
    sdpExtensionChanged.value ||
    ptzDefaultSpeedChanged.value ||
    defaultChannelStreamTransportChanged.value ||
    defaultPlaybackProtocolChanged.value ||
    fixedAddressPlaybackChanged.value ||
    playAuthChanged.value ||
    playbackSettingsChanged.value ||
    globalSubscriptionChanged.value ||
    defaultChannelAudioChanged.value ||
    syncChannelsOnOnlineChanged.value ||
    onlineOnHeartbeatChanged.value ||
    saveAlarmMessagesChanged.value ||
    sipCommandTimeoutChanged.value ||
    preallocationModeChanged.value ||
    ignoreChannelOfflineStatusNotifyChanged.value ||
    sipLogChanged.value ||
    cloudRecordingRetentionChanged.value ||
    recordCacheTailToleranceChanged.value ||
    deviceAccessChanged.value ||
    cascadeChanged.value
);
const configSaving = computed(() => aggregateSaving.value);
const configReady = computed(() => aggregateReady.value && !aggregateLoading.value);
const formLayout = computed(() => (isMobile.value ? "vertical" : "horizontal"));
const cascadeFields = [
  { key: "userAgent", label: "User-Agent", tooltip: "级联 SIP 报文使用的平台标识，所有上级平台共用。", max: 128, required: true },
  { key: "platformName", label: "平台名称", tooltip: "向上级平台发布的平台名称。", max: 128, required: true },
  { key: "manufacturer", label: "厂商", tooltip: "平台设备信息和平台目录中的厂商名称。", max: 128, required: true },
  { key: "model", label: "型号", tooltip: "平台设备信息和平台目录中的型号。", max: 128, required: true },
  { key: "civilCode", label: "行政区划", tooltip: "平台所属行政区划，可为空或填写 2、4、6、8 位数字。", max: 8, required: false },
  { key: "address", label: "安装地址", tooltip: "平台目录中的安装地址。", max: 256, required: false }
] as const;

function applyPlaybackSettings(settings: PlaybackSettingsConfig) {
  draft.playback.playTimeoutMs = settings.playTimeoutMs;
  draft.playback.onDemandLive = settings.onDemandLive;
  draft.playback.cloudRecordingEnabled = settings.cloudRecordingEnabled;
  Object.assign(savedPlaybackSettings, settings);
}

// 回填并同步 saved 快照：老后端（未升级）返回里没有 deviceAccess，一律按"未配置"处理，
// 不能因为字段缺失就把这两项当成脏数据（否则页面一进来就显示"有未保存修改"）。
function applyDeviceAccessConfig(config?: DeviceAccessConfig) {
  const qr = config?.qrProvisionBaseUrl ?? "";
  const snapshot = config?.snapshotUploadBaseUrl ?? "";
  draft.qrProvisionBaseUrl = qr;
  draft.snapshotUploadBaseUrl = snapshot;
  savedQRProvisionBaseURL.value = qr;
  savedSnapshotUploadBaseURL.value = snapshot;
}

function applyCascadeSettings(settings?: CascadeSettingsConfig) {
  const values = settings ?? createCascadeSettingsDraft();
  Object.assign(draft.cascade, values);
  Object.assign(savedCascadeSettings, values);
}

function restorePlaybackSettingsDraft() {
  draft.playback.playTimeoutMs = savedPlaybackSettings.playTimeoutMs;
  draft.playback.onDemandLive = savedPlaybackSettings.onDemandLive;
  draft.playback.cloudRecordingEnabled = savedPlaybackSettings.cloudRecordingEnabled;
}

function applyFixedAddressPlaybackConfig(config: FixedAddressPlaybackConfig) {
  const normalized = {
    fixedAddressEnabled: config.fixedAddressEnabled,
    autoOnDemandEnabled: config.fixedAddressEnabled && config.autoOnDemandEnabled
  };
  draft.playback.fixedAddressEnabled = normalized.fixedAddressEnabled;
  draft.playback.autoOnDemandEnabled = normalized.autoOnDemandEnabled;
  Object.assign(savedFixedAddressPlayback, normalized);
}

function restoreFixedAddressPlaybackDraft() {
  draft.playback.fixedAddressEnabled = savedFixedAddressPlayback.fixedAddressEnabled;
  draft.playback.autoOnDemandEnabled = savedFixedAddressPlayback.fixedAddressEnabled
    ? savedFixedAddressPlayback.autoOnDemandEnabled
    : false;
}

function applyPlayAuthConfig(config: PlayAuthConfig) {
  playAuthRequiredByOpenAPI.value = config.authRequiredByOpenAPI === true;
  playAuthConfigConflict.value = config.authConfigConflict === true;
  const normalized = normalizePlayAuthConfig(config);
  draft.playback.authEnabled = normalized.authEnabled;
  draft.playback.authBindClientIP = normalized.authBindClientIP;
  draft.playback.authTTLSeconds = normalized.authTTLSeconds;
  Object.assign(savedPlayAuth, normalized);
}

function restorePlayAuthDraft() {
  const normalized = normalizePlayAuthConfig(savedPlayAuth);
  draft.playback.authEnabled = normalized.authEnabled;
  draft.playback.authBindClientIP = normalized.authBindClientIP;
  draft.playback.authTTLSeconds = normalized.authTTLSeconds;
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

function resetDraft() {
  Object.assign(draft.cascade, savedCascadeSettings);
  draft.saveMobilePositionHistory = savedPositionHistoryEnabled.value;
  draft.positionHistoryRetentionDays = savedPositionHistoryRetentionDays.value;
  draft.recordCacheTailToleranceSec = savedRecordCacheTailToleranceSec.value;
  draft.qrProvisionBaseUrl = savedQRProvisionBaseURL.value;
  draft.snapshotUploadBaseUrl = savedSnapshotUploadBaseURL.value;
  draft.sdpExtension = savedSDPExtensionEnabled.value;
  draft.ptzSpeed = savedPTZDefaultSpeed.value;
  draft.defaultChannelStreamTransport = savedDefaultChannelStreamTransport.value;
  draft.playback.defaultProtocol = savedDefaultPlaybackProtocol.value;
  restoreFixedAddressPlaybackDraft();
  restorePlayAuthDraft();
  restorePlaybackSettingsDraft();
  draft.globalSubscriptionItems = [...savedGlobalSubscriptionItems.value];
  draft.defaultChannelAudioEnabled = savedDefaultChannelAudioEnabled.value;
  draft.syncChannelsOnOnline = savedSyncChannelsOnOnline.value;
  draft.onlineOnHeartbeat = savedOnlineOnHeartbeat.value;
  draft.saveAlarmMessages = savedSaveAlarmMessages.value;
  draft.sipTimeoutSec = savedSIPCommandTimeoutSec.value;
  draft.preallocationMode = savedPreallocationMode.value;
  draft.ignoreChannelOfflineStatusNotify = savedIgnoreChannelOfflineStatusNotify.value;
  draft.sipLogEnabled = savedSIPLogEnabled.value;
}

watch(
  () => draft.playback.fixedAddressEnabled,
  enabled => {
    if (!enabled) draft.playback.autoOnDemandEnabled = false;
  }
);

function handlePlayAuthEnabledChange(enabled: boolean) {
  if (!enabled && playAuthRequiredByOpenAPI.value) return;
  const normalized = normalizePlayAuthConfig({
    authEnabled: enabled,
    authBindClientIP: draft.playback.authBindClientIP,
    authTTLSeconds: draft.playback.authTTLSeconds
  });
  draft.playback.authEnabled = normalized.authEnabled;
  draft.playback.authBindClientIP = normalized.authBindClientIP;
  draft.playback.authTTLSeconds = normalized.authTTLSeconds;
}

async function saveConfig() {
  if (!configReady.value) return;
  if (!cascadeValid.value) return;
  const numberFieldError = numberFields.value.map(field => field.value?.error || "").find(Boolean);
  if (numberFieldError) {
    Message.warning(numberFieldError);
    return;
  }
  if (
    !cloudRecordingRetentionValid.value ||
    !recordCacheTailToleranceValid.value ||
    !playTimeoutValid.value ||
    !playAuthTTLValid.value
  )
    return;
  if (!hasChanges.value) {
    return;
  }
  if (!qrProvisionBaseURLValid.value || !snapshotUploadBaseURLValid.value) {
    Message.warning("设备接入地址需要以 http:// 或 https:// 开头，且不能包含空格");
    return;
  }
  aggregateSaving.value = true;
  try {
    const response = await updateServiceConfig({
      positionHistory: { enabled: draft.saveMobilePositionHistory, retentionDays: draft.positionHistoryRetentionDays },
      cloudRecordingRetention: { retentionDays: draft.cloudRecordingRetentionDays },
      recordCacheTailToleranceSec: draft.recordCacheTailToleranceSec,
      sdpExtension: { enabled: draft.sdpExtension },
      syncChannelsOnOnline: { enabled: draft.syncChannelsOnOnline },
      onlineOnHeartbeat: { enabled: draft.onlineOnHeartbeat },
      saveAlarmMessages: { enabled: draft.saveAlarmMessages },
      sipCommandTimeout: { timeoutSec: draft.sipTimeoutSec },
      preallocationMode: { enabled: draft.preallocationMode },
      ignoreChannelOfflineStatusNotify: { enabled: draft.ignoreChannelOfflineStatusNotify },
      ptzDefaultSpeed: { level: draft.ptzSpeed },
      defaultChannelStreamTransport: { transport: draft.defaultChannelStreamTransport },
      defaultPlaybackProtocol: { protocol: draft.playback.defaultProtocol },
      globalSubscriptions: { items: draft.globalSubscriptionItems },
      defaultChannelAudio: { enabled: draft.defaultChannelAudioEnabled },
      playbackSettings: {
        playTimeoutMs: draft.playback.playTimeoutMs,
        onDemandLive: draft.playback.onDemandLive,
        cloudRecordingEnabled: draft.playback.cloudRecordingEnabled
      },
      fixedAddressPlayback: {
        fixedAddressEnabled: draft.playback.fixedAddressEnabled,
        autoOnDemandEnabled: draft.playback.autoOnDemandEnabled
      },
      playAuth: {
        authEnabled: draft.playback.authEnabled,
        authBindClientIP: draft.playback.authBindClientIP,
        authTTLSeconds: draft.playback.authTTLSeconds
      },
      sipLog: {
        enabled: draft.sipLogEnabled,
        applied: sipLogApplied.value
      },
      deviceAccess: {
        qrProvisionBaseUrl: draft.qrProvisionBaseUrl.trim(),
        snapshotUploadBaseUrl: draft.snapshotUploadBaseUrl.trim()
      },
      cascade: { ...draft.cascade }
    });
    if (response.code !== 0) throw new Error(response.message || "保存配置失败");
    const v = response.data;
    applyCascadeSettings(v.cascade);
    draft.saveMobilePositionHistory = v.positionHistory.enabled;
    draft.positionHistoryRetentionDays = v.positionHistory.retentionDays ?? 7;
    draft.cloudRecordingRetentionDays = v.cloudRecordingRetention.retentionDays ?? 7;
    draft.recordCacheTailToleranceSec = v.recordCacheTailToleranceSec ?? 2;
    draft.sdpExtension = v.sdpExtension.enabled;
    draft.syncChannelsOnOnline = v.syncChannelsOnOnline.enabled;
    draft.onlineOnHeartbeat = v.onlineOnHeartbeat.enabled;
    draft.saveAlarmMessages = v.saveAlarmMessages.enabled;
    draft.sipTimeoutSec = v.sipCommandTimeout.timeoutSec;
    draft.preallocationMode = v.preallocationMode.enabled;
    draft.ignoreChannelOfflineStatusNotify = v.ignoreChannelOfflineStatusNotify.enabled;
    draft.ptzSpeed = v.ptzDefaultSpeed.level;
    draft.defaultChannelStreamTransport = v.defaultChannelStreamTransport.transport;
    draft.playback.defaultProtocol = v.defaultPlaybackProtocol.protocol;
    draft.globalSubscriptionItems = [...v.globalSubscriptions.items];
    draft.defaultChannelAudioEnabled = v.defaultChannelAudio.enabled;
    applyPlaybackSettings(v.playbackSettings);
    applyFixedAddressPlaybackConfig(v.fixedAddressPlayback);
    applyPlayAuthConfig(v.playAuth);
    draft.sipLogEnabled = v.sipLog.enabled;
    sipLogApplied.value = v.sipLog.applied ?? true;
    applyDeviceAccessConfig(v.deviceAccess);
    savedPositionHistoryEnabled.value = v.positionHistory.enabled;
    savedPositionHistoryRetentionDays.value = v.positionHistory.retentionDays ?? 7;
    savedCloudRecordingRetentionDays.value = v.cloudRecordingRetention.retentionDays ?? 7;
    savedRecordCacheTailToleranceSec.value = v.recordCacheTailToleranceSec ?? 2;
    savedSDPExtensionEnabled.value = v.sdpExtension.enabled;
    savedPTZDefaultSpeed.value = v.ptzDefaultSpeed.level;
    savedDefaultChannelStreamTransport.value = v.defaultChannelStreamTransport.transport;
    savedDefaultPlaybackProtocol.value = v.defaultPlaybackProtocol.protocol;
    savedGlobalSubscriptionItems.value = [...v.globalSubscriptions.items];
    savedDefaultChannelAudioEnabled.value = v.defaultChannelAudio.enabled;
    savedSyncChannelsOnOnline.value = v.syncChannelsOnOnline.enabled;
    savedOnlineOnHeartbeat.value = v.onlineOnHeartbeat.enabled;
    savedSaveAlarmMessages.value = v.saveAlarmMessages.enabled;
    savedSIPCommandTimeoutSec.value = v.sipCommandTimeout.timeoutSec;
    savedPreallocationMode.value = v.preallocationMode.enabled;
    savedIgnoreChannelOfflineStatusNotify.value = v.ignoreChannelOfflineStatusNotify.enabled;
    savedSIPLogEnabled.value = v.sipLog.enabled;
    Message.success("国标服务配置已更新");
    return;
  } catch (error: any) {
    restoreFixedAddressPlaybackDraft();
    restorePlayAuthDraft();
    restorePlaybackSettingsDraft();
    Message.error(error?.message || "保存国标服务配置失败");
  } finally {
    aggregateSaving.value = false;
    positionHistorySaving.value = false;
    sdpExtensionSaving.value = false;
    ptzDefaultSpeedSaving.value = false;
    defaultChannelStreamTransportSaving.value = false;
    defaultPlaybackProtocolSaving.value = false;
    fixedAddressPlaybackSaving.value = false;
    playAuthSaving.value = false;
    playbackSettingsSaving.value = false;
    globalSubscriptionSaving.value = false;
    defaultChannelAudioSaving.value = false;
    syncChannelsOnOnlineSaving.value = false;
    onlineOnHeartbeatSaving.value = false;
    saveAlarmMessagesSaving.value = false;
    sipCommandTimeoutSaving.value = false;
    preallocationModeSaving.value = false;
    ignoreChannelOfflineStatusNotifySaving.value = false;
    sipLogSaving.value = false;
    cloudRecordingRetentionSaving.value = false;
  }
}

async function loadServiceConfig() {
  aggregateLoading.value = true;
  try {
    const response = await fetchServiceConfig();
    if (response.code !== 0) throw new Error(response.message || "加载配置失败");
    const v = response.data;
    applyCascadeSettings(v.cascade);
    draft.saveMobilePositionHistory = v.positionHistory.enabled;
    draft.positionHistoryRetentionDays = v.positionHistory.retentionDays ?? 7;
    savedPositionHistoryEnabled.value = v.positionHistory.enabled;
    savedPositionHistoryRetentionDays.value = v.positionHistory.retentionDays ?? 7;
    draft.cloudRecordingRetentionDays = v.cloudRecordingRetention.retentionDays ?? 7;
    draft.recordCacheTailToleranceSec = v.recordCacheTailToleranceSec ?? 2;
    savedCloudRecordingRetentionDays.value = v.cloudRecordingRetention.retentionDays ?? 7;
    savedRecordCacheTailToleranceSec.value = v.recordCacheTailToleranceSec ?? 2;
    draft.sdpExtension = v.sdpExtension.enabled;
    savedSDPExtensionEnabled.value = v.sdpExtension.enabled;
    draft.ptzSpeed = v.ptzDefaultSpeed.level;
    savedPTZDefaultSpeed.value = v.ptzDefaultSpeed.level;
    draft.defaultChannelStreamTransport = v.defaultChannelStreamTransport.transport;
    savedDefaultChannelStreamTransport.value = v.defaultChannelStreamTransport.transport;
    draft.playback.defaultProtocol = v.defaultPlaybackProtocol.protocol;
    savedDefaultPlaybackProtocol.value = v.defaultPlaybackProtocol.protocol;
    draft.globalSubscriptionItems = [...v.globalSubscriptions.items];
    savedGlobalSubscriptionItems.value = [...v.globalSubscriptions.items];
    draft.defaultChannelAudioEnabled = v.defaultChannelAudio.enabled;
    savedDefaultChannelAudioEnabled.value = v.defaultChannelAudio.enabled;
    draft.syncChannelsOnOnline = v.syncChannelsOnOnline.enabled;
    savedSyncChannelsOnOnline.value = v.syncChannelsOnOnline.enabled;
    draft.onlineOnHeartbeat = v.onlineOnHeartbeat.enabled;
    savedOnlineOnHeartbeat.value = v.onlineOnHeartbeat.enabled;
    draft.saveAlarmMessages = v.saveAlarmMessages.enabled;
    savedSaveAlarmMessages.value = v.saveAlarmMessages.enabled;
    draft.sipTimeoutSec = v.sipCommandTimeout.timeoutSec;
    savedSIPCommandTimeoutSec.value = v.sipCommandTimeout.timeoutSec;
    draft.preallocationMode = v.preallocationMode.enabled;
    savedPreallocationMode.value = v.preallocationMode.enabled;
    draft.ignoreChannelOfflineStatusNotify = v.ignoreChannelOfflineStatusNotify.enabled;
    savedIgnoreChannelOfflineStatusNotify.value = v.ignoreChannelOfflineStatusNotify.enabled;
    draft.sipLogEnabled = v.sipLog.enabled;
    sipLogApplied.value = v.sipLog.applied ?? true;
    applyDeviceAccessConfig(v.deviceAccess);
    savedSIPLogEnabled.value = v.sipLog.enabled;
    applyPlaybackSettings(v.playbackSettings);
    applyFixedAddressPlaybackConfig(v.fixedAddressPlayback);
    applyPlayAuthConfig(v.playAuth);
    positionHistoryReady.value = true;
    sdpExtensionReady.value = true;
    ptzDefaultSpeedReady.value = true;
    defaultChannelStreamTransportReady.value = true;
    defaultPlaybackProtocolReady.value = true;
    playbackSettingsReady.value = true;
    fixedAddressPlaybackReady.value = true;
    playAuthReady.value = true;
    globalSubscriptionReady.value = true;
    defaultChannelAudioReady.value = true;
    syncChannelsOnOnlineReady.value = true;
    onlineOnHeartbeatReady.value = true;
    saveAlarmMessagesReady.value = true;
    sipCommandTimeoutReady.value = true;
    preallocationModeReady.value = true;
    ignoreChannelOfflineStatusNotifyReady.value = true;
    sipLogReady.value = true;
    cloudRecordingRetentionReady.value = true;
    aggregateReady.value = true;
    await loadPlaybackProtocolOptions();
  } catch (error: any) {
    Message.error(error?.message || "加载国标服务配置失败");
  } finally {
    aggregateLoading.value = false;
    positionHistoryLoading.value = false;
    sdpExtensionLoading.value = false;
    ptzDefaultSpeedLoading.value = false;
    defaultChannelStreamTransportLoading.value = false;
    defaultPlaybackProtocolLoading.value = false;
    playbackSettingsLoading.value = false;
    fixedAddressPlaybackLoading.value = false;
    playAuthLoading.value = false;
    globalSubscriptionLoading.value = false;
    defaultChannelAudioLoading.value = false;
    syncChannelsOnOnlineLoading.value = false;
    onlineOnHeartbeatLoading.value = false;
    saveAlarmMessagesLoading.value = false;
    sipCommandTimeoutLoading.value = false;
    preallocationModeLoading.value = false;
    ignoreChannelOfflineStatusNotifyLoading.value = false;
    sipLogLoading.value = false;
    cloudRecordingRetentionLoading.value = false;
  }
}

// 从 SIP 日志页跳转过来时高亮那个开关（老板 2026-10-08 定的引导链路）。
//
// ⛔ 判据只认 `?focus=` 的**具体键名**，不做模糊匹配 ——
//   否则 "?focus=sip" 之类会误命中一堆开关，用户反而找不到要看的地方。
// ⛔⛔ `useRoute()` 在没有路由上下文的场合（单测里直接 mount 本组件）会返回 undefined，
//   直接读 `route.query` 会把整个组件渲染打断（`Cannot read properties of undefined`），
//   ⭐ 而且症状是「39 条用例一起红」，看不出是哪一行引起的。
//   ⇒ 这里判空后退化成「不高亮」，功能降级但页面照常。
const route = useRoute();
const focusedField = computed(() => {
  const raw = route?.query?.focus;
  return typeof raw === "string" ? raw : "";
});
const isSipLogFocused = computed(() => focusedField.value === "sipLogEnabled");

onMounted(async () => {
  await loadServiceConfig();
  // 数据到位后再高亮：字段是异步渲染的，早一步 scrollIntoView 找不到节点。
  if (isSipLogFocused.value) {
    await nextTick();
    document.querySelector('[data-field="sipLogEnabled"]')?.scrollIntoView({ block: "center" });
  }
});
</script>

<template>
  <div class="snow-fill service-config-page">
    <a-tabs
      v-model:active-key="activeTab"
      class="uvp-system-tabs uvp-config-tabs service-config-tabs"
      :animation="true"
      lazy-load
    >
      <template #extra>
        <div class="service-config-tabs__actions">
          <span v-if="hasChanges" class="service-config-unsaved">
            <i class="service-config-unsaved__dot" />
            未保存
          </span>
          <a-button class="uvp-page-action-btn" :disabled="configSaving || !hasChanges" @click="resetDraft">
            <template #icon><RotateCcw :size="15" /></template>
            重置
          </a-button>
          <a-button
            class="uvp-page-action-btn"
            type="primary"
            :loading="configSaving"
            :disabled="
              configSaving ||
              !configReady ||
              !hasChanges ||
              !cascadeValid ||
              !numberFieldsValid ||
              !recordCacheTailToleranceValid ||
              !playTimeoutValid ||
              !playAuthTTLValid
            "
            @click="saveConfig"
          >
            <template #icon><Check :size="15" /></template>
            保存配置
          </a-button>
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
                    :disabled="positionHistoryLoading || positionHistorySaving || !positionHistoryReady"
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
                  <s-number-field
                    ref="positionHistoryRetentionField"
                    v-model="draft.positionHistoryRetentionDays"
                    class="service-config-number-input"
                    :min="1"
                    :max="365"
                    required
                    :disabled="
                      positionHistoryLoading || positionHistorySaving || !positionHistoryReady || !draft.saveMobilePositionHistory
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
                    :disabled="sdpExtensionLoading || sdpExtensionSaving || !sdpExtensionReady"
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
                      :disabled="ptzDefaultSpeedLoading || ptzDefaultSpeedSaving || !ptzDefaultSpeedReady"
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
                    :disabled="defaultChannelAudioLoading || defaultChannelAudioSaving || !defaultChannelAudioReady"
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
                    :disabled="syncChannelsOnOnlineLoading || syncChannelsOnOnlineSaving || !syncChannelsOnOnlineReady"
                  >
                    <template #checked>开启</template>
                    <template #unchecked>关闭</template>
                  </a-switch>
                </a-form-item>
              </a-col>
              <a-col :span="isMobile ? 24 : 12">
                <a-form-item
                  field="sipLogEnabled"
                  data-field="sipLogEnabled"
                  :class="{ 'sip-log-focused': isSipLogFocused }"
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
                    :disabled="sipLogLoading || sipLogSaving || !sipLogReady"
                  >
                    <template #checked>开启</template>
                    <template #unchecked>关闭</template>
                  </a-switch>
                </a-form-item>
              </a-col>
              <a-col :span="isMobile ? 24 : 12">
                <a-form-item
                  field="cloudRecordingRetentionDays"
                  label="云端录像默认保留天数（天）"
                  tooltip="云端录像与设备录像下载产生的缓存文件按录制时间自动清理，默认保留 7 天。"
                  :validate-status="cloudRecordingRetentionValid ? undefined : 'error'"
                >
                  <s-number-field
                    ref="cloudRecordingRetentionField"
                    v-model="draft.cloudRecordingRetentionDays"
                    class="service-config-number-input"
                    :min="1"
                    :max="365"
                    required
                    :disabled="cloudRecordingRetentionLoading || cloudRecordingRetentionSaving || !cloudRecordingRetentionReady"
                  />
                  <template v-if="!cloudRecordingRetentionValid" #extra>
                    <span>请输入 1-365 之间的整数</span>
                  </template>
                </a-form-item>
              </a-col>
              <a-col :span="isMobile ? 24 : 12">
                <a-form-item
                  field="recordCacheTailToleranceSec"
                  label="录像缓存尾部容差（秒）"
                  tooltip="设备拒绝最后一小段回放请求时，只要已有录像文件且剩余时间不超过该值，就将任务按成功收尾；默认 2 秒，设置为 0 可关闭。"
                  :validate-status="recordCacheTailToleranceValid ? undefined : 'error'"
                >
                  <s-number-field
                    ref="recordCacheTailToleranceField"
                    v-model="draft.recordCacheTailToleranceSec"
                    class="service-config-number-input"
                    :min="0"
                    :max="30"
                    required
                    :disabled="aggregateLoading || aggregateSaving || !aggregateReady"
                  />
                  <template v-if="!recordCacheTailToleranceValid" #extra>
                    <span>请输入 0-30 之间的整数</span>
                  </template>
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
                    :disabled="onlineOnHeartbeatLoading || onlineOnHeartbeatSaving || !onlineOnHeartbeatReady"
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
                    :disabled="saveAlarmMessagesLoading || saveAlarmMessagesSaving || !saveAlarmMessagesReady"
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
                  <s-number-field
                    ref="sipTimeoutField"
                    v-model="draft.sipTimeoutSec"
                    class="service-config-number-input"
                    :min="1"
                    :max="300"
                    required
                    :disabled="sipCommandTimeoutLoading || sipCommandTimeoutSaving || !sipCommandTimeoutReady"
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
                    :disabled="preallocationModeLoading || preallocationModeSaving || !preallocationModeReady"
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
                    :disabled="globalSubscriptionLoading || globalSubscriptionSaving || !globalSubscriptionReady"
                  >
                    <a-checkbox value="catalog">目录</a-checkbox>
                    <a-checkbox value="alarm">报警</a-checkbox>
                    <a-checkbox value="mobile_position">位置</a-checkbox>
                    <a-checkbox value="ptz_precise_position">PTZ 精准位置变化（2022）</a-checkbox>
                  </a-checkbox-group>
                </a-form-item>
              </a-col>
              <a-col :span="24">
                <div class="service-config-subtitle">设备接入地址</div>
              </a-col>
              <a-col :span="isMobile ? 24 : 12">
                <a-form-item
                  field="qrProvisionBaseUrl"
                  label="扫码配网地址"
                  tooltip="二维码里交给设备的平台地址，必须是设备能访问到的地址（内网填本机 IP，公网部署填公网地址）。留空表示未配置，出码会被拒绝并提示先配置此项。"
                >
                  <a-input
                    v-model="draft.qrProvisionBaseUrl"
                    placeholder="http://192.168.1.10:51010"
                    allow-clear
                    :error="!qrProvisionBaseURLValid"
                    :disabled="!configReady"
                  />
                </a-form-item>
              </a-col>
              <a-col :span="isMobile ? 24 : 12">
                <a-form-item
                  field="snapshotUploadBaseUrl"
                  label="抓拍回传地址"
                  tooltip="2022 抓拍时下发给设备的上传地址。设备不认自签证书，HTTPS 自签部署请改用明文口（如 http://IP:51001）。留空表示未配置，将按操作员访问平台时的地址自动推导，仅设备与平台直连时可靠。"
                >
                  <a-input
                    v-model="draft.snapshotUploadBaseUrl"
                    placeholder="http://192.168.1.10:51001"
                    allow-clear
                    :error="!snapshotUploadBaseURLValid"
                    :disabled="!configReady"
                  />
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
                    class="playback-protocol-select"
                    :loading="defaultPlaybackProtocolLoading || defaultPlaybackProtocolSaving"
                    :disabled="defaultPlaybackProtocolLoading || defaultPlaybackProtocolSaving || !defaultPlaybackProtocolReady"
                  >
                    <a-option v-for="option in playbackProtocolOptions" :key="option.value" :value="option.value">
                      {{ option.label }}
                    </a-option>
                  </a-select>
                </a-form-item>
              </a-col>
              <a-col :span="isMobile ? 24 : 12">
                <a-form-item
                  field="fixedAddressEnabled"
                  label="固定播放地址"
                  tooltip="开启后，新建实时播放使用稳定的固定播放地址；关闭时沿用动态播放地址。"
                >
                  <a-switch
                    v-model="draft.playback.fixedAddressEnabled"
                    :loading="fixedAddressPlaybackLoading || fixedAddressPlaybackSaving"
                    :disabled="fixedAddressPlaybackLoading || fixedAddressPlaybackSaving || !fixedAddressPlaybackReady"
                  />
                </a-form-item>
              </a-col>
              <a-col :span="isMobile ? 24 : 12">
                <a-form-item
                  field="autoOnDemandEnabled"
                  label="自动点播"
                  tooltip="仅在固定播放地址开启时生效；访问缺少媒体流的实时地址时自动发起点播。"
                >
                  <a-switch
                    v-model="draft.playback.autoOnDemandEnabled"
                    :loading="fixedAddressPlaybackLoading || fixedAddressPlaybackSaving"
                    :disabled="
                      !draft.playback.fixedAddressEnabled ||
                      fixedAddressPlaybackLoading ||
                      fixedAddressPlaybackSaving ||
                      !fixedAddressPlaybackReady
                    "
                  />
                </a-form-item>
              </a-col>
              <a-col :span="isMobile ? 24 : 12">
                <a-form-item
                  field="authEnabled"
                  label="播放鉴权"
                  tooltip="开启后播放地址携带按配置有效期签发的短时凭证，仅限制新连接和重连，不会主动中断已建立播放；关闭后媒体裸地址泄露即可被直接使用。"
                >
                  <a-switch
                    :model-value="draft.playback.authEnabled"
                    :loading="playAuthLoading || playAuthSaving"
                    :disabled="playAuthLoading || playAuthSaving || !playAuthReady || playAuthRequiredByOpenAPI"
                    @update:model-value="handlePlayAuthEnabledChange"
                  />
                  <template v-if="playAuthRequiredByOpenAPI" #extra>
                    <span>OpenAPI 播放隔离要求持续鉴权，停用 OpenAPI 不会解除此保护；历史裸播放地址不再放行。</span>
                    <span v-if="playAuthConfigConflict">配置文件请求关闭鉴权，当前仍强制开启；请核对配置文件。</span>
                  </template>
                </a-form-item>
              </a-col>
              <a-col :span="isMobile ? 24 : 12">
                <a-form-item
                  field="authBindClientIP"
                  label="绑定客户端 IP"
                  tooltip="仅在播放鉴权开启时生效。NAT、VPN、移动网络切换、IPv6 临时地址或代理拓扑变化可能导致合法重连被拒绝。"
                >
                  <a-switch
                    v-model="draft.playback.authBindClientIP"
                    :loading="playAuthLoading || playAuthSaving"
                    :disabled="!draft.playback.authEnabled || playAuthLoading || playAuthSaving || !playAuthReady"
                  />
                </a-form-item>
              </a-col>
              <a-col :span="isMobile ? 24 : 12">
                <a-form-item
                  field="authTTLSeconds"
                  label="凭证有效期（秒）"
                  tooltip="仅影响新签发的播放凭证；已签发凭证保持原到期时间。默认 120 秒。"
                  :validate-status="playAuthTTLValid ? undefined : 'error'"
                >
                  <s-number-field
                    ref="playAuthTTLField"
                    v-model="draft.playback.authTTLSeconds"
                    class="service-config-number-input"
                    :min="60"
                    :max="3600"
                    required
                    :disabled="playAuthLoading || playAuthSaving || !playAuthReady"
                  />
                  <template v-if="!playAuthTTLValid" #extra>
                    <span>请输入 60-3600 之间的整数</span>
                  </template>
                </a-form-item>
              </a-col>
              <a-col :span="isMobile ? 24 : 12">
                <a-form-item
                  field="playTimeoutMs"
                  label="点播超时时间（毫秒）"
                  tooltip="控制实时点播从发送 INVITE 到媒体流就绪的总等待时间。"
                  :validate-status="playTimeoutValid ? undefined : 'error'"
                >
                  <s-number-field
                    ref="playTimeoutField"
                    v-model="draft.playback.playTimeoutMs"
                    class="service-config-number-input"
                    :min="1000"
                    :max="300000"
                    required
                    :disabled="playbackSettingsLoading || playbackSettingsSaving || !playbackSettingsReady"
                  />
                </a-form-item>
              </a-col>
              <a-col :span="isMobile ? 24 : 12">
                <a-form-item
                  field="onDemandLive"
                  label="按需直播"
                  tooltip="这是按需直播的新通道默认值；仅应用于通过 Catalog 新发现的通道，已有通道保持不变。"
                >
                  <a-switch
                    v-model="draft.playback.onDemandLive"
                    :loading="playbackSettingsLoading || playbackSettingsSaving"
                    :disabled="playbackSettingsLoading || playbackSettingsSaving || !playbackSettingsReady"
                  />
                </a-form-item>
              </a-col>
              <a-col :span="isMobile ? 24 : 12">
                <a-form-item
                  field="cloudRecordingEnabled"
                  label="云端录像"
                  tooltip="这是云端录像的新通道默认值；仅应用于通过 Catalog 新发现的通道，开启后录像对账可能自动发起拉流。"
                >
                  <a-switch
                    v-model="draft.playback.cloudRecordingEnabled"
                    :loading="playbackSettingsLoading || playbackSettingsSaving"
                    :disabled="playbackSettingsLoading || playbackSettingsSaving || !playbackSettingsReady"
                  />
                </a-form-item>
              </a-col>
            </a-row>
          </a-form>
        </a-card>
      </a-tab-pane>

      <a-tab-pane key="cascade" title="国标级联">
        <a-card :bordered="false" class="uvp-system-panel uvp-system-panel--dense mb-4">
          <a-form class="uvp-system-form" :layout="formLayout" :model="draft" auto-label-width>
            <a-row :gutter="24">
              <a-col v-for="field in cascadeFields" :key="field.key" :span="isMobile ? 24 : 12">
                <a-form-item
                  :field="`cascade.${field.key}`"
                  :label="field.label"
                  :tooltip="field.tooltip"
                  :required="field.required"
                  :validate-status="cascadeErrors[field.key] ? 'error' : undefined"
                  :help="cascadeErrors[field.key]"
                >
                  <a-input
                    v-model="draft.cascade[field.key]"
                    class="service-config-text-input"
                    :max-length="field.max"
                    :disabled="configSaving || !configReady"
                    :allow-clear="!field.required"
                  />
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

// 「设备接入地址」小节标题：只作视觉分组，不承担任何逻辑判据。
.service-config-subtitle {
  margin: 4px 0 8px;
  font-size: 13px;
  font-weight: 600;
  color: var(--uvp-text-2, #4e5969);
}

// 从 SIP 日志页跳来时的定位高亮（老板 2026-10-08）。
// ⛔ 只描边不铺大面积底色 —— 暗色下大面积 brand 底会发白发糊（本仓踩过）。
.sip-log-focused {
  border-radius: var(--uvp-border-radius-md, 8px);
  box-shadow: 0 0 0 2px var(--uvp-brand, #2563eb) inset;
}

.service-config-tabs {
  :deep(.arco-tabs-nav) {
    display: flex;
    gap: 12px;
    align-items: flex-start;
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
  gap: 8px;
  align-items: center;
}

.service-config-unsaved {
  display: inline-flex;
  gap: 6px;
  align-items: center;
  margin-right: 4px;
  font-size: 12px;
  font-weight: 600;
  color: var(--uvp-warning);
  white-space: nowrap;
}

.service-config-unsaved__dot {
  width: 7px;
  height: 7px;
  background: var(--uvp-warning);
  border-radius: 50%;
  box-shadow: 0 0 0 3px var(--uvp-warning-soft);
}
.service-config-tabs__actions :deep(.arco-btn) {
  box-sizing: border-box;
  border-radius: 10px;
}
:global(.service-config-page .arco-input-wrapper),
:global(.service-config-page .arco-input-number),
:global(.service-config-page .arco-select-view) {
  box-sizing: border-box;
  background: var(--uvp-search-control-bg) !important;
  border-color: var(--uvp-search-secondary-btn-border) !important;
  border-radius: 10px !important;
}
:global(.service-config-page .arco-input-wrapper:focus-within),
:global(.service-config-page .arco-input-number:focus-within),
:global(.service-config-page .arco-select-view-focus) {
  border-color: var(--uvp-brand) !important;
  box-shadow: var(--uvp-search-control-focus-shadow) !important;
}
:global(.service-config-page .arco-input::placeholder),
:global(.service-config-page .arco-select-view-input::placeholder) {
  color: var(--uvp-text-tertiary) !important;
  opacity: 1;
}

:deep(.service-config-number-input) {
  width: min(100%, 260px);
}

:deep(.service-config-text-input) {
  width: min(100%, 360px);
}

.ptz-default-speed {
  display: grid;
  grid-template-columns: auto minmax(88px, 1fr) auto 48px;
  gap: 8px;
  align-items: center;
  width: min(100%, 480px);
}

.ptz-default-speed__edge {
  font-size: 12px;
  color: var(--color-text-3);
  white-space: nowrap;
}

.ptz-default-speed__value {
  font-variant-numeric: tabular-nums;
  color: var(--color-text-1);
  text-align: right;
  white-space: nowrap;
}

:deep(.stream-transport-select) {
  width: 260px !important;
  max-width: 100%;
}

:deep(.playback-protocol-select) {
  width: 260px !important;
  max-width: 100%;
}

.global-subscription-items {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 6px 20px;
  align-items: center;
  width: min(100%, 560px);
  min-height: 32px;

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

@media (width <= 640px) {
  :deep(.service-config-number-input),
  :deep(.stream-transport-select) {
    width: 100%;
  }

  :deep(.playback-protocol-select) {
    width: 100% !important;
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
