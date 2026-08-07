<script setup lang="ts">
import { Message } from "@arco-design/web-vue";
import { onMounted, reactive, ref } from "vue";
import { fetchPositionHistoryConfig, updatePositionHistoryConfig } from "@/api/gb28181";
import { createStaticServiceConfigDraft } from "./serviceConfigState";

const activeTab = ref("gb");
const draft = reactive(createStaticServiceConfigDraft());
const positionHistoryLoading = ref(true);
const positionHistorySaving = ref(false);
const positionHistoryReady = ref(false);

async function loadPositionHistoryConfig() {
    positionHistoryLoading.value = true;
    try {
        const response = await fetchPositionHistoryConfig();
        if (response.code !== 0) throw new Error(response.message || "加载配置失败");
        draft.saveMobilePositionHistory = response.data.enabled;
        positionHistoryReady.value = true;
    } catch (error: any) {
        Message.error(error?.message || "加载移动位置历史轨迹配置失败");
    } finally {
        positionHistoryLoading.value = false;
    }
}

async function updatePositionHistory(enabled: boolean) {
    if (!positionHistoryReady.value) return;
    positionHistorySaving.value = true;
    try {
        const response = await updatePositionHistoryConfig(enabled);
        if (response.code !== 0) throw new Error(response.message || "保存配置失败");
        draft.saveMobilePositionHistory = response.data.enabled;
        Message.success(response.message || "移动位置历史轨迹配置已更新");
    } catch (error: any) {
        draft.saveMobilePositionHistory = !enabled;
        Message.error(error?.message || "保存移动位置历史轨迹配置失败");
    } finally {
        positionHistorySaving.value = false;
    }
}

onMounted(loadPositionHistoryConfig);
</script>

<template>
    <div class="snow-fill">
        <div class="snow-fill-inner uvp-page-shell-flat service-config-page">
            <a-tabs v-model:active-key="activeTab" class="service-config-tabs" lazy-load>
                <a-tab-pane key="gb" title="国标相关">
                    <div class="config-grid">
                        <div class="config-column">
                            <label class="config-item">
                                <span class="config-label">保存移动位置历史轨迹</span>
                                <a-switch
                                    v-model="draft.saveMobilePositionHistory"
                                    :loading="positionHistoryLoading || positionHistorySaving"
                                    :disabled="positionHistoryLoading || positionHistorySaving || !positionHistoryReady"
                                    size="small"
                                    @change="updatePositionHistory"
                                />
                            </label>
                            <label class="config-item">
                                <span class="config-label">SDP扩展</span>
                                <a-switch v-model="draft.sdpExtension" size="small" />
                            </label>
                            <label class="config-item">
                                <span class="config-label">云台速度</span>
                                <a-input-number v-model="draft.ptzSpeed" :min="1" :max="255" hide-button />
                            </label>
                            <label class="config-item">
                                <span class="config-label">设备上线时同步通道</span>
                                <a-switch v-model="draft.syncChannelsOnOnline" size="small" />
                            </label>
                            <label class="config-item">
                                <span class="config-label">是否开启SIP日志</span>
                                <a-switch v-model="draft.sipLogEnabled" size="small" />
                            </label>
                            <label class="config-item">
                                <span class="config-label">保持通道状态</span>
                                <a-switch v-model="draft.keepChannelStatus" size="small" />
                            </label>
                            <label class="config-item">
                                <span class="config-label">收到心跳就把设备设置为上线</span>
                                <a-switch v-model="draft.onlineOnHeartbeat" size="small" />
                            </label>
                        </div>

                        <div class="config-column">
                            <label class="config-item">
                                <span class="config-label">是否存储报警消息</span>
                                <a-switch v-model="draft.saveAlarmMessages" size="small" />
                            </label>
                            <label class="config-item">
                                <span class="config-label">SIP信令超时时间（秒）</span>
                                <a-input-number v-model="draft.sipTimeoutSec" :min="1" :max="300" hide-button />
                            </label>
                            <label class="config-item">
                                <span class="config-label">使用来源请求ip作为streamIp</span>
                                <a-switch v-model="draft.useRequestIpAsStreamIp" size="small" />
                            </label>
                            <label class="config-item">
                                <span class="config-label">是否使用设备来源IP作为回复IP</span>
                                <a-switch v-model="draft.useDeviceSourceIpAsReplyIp" size="small" />
                            </label>
                            <label class="config-item">
                                <span class="config-label">缺少国标ID是否给所有上级发送消息</span>
                                <a-switch v-model="draft.broadcastMissingGbId" size="small" />
                            </label>
                            <label class="config-item">
                                <span class="config-label">设置notify缓存队列最大长度</span>
                                <a-input-number v-model="draft.notifyCacheMaxLength" :min="1" :max="100000" hide-button />
                            </label>
                            <label class="config-item">
                                <span class="config-label">预分配模式</span>
                                <a-switch v-model="draft.preallocationMode" size="small" />
                            </label>
                        </div>
                    </div>
                </a-tab-pane>

                <a-tab-pane key="playback" title="播放相关">
                    <div class="config-grid config-grid-single">
                        <div class="config-column config-column-stacked">
                            <label class="config-item">
                                <span class="config-label">自动点播</span>
                                <a-switch v-model="draft.playback.autoInvite" size="small" />
                            </label>
                            <label class="config-item">
                                <span class="config-label">点播超时时间（毫秒）</span>
                                <a-input-number
                                    v-model="draft.playback.inviteTimeoutMs"
                                    :min="1000"
                                    :max="300000"
                                    hide-button
                                />
                            </label>
                            <label class="config-item">
                                <span class="config-label">推流是否录制</span>
                                <a-switch v-model="draft.playback.recordPushStream" size="small" />
                            </label>
                            <label class="config-item">
                                <span class="config-label">云端录像</span>
                                <a-switch v-model="draft.playback.cloudRecording" size="small" />
                            </label>
                            <label class="config-item">
                                <span class="config-label">是否开启无人观看自动停止</span>
                                <a-switch v-model="draft.playback.stopWhenUnwatched" size="small" />
                            </label>
                            <label class="config-item">
                                <span class="config-label">推流鉴权</span>
                                <a-switch v-model="draft.playback.pushAuth" size="small" />
                            </label>
                        </div>
                    </div>
                </a-tab-pane>

                <a-tab-pane key="cascade" title="国标级联相关">
                    <div class="config-grid config-grid-single">
                        <div class="config-column config-column-stacked">
                            <label class="config-item">
                                <span class="config-label">上级平台点播超时时间</span>
                                <a-input-number
                                    v-model="draft.cascade.parentInviteTimeoutMs"
                                    :min="1000"
                                    :max="600000"
                                    hide-button
                                />
                            </label>
                            <label class="config-item config-item-wide">
                                <span class="config-label">国标级联对讲流模式</span>
                                <a-select v-model="draft.cascade.intercomStreamMode">
                                    <a-option value="TCP被动">TCP被动</a-option>
                                </a-select>
                            </label>
                            <label class="config-item">
                                <span class="config-label">设备/通道状态变化时发送消息</span>
                                <a-switch v-model="draft.cascade.sendStatusChangeMessages" size="small" />
                            </label>
                            <label class="config-item">
                                <span class="config-label">是否使用自定义的ssrc</span>
                                <a-switch v-model="draft.cascade.useCustomSsrc" size="small" />
                            </label>
                            <label class="config-item">
                                <span class="config-label">国标级联离线久重试间隔（秒）</span>
                                <a-input-number
                                    v-model="draft.cascade.offlineRetryIntervalSec"
                                    :min="1"
                                    :max="3600"
                                    hide-button
                                />
                            </label>
                            <label class="config-item">
                                <span class="config-label">国标续订方式</span>
                                <a-switch v-model="draft.cascade.renewalMode" size="small" />
                            </label>
                            <label class="config-item">
                                <span class="config-label">使用推流状态作为推流通道状态</span>
                                <a-switch v-model="draft.cascade.usePushStatusAsChannelStatus" size="small" />
                            </label>
                        </div>
                    </div>
                </a-tab-pane>
            </a-tabs>
        </div>
    </div>
