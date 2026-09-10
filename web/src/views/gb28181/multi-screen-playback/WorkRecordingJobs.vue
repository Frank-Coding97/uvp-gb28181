<script setup lang="ts">
import { ref, watch } from "vue";
import { listWorkRecordings, stopWorkRecording, type WorkRecordingListItem } from "@/api/gb28181-work-recording";
const props = defineProps<{ visible: boolean; canStop: boolean }>();
const emit = defineEmits<{ (event: "close"): void; (event: "changed"): void; (event: "form", job: WorkRecordingListItem): void }>();
const rows = ref<WorkRecordingListItem[]>([]);
const page = ref(1);
const total = ref(0);
const loading = ref(false);
const error = ref("");
const stopping = ref("");
let sequence = 0;
const states: Record<string, string> = { starting: "开始中", recording: "录像中", stopping: "结束中", stopped: "已结束", failed: "开始失败", unknown: "待核实" };
async function load(nextPage = page.value) {
    const current = ++sequence;
    loading.value = true;
    error.value = "";
    try {
        const response = await listWorkRecordings(nextPage);
        if (response.code !== 0) throw new Error(response.message || "查询作业失败");
        if (current !== sequence) return;
        rows.value = response.data.items;
        total.value = response.data.total;
        page.value = nextPage;
    } catch (reason: any) {
        if (current === sequence) error.value = reason?.response?.data?.message || reason?.message || "查询作业失败";
    } finally {
        if (current === sequence) loading.value = false;
    }
}
async function stop(job: WorkRecordingListItem) {
    if (stopping.value || !props.canStop) return;
    stopping.value = job.id;
    error.value = "";
    try {
        const response = await stopWorkRecording(job.id);
        if (response.code !== 0) throw new Error(response.message || "结束作业未完成，请刷新核实");
        emit("changed");
        await load();
    } catch (reason: any) {
        error.value = reason?.response?.data?.message || reason?.message || "结束作业未完成，请刷新核实";
    } finally {
        stopping.value = "";
    }
}
watch(() => props.visible, visible => {
    if (visible) void load(1);
    else { sequence++; loading.value = false; }
});
</script>

<template>
    <a-modal :visible="visible" title="我的作业记录" :width="760" :footer="false" @cancel="emit('close')">
        <div class="jobs-heading"><span>关闭播放窗口后，可在这里找回作业。以下为最近保存的状态。</span><a-button :loading="loading" @click="load()">刷新</a-button></div>
        <p v-if="error" role="alert" class="jobs-error">{{ error }}</p>
        <a-empty v-if="!loading && !error && !rows.length" description="暂无作业记录" />
        <div v-for="job in rows" :key="job.id" class="job-row">
            <div><strong>{{ job.channelName || `通道 ${job.channelId}` }}</strong><p>开始时间：{{ job.startedAt ? new Date(job.startedAt).toLocaleString() : '尚未确认' }}</p><small>{{ job.id }}</small></div>
            <span>{{ states[job.state] || '待核实' }}</span>
            <a-button @click="emit('form', job)">作业表单</a-button>
            <a-button v-if="canStop && ['recording', 'unknown'].includes(job.state)" :loading="stopping === job.id" :disabled="Boolean(stopping)" status="danger" @click="stop(job)">{{ job.state === 'unknown' ? '重试结束' : '结束录制' }}</a-button>
        </div>
        <a-pagination v-if="total > 10" :current="page" :total="total" :page-size="10" :disabled="loading" @change="load" />
    </a-modal>
</template>

<style scoped>
.jobs-heading { display: flex; align-items: center; gap: 16px; justify-content: space-between; color: var(--color-text-3); font-size: 12px; margin-bottom: 12px; }
.jobs-error { color: rgb(var(--danger-6)); }
.job-row { display: flex; align-items: center; gap: 18px; padding: 16px 0; border-bottom: 1px solid var(--color-border-2); }
.job-row > div { flex: 1; }
.job-row p { margin: 7px 0; color: var(--color-text-2); font-size: 12px; }
.job-row small { color: var(--color-text-3); }
.arco-pagination { margin-top: 18px; justify-content: flex-end; }
</style>
