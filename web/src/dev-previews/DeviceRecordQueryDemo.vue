<script setup lang="ts">
import { ref } from "vue";
import { Circle, Grid2X2, History, List, Play, Video } from "@lucide/vue";
import type { ChannelVO } from "../views/gb28181/device-mgmt/api";
import DeviceRecordQueryDrawer from "../views/gb28181/device-mgmt/components/DeviceRecordQueryDrawer.vue";
import { closeRecordQueryEntry, createRecordQueryEntryState, openRecordQueryEntry } from "../views/gb28181/device-mgmt/recordQueryEntryState";
import { resolveRecordQueryMockScenario, type RecordQueryMockScenario } from "../views/gb28181/device-mgmt/recordQueryMock";

type ViewMode = "list" | "card";

const channels: ChannelVO[] = [
    {
        id: 31,
        channelId: "34020000001320000001",
        deviceId: "34020000002000000001",
        name: "东门出入口",
        alias: "园区东门",
        manufacturer: "海康威视",
        model: "DS-2CD3T87WD-L",
        owner: "",
        civilCode: "340200",
        parentId: "34020000002000000001",
        ptzType: 0,
        longitude: 120.1551,
        latitude: 30.2741,
        status: 1,
        streamId: "",
        onDemandLive: true,
        streamTransport: "TCP-Passive",
        audioEnabled: true,
        cloudRecordingEnabled: false,
        cloudRecordingState: "disabled",
        cloudRecordingError: "",
        createdAt: "2026-08-02T08:00:00+08:00",
        updatedAt: "2026-08-02T19:40:20+08:00"
    },
    {
        id: 32,
        channelId: "34020000001320000002",
        deviceId: "34020000002000000001",
        name: "停车场通道",
        alias: "地下车库入口",
        manufacturer: "大华股份",
        model: "IPC-HFW5442E-Z",
        owner: "",
        civilCode: "340200",
        parentId: "34020000002000000001",
        ptzType: 0,
        longitude: 120.1542,
        latitude: 30.2736,
        status: 1,
        streamId: "",
        onDemandLive: true,
        streamTransport: "UDP",
        audioEnabled: false,
        cloudRecordingEnabled: false,
        cloudRecordingState: "disabled",
        cloudRecordingError: "",
        createdAt: "2026-08-02T08:00:00+08:00",
        updatedAt: "2026-08-02T19:39:10+08:00"
    }
];

const scenarios: Array<{ label: string; value: RecordQueryMockScenario }> = [
    { label: "完整结果", value: "complete" },
    { label: "空结果", value: "empty" },
    { label: "部分结果", value: "partial" },
    { label: "查询超时", value: "timeout" },
    { label: "设备离线", value: "offline" },
    { label: "查询失败", value: "error" }
];

const viewMode = ref<ViewMode>("list");
const scenario = ref<RecordQueryMockScenario>(resolveRecordQueryMockScenario());
const recordQueryEntry = ref(createRecordQueryEntryState());

function openRecordQuery(channel: ChannelVO) {
    recordQueryEntry.value = openRecordQueryEntry(recordQueryEntry.value, channel);
}

function handleRecordQueryVisible(visible: boolean) {
    if (!visible) recordQueryEntry.value = closeRecordQueryEntry(recordQueryEntry.value);
}

function changeScenario() {
    const url = new URL(window.location.href);
    url.hash = `/device-record-query-demo?recordQueryMock=${scenario.value}`;
    window.history.replaceState({}, "", url);
}
</script>