</template>

<style scoped>
.service-config-page {
    min-height: 100%;
    padding: 0;
    background: var(--uvp-panel-bg, #fff);
    border: 1px solid var(--uvp-panel-border);
}

.service-config-tabs {
    min-height: 100%;
}

.config-grid {
    display: grid;
    grid-template-columns: repeat(2, minmax(0, 1fr));
    column-gap: clamp(36px, 7vw, 108px);
    padding: 26px 16px 30px;
}

.config-column {
    min-width: 0;
}

.config-grid-single {
    grid-template-columns: minmax(0, 520px);
}

.config-column-stacked .config-item {
    align-items: flex-start;
    flex-direction: column;
    gap: 8px;
    padding: 8px 0;
}

.config-column-stacked .config-item :deep(.arco-input-wrapper),
.config-column-stacked .config-item :deep(.arco-input-number),
.config-column-stacked .config-item :deep(.arco-select) {
    flex: 0 0 auto;
}

.config-item {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 18px;
    min-height: 56px;
    color: var(--uvp-text-primary);
    font-size: 14px;
}

.config-label {
    min-width: 0;
    line-height: 20px;
}

.config-item :deep(.arco-switch) {
    flex: 0 0 auto;
}

.config-item :deep(.arco-input-wrapper),
.config-item :deep(.arco-input-number) {
    flex: 0 0 200px;
    width: 200px;
}

.config-item-wide :deep(.arco-select) {
    width: 100%;
}

@media (max-width: 900px) {
    .config-grid {
        grid-template-columns: 1fr;
        row-gap: 0;
    }
}

@media (max-width: 640px) {
    .config-grid {
        padding: 18px 16px 24px;
    }

    .config-item {
        align-items: flex-start;
        flex-direction: column;
        gap: 8px;
        padding: 12px 0;
    }

    .config-item :deep(.arco-input-wrapper),
    .config-item :deep(.arco-input-number) {
        flex-basis: auto;
        width: 100%;
    }
}
</style>
