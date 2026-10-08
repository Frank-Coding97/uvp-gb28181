<script setup lang="ts">
import { computed, ref } from "vue";
import { Message } from "@arco-design/web-vue";
import { Check, Copy, Rocket, Server } from "lucide-vue-next";
import type { SipNetworkInterfaces } from "@/api/gb28181";
import type { SipSetupForm } from "../useSipSetup";
import { activeSipAddresses } from "../sipSetupRules";
import { writeTextToClipboard } from "@/utils/app";

const props = defineProps<{
  form: SipSetupForm;
  hasPassword: boolean;
  network: SipNetworkInterfaces | null;
}>();

const copiedKey = ref<string>("");

const deploymentLabel = computed(() => (props.form.deploymentMode === "public" ? "公网部署" : "局域网部署"));

const serverAddresses = computed(() =>
  activeSipAddresses(props.form.deploymentMode, props.form.listenIp, props.form.advertiseIp, props.network?.items || [])
);

const passwordDisplay = computed(() => {
  if (props.form.password) return props.form.password;
  if (props.hasPassword) return "(保留原密码)";
  return "-";
});

async function copyValue(key: string, value: string) {
  if (!value || value === "-" || value === "(保留原密码)") return;
  if (await writeTextToClipboard(value)) {
    copiedKey.value = key;
    setTimeout(() => {
      if (copiedKey.value === key) copiedKey.value = "";
    }, 1500);
    return;
  }
  Message.warning("复制失败,请手动选中");
}

const fields = computed(() => [
  ...(serverAddresses.value.length ? serverAddresses.value : [""]).map((value, index) => ({
    key: `server-${index}`,
    label: index === 0 ? "SIP 服务器地址" : "",
    value
  })),
  { key: "port", label: "SIP 端口", value: String(props.form.port) },
  { key: "serverId", label: "平台 ID", value: props.form.serverId },
  { key: "domain", label: "SIP 域", value: props.form.domain },
  { key: "password", label: "SIP 密码", value: passwordDisplay.value }
]);

const mediaFields = computed(() => [
  { key: "hook-ip", label: "Hook IP（平台默认）", value: props.form.hookIp || "-" },
  { key: "stream-ip", label: "Stream IP（平台默认）", value: props.form.streamIp || "-" }
]);
</script>

<template>
  <div class="confirm-groups">
    <!-- 部署方式 -->
    <section class="confirm-card confirm-card--intro">
      <div class="confirm-card__icon">
        <Rocket :size="18" />
      </div>
      <div class="confirm-card__intro-body">
        <span class="confirm-card__intro-label">部署方式</span>
        <span class="confirm-card__intro-value">{{ deploymentLabel }}</span>
      </div>
      <a-tag color="arcoblue" class="confirm-card__intro-tag">
        {{ form.deploymentMode === "public" ? "跨公网 NAT" : "同局域网" }}
      </a-tag>
    </section>

    <!-- 设备端接入参数 -->
    <section class="confirm-card">
      <header class="confirm-card__header">
        <span class="confirm-card__icon confirm-card__icon--soft">
          <Server :size="16" />
        </span>
        <h4>设备接入信息</h4>
        <span class="confirm-card__header-hint">设备端 SIP 配置填以下参数</span>
      </header>
      <div class="confirm-card__rows">
        <div v-for="field in fields" :key="field.key" class="confirm-row">
          <span class="confirm-row__label">{{ field.label }}</span>
          <div class="confirm-row__body">
            <code class="confirm-value">{{ field.value || "-" }}</code>
            <button
              v-if="field.value && field.value !== '-' && field.value !== '(保留原密码)'"
              type="button"
              class="confirm-copy"
              :class="{ 'is-copied': copiedKey === field.key }"
              :title="copiedKey === field.key ? '已复制' : `复制${field.label}`"
              @click="copyValue(field.key, field.value)"
            >
              <Check v-if="copiedKey === field.key" :size="14" />
              <Copy v-else :size="14" />
            </button>
          </div>
        </div>
      </div>
    </section>

    <section class="confirm-card">
      <header class="confirm-card__header">
        <span class="confirm-card__icon confirm-card__icon--soft">
          <Server :size="16" />
        </span>
        <h4>媒体网络默认地址</h4>
        <span class="confirm-card__header-hint">媒体节点可单独覆盖</span>
      </header>
      <div class="confirm-card__rows">
        <div v-for="field in mediaFields" :key="field.key" class="confirm-row">
          <span class="confirm-row__label">{{ field.label }}</span>
          <div class="confirm-row__body">
            <code class="confirm-value">{{ field.value }}</code>
            <button
              v-if="field.value !== '-'"
              type="button"
              class="confirm-copy"
              :class="{ 'is-copied': copiedKey === field.key }"
              :title="copiedKey === field.key ? '已复制' : `复制${field.label}`"
              @click="copyValue(field.key, field.value)"
            >
              <Check v-if="copiedKey === field.key" :size="14" />
              <Copy v-else :size="14" />
            </button>
          </div>
        </div>
      </div>
    </section>
  </div>
