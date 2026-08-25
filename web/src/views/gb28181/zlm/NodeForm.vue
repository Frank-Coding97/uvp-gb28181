<script setup lang="ts">
import { ref, watch } from "vue";
import { Message } from "@arco-design/web-vue";
import {
    createZLMNode,
    updateZLMNode,
    type ZLMNode,
    type CreateZLMNodeReq
} from "@/api/gb28181-zlm";

const props = defineProps<{
    visible: boolean;
    node?: ZLMNode | null;
}>();
const emit = defineEmits<{
    (e: "update:visible", v: boolean): void;
    (e: "saved"): void;
}>();

const form = ref<CreateZLMNodeReq>({
    name: "",
    host: "",
    receiveHost: "",
    playbackHost: "",
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
            if (props.node) {
                form.value = {
                    name: props.node.name,
                    host: props.node.host,
                    receiveHost: props.node.receiveHost || "",
                    playbackHost: props.node.playbackHost || "",
                    apiPort: props.node.apiPort,
                    apiSecret: "",
                    weight: props.node.weight,
                    rtpPortStart: props.node.rtpPortStart,
                    rtpPortEnd: props.node.rtpPortEnd
                };
            } else {
                form.value = {
                    name: "",
                    host: "",
                    receiveHost: "",
                    playbackHost: "",
                    apiPort: 18080,
                    apiSecret: "",
                    weight: 50,
                    rtpPortStart: 30000,
                    rtpPortEnd: 35000
                };
            }
        }
    }
);

const editing = () => Boolean(props.node);

async function handleSubmit() {
    if (!form.value.name || !form.value.host || (!editing() && !form.value.apiSecret)) {
        Message.warning("请填完必填字段");
        return;
    }
    loading.value = true;
    try {
        const res = editing()
            ? await updateZLMNode(props.node!.id, {
                name: form.value.name,
                receiveHost: form.value.receiveHost,
                playbackHost: form.value.playbackHost,
                weight: form.value.weight,
                rtpPortStart: form.value.rtpPortStart,
                rtpPortEnd: form.value.rtpPortEnd,
                ...(form.value.apiSecret ? { apiSecret: form.value.apiSecret } : {})
            })
            : await createZLMNode(form.value);
        if (res.code === 0) {
            Message.success(editing() ? "节点配置已更新" : "节点创建成功");
            emit("saved");
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
        :title="editing() ? '编辑流媒体节点' : '添加流媒体节点'"
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
            <a-form-item label="管理地址（API Host）" required>
                <a-input v-model="form.host" :disabled="editing()" placeholder="后端访问 ZLM 的地址,如 192.168.1.10" />
                <div class="form-tip">用于后端访问 ZLM API；编辑时修改请重新添加节点。</div>
            </a-form-item>
            <a-form-item label="设备收流地址">
                <a-input v-model="form.receiveHost" placeholder="写入 SDP 的地址,留空跟随管理地址" />
                <div class="form-tip">设备向此地址发送 RTP。公网部署时填写设备可达的公网 IP。</div>
            </a-form-item>
            <a-form-item label="播放访问地址">
                <a-input v-model="form.playbackHost" placeholder="浏览器播放地址,可填公网 IP 或域名" />
                <div class="form-tip">用于生成 FLV、HLS、WebRTC 等播放 URL，留空跟随管理地址。</div>
            </a-form-item>
            <a-form-item label="API 端口" required>
                <a-input-number v-model="form.apiPort" :min="1" :max="65535" :disabled="editing()" />
            </a-form-item>
            <a-form-item label="API Secret" :required="!editing()">
                <a-input-password v-model="form.apiSecret" :placeholder="editing() ? '留空表示不修改' : 'ZLM api.secret'" />
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

:global(.zlm-node-form .arco-input-wrapper),
:global(.zlm-node-form .arco-input-number),
:global(.zlm-node-form .arco-input-password) {
    box-sizing: border-box;
    min-height: 44px;
    background: var(--uvp-search-control-bg) !important;
    border-color: var(--uvp-search-secondary-btn-border) !important;
    border-radius: 10px !important;
    box-shadow: var(--uvp-search-control-shadow) !important;
}

:global(.zlm-node-form .arco-input-wrapper:focus-within),
:global(.zlm-node-form .arco-input-number:focus-within),
:global(.zlm-node-form .arco-input-password:focus-within) {
    border-color: var(--uvp-brand) !important;
    box-shadow: var(--uvp-search-control-focus-shadow) !important;
}

:global(.zlm-node-form .arco-input::placeholder) {
    color: var(--uvp-text-tertiary) !important;
    opacity: 1;
}

:global(.zlm-node-form .arco-drawer-footer .arco-btn) {
    box-sizing: border-box;
    height: 44px;
    min-height: 44px;
    border-radius: 10px;
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
