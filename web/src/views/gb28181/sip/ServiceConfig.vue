<script setup lang="ts">
import { Message } from "@arco-design/web-vue";
import { Check, Pencil, X } from "lucide-vue-next";
import { computed, onMounted, reactive, ref } from "vue";
import {
    fetchPTZDefaultSpeedConfig,
    fetchPositionHistoryConfig,
    fetchSDPExtensionConfig,
    updatePositionHistoryConfig,
    updatePTZDefaultSpeedConfig,
    updateSDPExtensionConfig
} from "@/api/gb28181";
import { useDevicesSize } from "@/hooks/useDevicesSize";
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
const savedPositionHistoryEnabled = ref(true);
const savedPositionHistoryRetentionDays = ref(7);
const savedSDPExtensionEnabled = ref(false);
const savedPTZDefaultSpeed = ref(6);
const positionHistoryChanged = computed(
    () =>
        draft.saveMobilePositionHistory !== savedPositionHistoryEnabled.value ||
        draft.positionHistoryRetentionDays !== savedPositionHistoryRetentionDays.value
);
const sdpExtensionChanged = computed(() => draft.sdpExtension !== savedSDPExtensionEnabled.value);
const ptzDefaultSpeedChanged = computed(() => draft.ptzSpeed !== savedPTZDefaultSpeed.value);
const hasChanges = computed(
    () => positionHistoryChanged.value || sdpExtensionChanged.value || ptzDefaultSpeedChanged.value
);
const configLoading = computed(() => positionHistoryLoading.value || sdpExtensionLoading.value || ptzDefaultSpeedLoading.value);
const configSaving = computed(() => positionHistorySaving.value || sdpExtensionSaving.value || ptzDefaultSpeedSaving.value);
const configReady = computed(() => positionHistoryReady.value && sdpExtensionReady.value && ptzDefaultSpeedReady.value);
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

function startEditing() {
    draft.saveMobilePositionHistory = savedPositionHistoryEnabled.value;
    draft.positionHistoryRetentionDays = savedPositionHistoryRetentionDays.value;
    draft.sdpExtension = savedSDPExtensionEnabled.value;
    draft.ptzSpeed = savedPTZDefaultSpeed.value;
    isEditing.value = true;
}

function cancelEditing() {
    draft.saveMobilePositionHistory = savedPositionHistoryEnabled.value;
    draft.positionHistoryRetentionDays = savedPositionHistoryRetentionDays.value;
    draft.sdpExtension = savedSDPExtensionEnabled.value;
    draft.ptzSpeed = savedPTZDefaultSpeed.value;
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
        isEditing.value = false;
        Message.success("国标服务配置已更新");
    } catch (error: any) {
        Message.error(error?.message || "保存国标服务配置失败");
    } finally {
        positionHistorySaving.value = false;
        sdpExtensionSaving.value = false;
        ptzDefaultSpeedSaving.value = false;
    }
}

onMounted(() => Promise.all([loadPositionHistoryConfig(), loadSDPExtensionConfig(), loadPTZDefaultSpeedConfig()]));
</script>

