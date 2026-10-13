<script setup lang="ts">
import { computed, watch } from "vue";
import type { SipNetworkAddress, SipNetworkInterfaces } from "@/api/gb28181";
import type { SipSetupForm } from "../useSipSetup";
import {
  isVirtualListenIp,
  resolveMediaAddressPolicy,
  validateHookIp,
  validateSdpIp,
  validateStreamIp
} from "../mediaNetworkAddress";

const props = defineProps<{ form: SipSetupForm; network: SipNetworkInterfaces | null }>();
const emit = defineEmits<{ update: [patch: Partial<SipSetupForm>] }>();

// 只暴露"能对外提供服务"的网卡:排除 loopback (127.x) 和 listenOnly=true 的 0.0.0.0 伪条目.
// 局域网模式下这些是允许用户选的具体网卡;公网模式下 listen 也用这个列表 (选内网侧网卡).
const usableNics = computed<SipNetworkAddress[]>(() =>
  (props.network?.items || []).filter(item => !item.loopback && !item.listenOnly)
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

// 局域网 → 具体网卡时 listen = advertise = 选中 IP.
//         选中 0.0.0.0 时不持久化单一 advertise IP,运行时按设备路由选择本地地址.
// 公网 → listen = 选中的网卡 IP(可能是 0.0.0.0),advertise 是用户手填的公网 IP.
function onNicChange(nicIp: string) {
  if (!props.form.deploymentMode) return;
  if (props.form.deploymentMode === "lan") {
    if (nicIp === "0.0.0.0") {
      emit("update", {
        listenIp: "0.0.0.0",
        advertiseIp: "",
        advertiseIpInferred: false
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

// 三个媒体地址的填写规则跟着「当前选中的网卡」走:
//   真实网卡 ⇒ 可留空(留空即用该网卡地址);虚拟网卡 / 0.0.0.0 ⇒ 必须手填。
// ⛔ 判据与后端 setup.ResolveMediaAddressPolicy 同源,别在这里另写一套。
const mediaPolicy = computed(() =>
  resolveMediaAddressPolicy({
    deploymentMode: props.form.deploymentMode,
    listenIp: props.form.listenIp,
    advertiseIp: props.form.advertiseIp,
    networkItems: props.network?.items
  })
);
const hookIpError = computed(() => validateHookIp(props.form.hookIp, mediaPolicy.value.hookIp));
const sdpIpError = computed(() => validateSdpIp(props.form.sdpIp, mediaPolicy.value.sdpIp));
const streamIpError = computed(() => validateStreamIp(props.form.streamIp, mediaPolicy.value.streamIp));

type MediaAddressField = "hookIp" | "sdpIp" | "streamIp";

/** 监听网卡本身推断不出来的原因（用于「为什么突然必填」的解释）。 */
const listenReason = computed(() => {
  if (props.form.listenIp === "0.0.0.0") return "当前监听地址是 0.0.0.0（所有网卡），无法自动推断出可用地址";
  if (isVirtualListenIp(props.form.listenIp, props.network?.items)) return "当前选中的是虚拟网卡，它的地址设备侧访问不到";
  return "当前监听网卡无法自动推断出可用地址";
});

function ruleHelp(field: MediaAddressField): string {
  const rule = mediaPolicy.value[field];
  if (rule.optional) return `可留空；留空时自动使用 ${rule.fallback}`;
  // 公网模式下 SDP / Stream 与网卡无关，缺的是对外地址。
  if (field !== "hookIp" && props.form.deploymentMode === "public") {
    return "请先填写「公网接入 IP」：设备在公网侧，只能访问该地址";
  }
  return `${listenReason.value}，必须手填`;
}

function placeholderFor(field: MediaAddressField, formatHint: string): string {
  const rule = mediaPolicy.value[field];
  return rule.optional ? `可留空，默认 ${rule.fallback}` : `必填：${formatHint}`;
}

function updateHookIp(value: string) {
  emit("update", { hookIp: value });
}

function updateSdpIp(value: string) {
  emit("update", { sdpIp: value });
}

function updateStreamIp(value: string) {
  emit("update", { streamIp: value });
}

// 每次 network 数据到达 / 部署模式变化时,如果当前 nic 不在下拉可选列表 → 自动选推荐网卡填进去.
// 判据用 nicOptions(含 0.0.0.0)而不是 usableNics,否则用户主动选 0.0.0.0 会被 watcher 立刻改回具体网卡.
watch(
  () => [nicOptions.value.length, props.form.deploymentMode, currentNic.value] as const,
  ([count, mode, nic]) => {
    if (!count || !mode) return;
    const stillValid = nicOptions.value.some(item => item.ip === nic);
    if (stillValid) return;
    // 默认选中优先序:推荐且非虚拟 > 非虚拟 > 推荐 > 列表首项。
    // ⛔ "非虚拟"必须比"推荐"更靠前:后端判据靠网卡名猜,认不出某些虚拟网卡是常态
    //    (历史上 Windows 友好名整批漏过),此时若信它的 recommended,就会默认选中虚拟网卡;
    //    而前后端版本不同步(只更了一端)时同样会走到这条路。
    const recommended =
      usableNics.value.find(item => item.recommended && !item.virtual) ||
      usableNics.value.find(item => !item.virtual) ||
      usableNics.value.find(item => item.recommended) ||
      usableNics.value[0];
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
      <a-form-item :label="form.deploymentMode === 'public' ? '本机内网网卡' : 'SIP 接入网卡'" required>
        <a-select :model-value="currentNic" :disabled="!nicOptions.length" @change="onNicChange">
          <a-option v-for="item in nicOptions" :key="`${item.ip}-${item.interfaceName || 'all'}`" :value="item.ip">
            <span>{{ item.ip }}</span>
            <span v-if="item.interfaceName" class="option-meta"> · {{ item.interfaceName }}</span>
            <a-tag v-if="item.virtual" size="small">虚拟网卡</a-tag>
            <a-tag v-else-if="item.isAll" size="small">多网卡</a-tag>
          </a-option>
        </a-select>
        <template #extra>
          <span v-if="form.deploymentMode === 'lan'"> 通常选具体网卡即可;需要同时监听多张网卡时选 0.0.0.0。 </span>
          <span v-else> 选择内网侧网卡作为监听地址(选 0.0.0.0 表示监听所有本机网卡)。 </span>
        </template>
      </a-form-item>

      <!-- 公网独有:手填对外可达 IPv4 -->
      <a-form-item v-if="form.deploymentMode === 'public'" label="公网接入 IP" required>
        <a-input
          :model-value="form.advertiseIp"
          placeholder="例如 203.0.113.10"
          allow-clear
          @update:model-value="onAdvertiseInputChange"
        />
        <template #extra> 设备通过公网接入平台时使用的地址(NAT 场景下填映射后的公网 IPv4)。 </template>
      </a-form-item>

      <section class="media-address-section" aria-labelledby="media-address-title">
        <h3 id="media-address-title">媒体网络地址</h3>
        <div class="media-address-grid">
          <a-form-item
            label="Hook IP"
            :required="!mediaPolicy.hookIp.optional"
            :validate-status="hookIpError ? 'error' : undefined"
            :help="hookIpError || ruleHelp('hookIp')"
          >
            <a-input
              data-testid="media-hook-ip"
              :model-value="form.hookIp"
              allow-clear
              :max-length="45"
              :placeholder="placeholderFor('hookIp', '具体 IPv4 或 IPv6')"
              @update:model-value="updateHookIp"
            />
            <template #extra>ZLMediaKit 回调平台时使用的地址；媒体节点可单独覆盖。</template>
          </a-form-item>

          <a-form-item
            label="SDP IP"
            :required="!mediaPolicy.sdpIp.optional"
            :validate-status="sdpIpError ? 'error' : undefined"
            :help="sdpIpError || ruleHelp('sdpIp')"
          >
            <a-input
              data-testid="media-sdp-ip"
              :model-value="form.sdpIp"
              allow-clear
              :max-length="253"
              :placeholder="placeholderFor('sdpIp', '设备可访问的 IP 或域名')"
              @update:model-value="updateSdpIp"
            />
            <template #extra>写入点播 INVITE 的 SDP 地址（设备据此回推 RTP 流）；不能填 127.0.0.1，媒体节点可单独覆盖。</template>
          </a-form-item>

          <a-form-item
            label="Stream IP"
            :required="!mediaPolicy.streamIp.optional"
            :validate-status="streamIpError ? 'error' : undefined"
            :help="streamIpError || ruleHelp('streamIp')"
          >
            <a-input
              data-testid="media-stream-ip"
              :model-value="form.streamIp"
              allow-clear
              :max-length="253"
              :placeholder="placeholderFor('streamIp', '播放地址（IP 或域名）')"
              @update:model-value="updateStreamIp"
            />
            <template #extra>浏览器播放媒体时使用的主机名，不含协议、端口或路径；媒体节点可单独覆盖。</template>
          </a-form-item>
        </div>
      </section>
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

.media-address-section {
  padding-top: 14px;
  border-top: 1px solid var(--uvp-panel-border, #e8edf5);
}

.media-address-section h3 {
  margin: 0 0 12px;
  font-size: 13px;
  font-weight: 600;
  color: var(--uvp-text-primary);
}

.media-address-grid {
  display: grid;
  grid-template-columns: minmax(0, 1fr);
  gap: 0;
}
</style>
