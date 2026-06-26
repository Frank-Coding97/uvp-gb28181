<script setup lang="ts">
import { ref, watch } from "vue";
import { Message } from "@arco-design/web-vue";
import {
    createZLMNode,
    testZLMNodeConnection,
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
const testing = ref(false);
const testResult = ref<{ ok: boolean; msg: string } | null>(null);

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
            testResult.value = null;
        }
    }
);

async function handleTest() {
    // 试探:先创建临时节点跑 test-connection? 暂时只校验字段非空。
    // T1.5 阶段简化:测试连通性在创建时由后端 Create 流程自动 probe。
    if (!form.value.host || !form.value.apiPort) {
        Message.warning("请填 host + apiPort");
        return;
    }
    Message.info("保存时将自动探测连通性");
}

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
    >
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
            </a-form-item>
            <a-form-item label="RTP 端口范围">
                <a-space>
                    <a-input-number v-model="form.rtpPortStart" :min="1024" :max="65535" />
                    <span>-</span>
                    <a-input-number v-model="form.rtpPortEnd" :min="1024" :max="65535" />
                </a-space>
            </a-form-item>
        </a-form>
        <template #footer>
            <a-button :loading="testing" @click="handleTest">测试连通性</a-button>
            <a-button @click="emit('update:visible', false)">取消</a-button>
            <a-button type="primary" :loading="loading" @click="handleSubmit">保存</a-button>
        </template>
    </a-drawer>
</template>
