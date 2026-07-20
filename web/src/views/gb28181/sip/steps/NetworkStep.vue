<script setup lang="ts">
import { computed } from "vue";
import type { SipNetworkInterfaces } from "@/api/gb28181";
import type { SipSetupForm } from "../useSipSetup";
import { deriveNetworkSelection, networkOptions } from "../sipSetupRules";

const props = defineProps<{ form: SipSetupForm; network: SipNetworkInterfaces | null }>();
const emit = defineEmits<{ update: [patch: Partial<SipSetupForm>] }>();

const items = computed(() => networkOptions(props.network?.items || [], props.form.listenIp));
const advertiseItems = computed(() => (props.network?.items || []).filter(item => !item.listenOnly && !item.loopback));

function updateListen(value: string) {
    if (!props.form.deploymentMode) return;
    emit("update", deriveNetworkSelection(props.form.deploymentMode, value, props.form.advertiseIp, props.network?.items || []));
}
</script>

<template>
    <div class="network-form">
        <a-alert v-if="network?.warning" type="warning" :show-icon="true">{{ network.warning }}</a-alert>
        <a-form layout="vertical">
            <a-form-item label="SIP 监听 IP" required>
                <a-select :model-value="form.listenIp" @change="updateListen">
                    <a-option v-for="item in items" :key="`${item.ip}-${item.interfaceName}`" :value="item.ip">
                        <span>{{ item.ip }}</span>
                        <span v-if="item.interfaceName" class="option-meta"> · {{ item.interfaceName }}</span>
                        <a-tag v-if="item.recommended" size="small" color="green">推荐</a-tag>
                        <a-tag v-else-if="item.virtual" size="small">虚拟网卡</a-tag>
                    </a-option>
                </a-select>
            </a-form-item>

            <a-form-item :label="form.deploymentMode === 'public' ? '公网接入 IP' : '设备接入 IP'" required>
                <a-input
                    v-if="form.deploymentMode === 'public' || form.listenIp === '0.0.0.0'"
                    :model-value="form.advertiseIp"
                    placeholder="例如 203.0.113.10"
                    @update:model-value="emit('update', { advertiseIp: $event, advertiseIpInferred: false })"
                />
                <a-input v-else :model-value="form.advertiseIp" readonly />
                <template v-if="form.deploymentMode === 'lan' && form.listenIp === '0.0.0.0'" #extra>
                    <a-select
                        v-if="advertiseItems.length"
                        size="small"
                        :model-value="form.advertiseIp"
                        @change="emit('update', { advertiseIp: $event, advertiseIpInferred: false })"
                    >
                        <a-option v-for="item in advertiseItems" :key="item.ip" :value="item.ip">
                            {{ item.ip }}<span v-if="item.interfaceName"> · {{ item.interfaceName }}</span>
                        </a-option>
                    </a-select>
                </template>
            </a-form-item>
        </a-form>
    </div>
</template>

<style scoped>
.network-form {
    display: grid;
    gap: 16px;
    max-width: 560px;
    margin: 0 auto;
}

.option-meta {
    color: var(--uvp-text-tertiary);
}
</style>