</template>

<style scoped>
.confirm-groups {
  display: grid;
  gap: 14px;
  max-width: 580px;
  margin: 0 auto;
}

.confirm-card {
  display: flex;
  flex-direction: column;
  gap: 10px;
  padding: 14px 16px;
  background: var(--uvp-panel-bg, #ffffff);
  border: 1px solid var(--uvp-panel-border, #e8edf5);
  border-radius: 12px;
}

.confirm-card__header {
  display: flex;
  gap: 8px;
  align-items: center;
  padding-bottom: 8px;
  border-bottom: 1px dashed var(--uvp-panel-border, #e8edf5);
}

.confirm-card__header h4 {
  margin: 0;
  font-size: 13px;
  font-weight: 600;
  color: var(--uvp-text-primary, #1f2937);
}

.confirm-card__header-hint {
  margin-left: auto;
  font-size: 11.5px;
  color: var(--uvp-text-tertiary, #6b7280);
}

.confirm-card__icon {
  display: inline-flex;
  flex-shrink: 0;
  align-items: center;
  justify-content: center;
  width: 30px;
  height: 30px;
  color: #ffffff;
  background: var(--uvp-brand, #2563eb);
  border-radius: 8px;
}

.confirm-card__icon--soft {
  width: 26px;
  height: 26px;
  color: var(--uvp-brand, #2563eb);
  background: var(--uvp-brand-soft, #e8f2ff);
  border-radius: 7px;
}

/* 部署方式卡:横排 */
.confirm-card--intro {
  flex-direction: row;
  gap: 12px;
  align-items: center;
  padding: 12px 16px;
  background: linear-gradient(120deg, var(--uvp-brand-soft, #e8f2ff) 0%, #ffffff 62%);
  border-color: rgb(37 99 235 / 18%);
}

.confirm-card__intro-body {
  display: flex;
  flex: 1;
  flex-direction: column;
  gap: 2px;
  min-width: 0;
}

.confirm-card__intro-label {
  font-size: 11.5px;
  color: var(--uvp-text-tertiary, #6b7280);
  letter-spacing: 0.4px;
}

.confirm-card__intro-value {
  font-size: 15px;
  font-weight: 620;
  color: var(--uvp-text-primary, #1f2937);
}

.confirm-card__intro-tag {
  flex-shrink: 0;
}

.confirm-card__rows {
  display: grid;
  gap: 12px;
}

.confirm-row {
  display: grid;
  grid-template-columns: 120px minmax(0, 1fr);
  gap: 14px;
  align-items: center;
  padding: 6px 2px;
}

.confirm-row__label {
  font-size: 13px;
  line-height: 1.5;
  color: var(--uvp-text-tertiary, #6b7280);
}

.confirm-row__body {
  display: flex;
  gap: 8px;
  align-items: center;
  min-width: 0;
}

.confirm-value {
  min-width: 0;
  font-family: SFMono-Regular, Consolas, Menlo, monospace;
  font-size: 14.5px;
  font-weight: 600;
  color: var(--uvp-brand-strong, #1d4ed8);
  letter-spacing: 0;
  overflow-wrap: anywhere;
}

.confirm-copy {
  display: inline-flex;
  flex-shrink: 0;
  align-items: center;
  justify-content: center;
  width: 26px;
  height: 26px;
  padding: 0;
  color: var(--uvp-text-tertiary, #6b7280);
  cursor: pointer;
  background: transparent;
  border: 1px solid transparent;
  border-radius: 6px;
  transition: all 160ms ease;
}

.confirm-copy:hover {
  color: var(--uvp-brand, #2563eb);
  background: var(--uvp-brand-soft, #e8f2ff);
  border-color: rgb(37 99 235 / 22%);
}

.confirm-copy.is-copied {
  color: #059669;
  background: var(--uvp-success-soft, #ecfdf5);
  border-color: rgb(5 150 105 / 30%);
}

@media (width <= 520px) {
  .confirm-row {
    grid-template-columns: 1fr;
    gap: 4px;
  }
}
</style>
