<script setup lang="ts">
import { computed, watch } from "vue";
import type { SipNetworkAddress, SipNetworkInterfaces } from "@/api/gb28181";
import type { SipSetupForm } from "../useSipSetup";

const props = defineProps<{ form: SipSetupForm; network: SipNetworkInterfaces | null }>();
const emit = defineEmits<{ update: [patch: Partial<SipSetupForm>] }>();

// 只暴露"能对外提供服务"的网卡:排除 loopback (127.x) 和 listenOnly=true 的 0.0.0.0 伪条目.
// 局域网模式下这些是允许用户选的具体网卡;公网模式下 listen 也用这个列表 (选内网侧网卡).
const usableNics = computed<SipNetworkAddress[]>(
    () => (props.network?.items || []).filter(item => !item.loopback && !item.listenOnly)
);

// 下拉选项:所有具体网卡 + 一个"0.0.0.0(所有网卡)"选项.
// 这样"监听哪张网卡"和"监听所有网卡"归到同一个下拉,不再单开高级面板.
const nicOptions = computed(() => {
    const all: Array<{ ip: string; interfaceName?: string; recommended?: boolean; virtual?: boolean; isAll?: boolean }> = [
        { ip: "0.0.0.0", interfaceName: "所有本机网卡", isAll: true }
    ];
    for (const item of usableNics.value) {
        all.push({
            ip: item.ip,
            interfaceName: item.interfaceName,
            recommended: item.recommended,
            virtual: item.virtual
        });
    }
    return all;
});

// 局域网 → listen = advertise = 选中网卡 IP.
//         选中 0.0.0.0 时:listen=0.0.0.0,advertise 落到推荐网卡 IP(必须是具体 IP).
// 公网 → listen = 选中的网卡 IP(可能是 0.0.0.0),advertise 是用户手填的公网 IP.
function onNicChange(nicIp: string) {
    if (!props.form.deploymentMode) return;
    if (props.form.deploymentMode === "lan") {
        if (nicIp === "0.0.0.0") {
            const recommended = usableNics.value.find(i => i.recommended) || usableNics.value[0];
            emit("update", {
                listenIp: "0.0.0.0",
                advertiseIp: recommended?.ip || "",
                advertiseIpInferred: Boolean(recommended)
            });
        } else {
            emit("update", { listenIp: nicIp, advertiseIp: nicIp, advertiseIpInferred: false });
        }
        return;
    }
    // 公网:只改 listen,advertise 保持用户已填的公网 IP
    emit("update", { listenIp: nicIp });
}

function onAdvertiseInputChange(value: string) {
    emit("update", { advertiseIp: value, advertiseIpInferred: false });
}

// 选中当前 nicIp: 局域网 listen 是权威(advertise 从属);公网 listen 是权威.
const currentNic = computed(() => props.form.listenIp);

// 每次 network 数据到达 / 部署模式变化时,如果当前 nic 不在下拉可选列表 → 自动选推荐网卡填进去.
// 判据用 nicOptions(含 0.0.0.0)而不是 usableNics,否则用户主动选 0.0.0.0 会被 watcher 立刻改回具体网卡.
watch(
    () => [nicOptions.value.length, props.form.deploymentMode, currentNic.value] as const,
    ([count, mode, nic]) => {
        if (!count || !mode) return;
        const stillValid = nicOptions.value.some(item => item.ip === nic);
        if (stillValid) return;
        const recommended = usableNics.value.find(item => item.recommended) || usableNics.value[0];
        if (!recommended) return;
        onNicChange(recommended.ip);
    },
    { immediate: true }
);
</script>

<template>
    <div class="network-form">
        <a-alert v-if="network?.warning" type="warning" :show-icon="true">{{ network.warning }}</a-alert>
        <a-form layout="vertical" auto-label-width>
            <!-- 主字段:选网卡.局域网 → 同时作 listen/advertise;公网 → 作 listen -->
            <a-form-item
                :label="form.deploymentMode === 'public' ? '本机内网网卡' : 'SIP 接入网卡'"
                required
            >
                <a-select :model-value="currentNic" :disabled="!nicOptions.length" @change="onNicChange">
                    <a-option
                        v-for="item in nicOptions"
                        :key="`${item.ip}-${item.interfaceName || 'all'}`"
                        :value="item.ip"
                    >
                        <span>{{ item.ip }}</span>
                        <span v-if="item.interfaceName" class="option-meta"> · {{ item.interfaceName }}</span>
                        <a-tag v-if="item.recommended" size="small" color="green">推荐</a-tag>
                        <a-tag v-else-if="item.virtual" size="small">虚拟网卡</a-tag>
                        <a-tag v-else-if="item.isAll" size="small">多网卡</a-tag>
                    </a-option>
                </a-select>
                <template #extra>
                    <span v-if="form.deploymentMode === 'lan'">
                        通常选具体网卡即可;需要同时监听多张网卡时选 0.0.0.0。
                    </span>
                    <span v-else>
                        选择内网侧网卡作为监听地址(选 0.0.0.0 表示监听所有本机网卡)。
                    </span>
                </template>
            </a-form-item>

            <!-- 公网独有:手填对外可达 IPv4 -->
            <a-form-item
                v-if="form.deploymentMode === 'public'"
                label="公网接入 IP"
                required
            >
                <a-input
                    :model-value="form.advertiseIp"
                    placeholder="例如 203.0.113.10"
                    allow-clear
                    @update:model-value="onAdvertiseInputChange"
                />
                <template #extra>
                    设备通过公网接入平台时使用的地址(NAT 场景下填映射后的公网 IPv4)。
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
    margin-left: 2px;
    color: var(--uvp-text-tertiary);
}
</style>
