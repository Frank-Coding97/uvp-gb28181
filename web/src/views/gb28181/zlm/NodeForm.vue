<script setup lang="ts">
import { ref, watch } from "vue";
import { Message } from "@arco-design/web-vue";
import {
    createZLMNode,
    type CreateZLMNodeReq
} from "@/api/gb28181-zlm";

const props = defineProps<{
    visible: boolean;
}>();
const emit = defineEmits<{
    (e: "update:visible", v: boolean): void;
    (e: "created"): void;
}>();

const form = ref<CreateZLMNodeReq>({
    name: "",
    host: "",
    apiPort: 18080,
    apiSecret: "",
    weight: 50,
    rtpPortStart: 30000,
    rtpPortEnd: 35000
});
const loading = ref(false);

watch(
    () => props.visible,
    (v) => {
        if (v) {
            form.value = {
                name: "",
                host: "",
                apiPort: 18080,
                apiSecret: "",
                weight: 50,
                rtpPortStart: 30000,
                rtpPortEnd: 35000
            };
        }
    }
);

async function handleSubmit() {
    if (!form.value.name || !form.value.host || !form.value.apiSecret) {
        Message.warning("请填完必填字段");
        return;
    }
    loading.value = true;
    try {
        const res = await createZLMNode(form.value);
        if (res.code === 0) {
            Message.success("节点创建成功");
            emit("created");
            emit("update:visible", false);
        } else {
            Message.error(res.message || "创建失败");
        }
    } catch (e: any) {
        Message.error(e?.message || "创建失败");
    } finally {
        loading.value = false;
    }
}
</script>

<template>
    <a-drawer
        :visible="visible"
        title="添加 ZLM 节点"
        :width="520"
        :ok-loading="loading"
        @ok="handleSubmit"
        @cancel="emit('update:visible', false)"
        class="zlm-node-form"
    >
        <div class="form-hint">
            保存时会自动探测 ZLM 连通性,可达后才写入注册表。
        </div>
        <a-form :model="form" layout="vertical">
            <a-form-item label="节点名" required>
                <a-input v-model="form.name" placeholder="如 zlm-bj-1" />
            </a-form-item>
            <a-form-item label="Host" required>
                <a-input v-model="form.host" placeholder="ZLM API 地址,如 192.168.1.10" />
            </a-form-item>
            <a-form-item label="API 端口" required>
                <a-input-number v-model="form.apiPort" :min="1" :max="65535" />
            </a-form-item>
            <a-form-item label="API Secret" required>
                <a-input-password v-model="form.apiSecret" placeholder="ZLM api.secret" />
            </a-form-item>
            <a-form-item label="权重(加权轮询用)">
                <a-slider v-model="form.weight" :min="0" :max="100" show-input />
                <div class="form-tip">权重影响加权轮询算法的分配比例;0 = 禁用调度;典型值 50</div>
            </a-form-item>
            <a-form-item label="RTP 端口范围">
                <a-space>
                    <a-input-number v-model="form.rtpPortStart" :min="1024" :max="65535" />
                    <span class="form-tip-inline">-</span>
                    <a-input-number v-model="form.rtpPortEnd" :min="1024" :max="65535" />
                </a-space>
                <div class="form-tip">ZLM 端口分配范围,默认 30000-35000</div>
            </a-form-item>
        </a-form>
        <template #footer>
            <a-button @click="emit('update:visible', false)">取消</a-button>
            <a-button type="primary" :loading="loading" @click="handleSubmit">保存</a-button>
        </template>
    </a-drawer>
</template>

<style scoped>
.zlm-node-form :deep(.arco-drawer-header) {
    font-family: var(--zlm-font-body);
}

.zlm-node-form :deep(.arco-drawer-title) {
    font-size: var(--zlm-fs-h2);
    font-weight: var(--zlm-fw-semibold);
    color: var(--zlm-text-1);
}

.form-hint {
    margin-bottom: var(--zlm-space-4);
    padding: var(--zlm-space-3) var(--zlm-space-4);
    background: var(--zlm-brand-50);
    color: var(--zlm-text-2);
    border-radius: var(--zlm-radius-md);
    border-left: 3px solid var(--zlm-brand-500);
    font-size: var(--zlm-fs-caption);
    line-height: var(--zlm-lh-normal);
}

.form-tip {
    font-size: var(--zlm-fs-caption);
    color: var(--zlm-text-3);
    margin-top: 4px;
}

.form-tip-inline {
    color: var(--zlm-text-3);
    margin: 0 4px;
}
</style>
