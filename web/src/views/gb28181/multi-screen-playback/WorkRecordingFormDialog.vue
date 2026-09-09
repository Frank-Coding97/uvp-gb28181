<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from "vue";
import { getWorkRecordingForm, saveWorkRecordingForm, type WorkRecordingForm, type WorkRecordingFormDetail } from "@/api/gb28181-work-recording";
const props = defineProps<{ visible: boolean; jobId: string; channelName: string; canEdit: boolean }>();
const emit = defineEmits<{ (event: "close"): void; (event: "saved"): void }>();
const sections = [
    { title: "基本信息", fields: [["projectName", "项目名称"], ["major", "专业"], ["stationArea", "站区"], ["mileage", "里程"], ["anchorSectionNo", "锚段号"], ["workLeader", "作业负责人"], ["startAnchorPillarNo", "起锚支柱号"], ["endAnchorPillarNo", "落锚柱柱号"]] },
    { title: "技术参数", fields: [["tensionWireCarModel", "恒张力放线车型号"], ["tensionWireCarNo", "恒张力放线车编号"], ["setTension", "放线设定张力"], ["straightenerStatus", "校直器状况"], ["straightenerInspector", "校直器检查人"]] }
];
const fields = ref<Record<string, string>>({});
const detail = ref<WorkRecordingFormDetail | null>(null);
const loading = ref(false);
const saving = ref(false);
const error = ref("");
const notice = ref("");
const baseline = ref("");
let sequence = 0;
const dirty = computed(() => Boolean(detail.value) && JSON.stringify(fields.value) !== baseline.value);
const editable = computed(() => props.canEdit && detail.value?.editable && !loading.value && !saving.value);
function accept(data: WorkRecordingFormDetail) {
    detail.value = data;
    fields.value = { ...data.form, workPersonnel: (data.form.workPersonnel || []).join("、") };
    baseline.value = JSON.stringify(fields.value);
}
async function load() {
    const current = ++sequence;
    const jobId = props.jobId;
    detail.value = null;
    loading.value = true;
    error.value = "";
    notice.value = "";
    try {
        const response = await getWorkRecordingForm(jobId);
        if (response.code !== 0) throw new Error(response.message || "表单读取失败");
        if (current !== sequence) return;
        if (response.data.jobId !== jobId) throw new Error("返回的作业与当前表单不一致");
        accept(response.data);
    } catch (reason: any) {
        if (current === sequence) error.value = reason?.response?.data?.message || reason?.message || "表单读取失败";
    } finally {
        if (current === sequence) loading.value = false;
    }
}
async function save() {
    if (!editable.value || !detail.value) return;
    const jobId = props.jobId;
    const current = sequence;
    const form = { ...fields.value, workPersonnel: (fields.value.workPersonnel || "").split(/[、,，\n]/).map(value => value.trim()).filter(Boolean) } as unknown as WorkRecordingForm;
    saving.value = true;
    error.value = "";
    notice.value = "";
    try {
        const response = await saveWorkRecordingForm(jobId, detail.value.formVersion, form);
        if (response.code !== 0) throw new Error(response.message || "保存失败");
        if (current !== sequence) return;
        if (response.data.jobId !== jobId) throw new Error("返回的作业与当前表单不一致");
        accept(response.data);
        notice.value = "草稿已保存";
        emit("saved");
    } catch (reason: any) {
        if (current === sequence) error.value = reason?.response?.data?.message || reason?.message || "保存失败，已保留输入";
    } finally {
        saving.value = false;
    }
}
function close() {
    if (saving.value) return;
    if (dirty.value) { error.value = "有尚未保存的修改，请先保存草稿，或选择放弃修改并关闭。"; return; }
    emit("close");
}
watch(() => [props.visible, props.jobId] as const, ([visible, jobId]) => {
    if (visible && jobId) void load();
    else { sequence++; loading.value = false; }
}, { immediate: true });
onBeforeUnmount(() => { sequence++; });
</script>

<template>
    <a-modal :visible="visible" :width="820" :footer="false" :mask-closable="false" @cancel="close">
        <template #title>放线作业记录</template>
        <div class="work-form-preview">
            <div class="form-context"><span>关联通道 <strong>{{ channelName }}</strong></span><span>{{ detail?.formState === 'submitted' ? '已提交' : '作业草稿' }}</span></div>
            <p v-if="loading">正在读取作业表单…</p>
            <p v-if="error" role="alert" class="form-error">{{ error }}</p>
            <p v-if="notice" role="status">{{ notice }}</p>
            <a-button v-if="!detail && !loading" @click="load">重新读取</a-button>
            <template v-if="detail">
                <section v-for="section in sections" :key="section.title">
                    <h3>{{ section.title }}</h3>
                    <div class="form-grid">
                        <label v-for="[key, label] in section.fields" :key="key"><span>{{ label }}</span><a-input v-model="fields[key]" :disabled="!editable" :max-length="256" :placeholder="key === 'mileage' ? '例如 DK0000 至 DK9999' : `请输入${label}`" /></label>
                        <label v-if="section.title === '基本信息'"><span>作业人员</span><a-input v-model="fields.workPersonnel" :disabled="!editable" placeholder="多位人员用顿号分隔" /></label>
                    </div>
                </section>
                <section>
                    <h3>作业情况</h3>
                    <div class="form-grid notes-grid">
                        <label><span>放线过程情况</span><a-textarea v-model="fields.wireLayingProcess" :disabled="!editable" :max-length="4000" placeholder="请填写放线过程及现场情况" :auto-size="{ minRows: 2, maxRows: 3 }" /></label>
                        <label><span>备注</span><a-textarea v-model="fields.remark" :disabled="!editable" :max-length="4000" placeholder="其他需要说明的事项" :auto-size="{ minRows: 2, maxRows: 3 }" /></label>
                    </div>
                </section>
            </template>
            <footer><span>保存或关闭表单不会结束录像</span><a-button v-if="dirty" :disabled="saving" @click="emit('close')">放弃修改并关闭</a-button><a-button :disabled="saving" @click="close">关闭</a-button><a-button v-if="canEdit" type="primary" :loading="saving" :disabled="!editable" @click="save">保存草稿</a-button></footer>
        </div>
    </a-modal>
</template>

<style scoped>
.work-form-preview { color: var(--color-text-1); }
.form-context { display: flex; justify-content: space-between; align-items: center; padding: 11px 14px; background: var(--color-fill-1); border-radius: 6px; color: var(--color-text-3); font-size: 13px; }
.form-context strong { color: var(--color-text-1); margin-left: 12px; font-weight: 500; }
.form-error { color: rgb(var(--danger-6)); }
.preview-badge { color: rgb(var(--primary-6)); font-size: 12px; }
section { margin-top: 20px; }
h3 { margin: 0 0 13px; padding-left: 9px; border-left: 3px solid rgb(var(--primary-6)); font-size: 14px; font-weight: 600; line-height: 16px; }
.form-grid { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 13px 18px; }
label { display: flex; flex-direction: column; gap: 7px; font-size: 13px; color: var(--color-text-2); }
.notes-grid { grid-template-columns: repeat(2, minmax(0, 1fr)); }
footer { margin-top: 22px; padding-top: 16px; border-top: 1px solid var(--color-border-2); display: flex; align-items: center; gap: 10px; }
footer > span { flex: 1; color: var(--color-text-3); font-size: 12px; }
@media (max-width: 620px) { .form-grid { grid-template-columns: repeat(2, minmax(0, 1fr)); } }
</style>