<template>
    <div class="record-query-demo">
        <header class="demo-header">
            <div>
                <span class="demo-kicker">国标设备 / 通道管理</span>
                <h1>设备管理</h1>
                <p>录像查询入口位于通道操作区，设备层不重复提供入口。</p>
            </div>
            <label class="scenario-select">
                <span>结果场景</span>
                <select v-model="scenario" @change="changeScenario">
                    <option v-for="item in scenarios" :key="item.value" :value="item.value">{{ item.label }}</option>
                </select>
            </label>
        </header>

        <main class="demo-main">
            <div class="table-toolbar">
                <div class="summary-group">
                    <span class="summary-item"><b>1</b> 台设备</span>
                    <span class="summary-item"><b>2</b> 个通道</span>
                    <span class="online-dot"><Circle :size="9" fill="currentColor" /> 全部在线</span>
                </div>
                <div class="view-switch" aria-label="视图模式">
                    <button
                        type="button"
                        :class="{ active: viewMode === 'list' }"
                        data-testid="record-query-view-list"
                        aria-label="列表视图"
                        @click="viewMode = 'list'"
                    ><List :size="15" />列表</button>
                    <button
                        type="button"
                        :class="{ active: viewMode === 'card' }"
                        data-testid="record-query-view-card"
                        aria-label="卡片视图"
                        @click="viewMode = 'card'"
                    ><Grid2X2 :size="15" />卡片</button>
                </div>
            </div>

            <div v-if="viewMode === 'list'" class="channel-table-wrap">
                <table class="channel-table">
                    <thead><tr><th>通道名称</th><th>通道国标编号</th><th>所属设备</th><th>厂商 / 型号</th><th>状态</th><th class="actions-column">操作</th></tr></thead>
                    <tbody>
                        <tr v-for="channel in channels" :key="channel.id">
                            <td><div class="channel-name"><span class="video-icon"><Video :size="15" /></span><div><strong>{{ channel.alias }}</strong><small>{{ channel.name }}</small></div></div></td>
                            <td><code>{{ channel.channelId }}</code></td>
                            <td><span>园区 NVR-A</span><small class="cell-subtext">{{ channel.deviceId }}</small></td>
                            <td><span>{{ channel.manufacturer }}</span><small class="cell-subtext">{{ channel.model }}</small></td>
                            <td><span class="status-pill"><Circle :size="8" fill="currentColor" />在线</span></td>
                            <td>
                                <div class="row-actions">
                                    <button type="button" class="action-link play"><Play :size="13" />播放</button>
                                    <button
                                        type="button"
                                        class="action-link record"
                                        :data-testid="`record-query-list-entry-${channel.id}`"
                                        @click="openRecordQuery(channel)"
                                    ><History :size="13" />录像</button>
                                    <button type="button" class="action-link detail">详情</button>
                                </div>
                            </td>
                        </tr>
                    </tbody>
                </table>
            </div>

            <div v-else class="channel-grid">
                <article v-for="channel in channels" :key="channel.id" class="channel-card">
                    <div class="preview-area"><Video :size="32" /><span>通道快照</span><strong>{{ channel.alias }}</strong></div>
                    <div class="card-info">
                        <div><strong>{{ channel.alias }}</strong><code>{{ channel.channelId }}</code></div>
                        <span class="status-pill"><Circle :size="8" fill="currentColor" />在线</span>
                    </div>
                    <div class="card-footer">
                        <span>{{ channel.manufacturer }} · {{ channel.model }}</span>
                        <div class="card-actions">
                            <a-tooltip content="点播" position="top"><button type="button" class="icon-button play" aria-label="点播"><Play :size="14" /></button></a-tooltip>
                            <a-tooltip content="查询设备录像" position="top">
                                <button
                                    type="button"
                                    class="icon-button record"
                                    aria-label="查询设备录像"
                                    :data-testid="`record-query-card-entry-${channel.id}`"
                                    @click.stop="openRecordQuery(channel)"
                                    @dblclick.stop
                                ><History :size="14" /></button>
                            </a-tooltip>
                        </div>
                    </div>
                </article>
            </div>
        </main>

        <DeviceRecordQueryDrawer
            :key="recordQueryEntry.token"
            :visible="recordQueryEntry.visible"
            :channel="recordQueryEntry.target"
            @update:visible="handleRecordQueryVisible"
        />
    </div>
</template>