<template>
    <div class="snow-fill">
        <div class="snow-fill-inner">
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
                                    <a-form-item field="saveMobilePositionHistory" label="保存移动位置历史轨迹">
                                        <a-switch
                                            v-model="draft.saveMobilePositionHistory"
                                            :loading="positionHistoryLoading || positionHistorySaving"
                                            :disabled="!isEditing || positionHistoryLoading || positionHistorySaving || !positionHistoryReady"
                                        >
                                            <template #checked>开启</template>
                                            <template #unchecked>关闭</template>
                                        </a-switch>
                                        <template #extra>
                                            <div>关闭后仍更新设备和通道的最新位置，不再新增轨迹点。</div>
                                        </template>
                                    </a-form-item>
                                </a-col>
                                <a-col :span="isMobile ? 24 : 12">
                                    <a-form-item field="positionHistoryRetentionDays" label="位置历史保留天数（天）">
                                        <a-input-number
                                            v-model="draft.positionHistoryRetentionDays"
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
                                        <template #extra>
                                            <div>历史轨迹按接收时间自动清理，默认保留 7 天。</div>
                                        </template>
                                    </a-form-item>
                                </a-col>
                                <a-col :span="isMobile ? 24 : 12">
                                    <a-form-item field="sdpExtension" label="扩展 SDP 兼容模式">
                                        <a-switch
                                            v-model="draft.sdpExtension"
                                            :loading="sdpExtensionLoading || sdpExtensionSaving"
                                            :disabled="!isEditing || sdpExtensionLoading || sdpExtensionSaving || !sdpExtensionReady"
                                        >
                                            <template #checked>开启</template>
                                            <template #unchecked>关闭</template>
                                        </a-switch>
                                        <template #extra>
                                            <div>为部分兼容性设备在点播和录像回放请求中声明更多视频编码类型，一般设备无需开启。</div>
                                        </template>
                                    </a-form-item>
                                </a-col>
                                <a-col :span="isMobile ? 24 : 12">
                                    <a-form-item field="ptzSpeed" label="云台默认速度">
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
                                        <template #extra>
                                            <div>作为播放弹窗和大屏云台控制的初始档位，可在控制面板临时调整。</div>
                                        </template>
                                    </a-form-item>
                                </a-col>
                                <a-col :span="isMobile ? 24 : 12">
                                    <a-form-item field="syncChannelsOnOnline" label="设备上线时同步通道">
                                        <a-switch v-model="draft.syncChannelsOnOnline" disabled />
                                    </a-form-item>
                                </a-col>
                                <a-col :span="isMobile ? 24 : 12">
                                    <a-form-item field="sipLogEnabled" label="是否开启SIP日志">
                                        <a-switch v-model="draft.sipLogEnabled" disabled />
                                    </a-form-item>
                                </a-col>
                                <a-col :span="isMobile ? 24 : 12">
                                    <a-form-item field="keepChannelStatus" label="保持通道状态">
                                        <a-switch v-model="draft.keepChannelStatus" disabled />
                                    </a-form-item>
                                </a-col>
                                <a-col :span="isMobile ? 24 : 12">
                                    <a-form-item field="onlineOnHeartbeat" label="收到心跳就把设备设置为上线">
                                        <a-switch v-model="draft.onlineOnHeartbeat" disabled />
                                    </a-form-item>
                                </a-col>
                                <a-col :span="isMobile ? 24 : 12">
                                    <a-form-item field="saveAlarmMessages" label="是否存储报警消息">
                                        <a-switch v-model="draft.saveAlarmMessages" disabled />
                                    </a-form-item>
                                </a-col>
                                <a-col :span="isMobile ? 24 : 12">
                                    <a-form-item field="sipTimeoutSec" label="SIP信令超时时间（秒）">
                                        <a-input-number v-model="draft.sipTimeoutSec" :min="1" :max="300" disabled />
                                    </a-form-item>
                                </a-col>
                                <a-col :span="isMobile ? 24 : 12">
                                    <a-form-item field="useRequestIpAsStreamIp" label="使用来源请求ip作为streamIp">
                                        <a-switch v-model="draft.useRequestIpAsStreamIp" disabled />
                                    </a-form-item>
                                </a-col>
                                <a-col :span="isMobile ? 24 : 12">
                                    <a-form-item field="useDeviceSourceIpAsReplyIp" label="是否使用设备来源IP作为回复IP">
                                        <a-switch v-model="draft.useDeviceSourceIpAsReplyIp" disabled />
                                    </a-form-item>
                                </a-col>
                                <a-col :span="isMobile ? 24 : 12">
                                    <a-form-item field="broadcastMissingGbId" label="缺少国标ID是否给所有上级发送消息">
                                        <a-switch v-model="draft.broadcastMissingGbId" disabled />
                                    </a-form-item>
                                </a-col>
                                <a-col :span="isMobile ? 24 : 12">
                                    <a-form-item field="notifyCacheMaxLength" label="设置notify缓存队列最大长度">
                                        <a-input-number v-model="draft.notifyCacheMaxLength" :min="1" :max="100000" disabled />
                                    </a-form-item>
                                </a-col>
                                <a-col :span="isMobile ? 24 : 12">
                                    <a-form-item field="preallocationMode" label="预分配模式">
                                        <a-switch v-model="draft.preallocationMode" disabled />
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
                                    <a-form-item field="autoInvite" label="自动点播">
                                        <a-switch v-model="draft.playback.autoInvite" disabled />
                                    </a-form-item>
                                </a-col>
                                <a-col :span="isMobile ? 24 : 12">
                                    <a-form-item field="inviteTimeoutMs" label="点播超时时间（毫秒）">
                                        <a-input-number v-model="draft.playback.inviteTimeoutMs" :min="1000" :max="300000" disabled />
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
                                        <a-input-number v-model="draft.cascade.parentInviteTimeoutMs" :min="1000" :max="600000" disabled />
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
                                        <a-input-number v-model="draft.cascade.offlineRetryIntervalSec" :min="1" :max="3600" disabled />
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
    </div>
</template>

<style lang="scss" scoped>
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

.mb-4 {
    margin-bottom: 16px;
}

@media (max-width: 640px) {
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