<style scoped>
.record-query-demo { min-height: 100vh; color: #172033; background: #f3f5f8; }
.demo-header { display: flex; justify-content: space-between; gap: 24px; align-items: flex-end; padding: 24px 32px 20px; background: #fff; border-bottom: 1px solid #dfe4eb; }
.demo-kicker { color: #697386; font-size: 12px; }
.demo-header h1 { margin: 5px 0 3px; font-size: 24px; line-height: 32px; letter-spacing: 0; }
.demo-header p { margin: 0; color: #697386; font-size: 13px; }
.scenario-select { display: flex; flex-direction: column; gap: 5px; color: #697386; font-size: 12px; }
.scenario-select select { min-width: 150px; height: 36px; padding: 0 32px 0 10px; color: #273247; background: #fff; border: 1px solid #cfd6e1; border-radius: 5px; }
.scenario-select select:focus-visible, button:focus-visible { outline: 2px solid #2563eb; outline-offset: 2px; }
.demo-main { padding: 20px 32px 32px; }
.table-toolbar { display: flex; justify-content: space-between; gap: 16px; align-items: center; min-height: 52px; padding: 8px 12px 8px 16px; background: #fff; border: 1px solid #dfe4eb; border-bottom: 0; border-radius: 6px 6px 0 0; }
.summary-group, .view-switch, .row-actions, .card-actions { display: flex; gap: 8px; align-items: center; }
.summary-item { padding-right: 12px; color: #697386; font-size: 12px; border-right: 1px solid #e5e9ef; }
.summary-item b { color: #172033; font-size: 14px; }
.online-dot, .status-pill { display: inline-flex; gap: 5px; align-items: center; color: #16805b; font-size: 12px; }
.view-switch { padding: 3px; background: #eef1f5; border-radius: 5px; }
.view-switch button { display: inline-flex; min-height: 30px; gap: 5px; align-items: center; padding: 0 10px; color: #697386; background: transparent; border: 0; border-radius: 4px; cursor: pointer; }
.view-switch button.active { color: #172033; background: #fff; box-shadow: 0 1px 2px rgb(23 32 51 / 10%); }
.channel-table-wrap { overflow-x: auto; background: #fff; border: 1px solid #dfe4eb; border-radius: 0 0 6px 6px; }
.channel-table { width: 100%; min-width: 1120px; border-collapse: collapse; }
.channel-table th { height: 42px; padding: 0 14px; color: #697386; background: #f8f9fb; font-size: 12px; font-weight: 500; text-align: left; border-bottom: 1px solid #dfe4eb; }
.channel-table td { height: 72px; padding: 10px 14px; color: #273247; font-size: 12px; border-bottom: 1px solid #e8ecf1; }
.channel-table tbody tr:last-child td { border-bottom: 0; }
.channel-table tbody tr:hover { background: #f8fafc; }
.actions-column { width: 190px; }
.channel-name { display: flex; gap: 9px; align-items: center; }
.channel-name > div, .channel-table td:nth-child(3), .channel-table td:nth-child(4) { display: flex; flex-direction: column; gap: 3px; }
.channel-name strong { font-size: 13px; }
.channel-name small, .cell-subtext { color: #8993a4; font-size: 11px; }
.video-icon { display: grid; width: 30px; height: 30px; place-items: center; color: #2563eb; background: #edf4ff; border-radius: 5px; }
code { color: #596579; font-family: ui-monospace, SFMono-Regular, Menlo, monospace; font-size: 11px; }
.action-link { display: inline-flex; min-height: 30px; gap: 4px; align-items: center; padding: 0 5px; background: transparent; border: 0; border-radius: 4px; cursor: pointer; }
.action-link.play { color: #2563eb; }
.action-link.record { color: #6b4f9b; }
.action-link.detail { color: #0f7490; }
.action-link:hover, .icon-button:hover { background: rgb(37 99 235 / 8%); }
.channel-grid { display: grid; grid-template-columns: repeat(2, minmax(280px, 1fr)); gap: 14px; }
.channel-card { overflow: hidden; background: #fff; border: 1px solid #dfe4eb; border-radius: 6px; }
.preview-area { display: flex; min-height: 170px; flex-direction: column; gap: 5px; align-items: center; justify-content: center; color: #8a95a6; background: #e9edf2; }
.preview-area strong { color: #3b4659; font-size: 14px; }
.preview-area span { font-size: 11px; }
.card-info, .card-footer { display: flex; justify-content: space-between; gap: 12px; align-items: center; padding: 12px 14px; }
.card-info { border-bottom: 1px solid #e8ecf1; }
.card-info > div { display: flex; flex-direction: column; gap: 4px; min-width: 0; }
.card-info strong { font-size: 14px; }
.card-footer { min-height: 50px; color: #697386; font-size: 11px; }
.icon-button { display: grid; width: 32px; height: 32px; place-items: center; background: #fff; border: 1px solid #d8dee8; border-radius: 5px; cursor: pointer; }
.icon-button.play { color: #2563eb; }
.icon-button.record { color: #6b4f9b; }
@media (max-width: 700px) {
    .demo-header { align-items: stretch; padding: 18px 16px; }
    .demo-header, .table-toolbar { flex-direction: column; }
    .demo-header h1 { font-size: 20px; line-height: 28px; }
    .scenario-select select { width: 100%; min-height: 44px; }
    .demo-main { padding: 14px 12px 24px; }
    .table-toolbar { align-items: stretch; border-bottom: 1px solid #dfe4eb; border-radius: 6px; }
    .summary-group { flex-wrap: wrap; }
    .view-switch { align-self: flex-start; }
    .view-switch button { min-height: 40px; }
    .channel-table-wrap { margin-top: 10px; border-radius: 6px; }
    .channel-grid { grid-template-columns: 1fr; margin-top: 10px; }
    .icon-button { width: 44px; height: 44px; }
}
</style>
