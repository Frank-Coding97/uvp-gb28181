<script setup lang="ts">
import { Copy } from "@lucide/vue";

interface ProtocolOption {
  value: string;
  label: string;
  browserPlayable: boolean;
}

defineProps<{
  protocol: string;
  protocolUrls: Record<string, string | null>;
  availableOptions: ProtocolOption[];
  shortcutOptions: ProtocolOption[];
  placeholder: string;
  canSharePlayback: boolean;
}>();

const emit = defineEmits<{
  (event: "switchProtocol", value: string): void;
  (event: "copyProtocol", value: string): void;
}>();
</script>

<template>
  <div class="protocol-switcher">
    <div class="switcher-left">
      <span class="kicker">播放协议</span>
      <a-select
        :model-value="protocol"
        :style="{ width: '160px' }"
        :placeholder="placeholder"
        size="small"
        :trigger-props="{ autoFitPopupWidth: false }"
        @change="emit('switchProtocol', String($event))"
      >
        <a-option
          v-for="option in availableOptions"
          :key="option.value"
          :value="option.value"
          :label="option.label"
          :disabled="!option.browserPlayable"
        >
          <div class="protocol-option">
            <strong>{{ option.label }}:</strong>
            <span class="protocol-url" :title="protocolUrls[option.value] || ''">{{ protocolUrls[option.value] }}</span>
            <button
              v-if="canSharePlayback"
              type="button"
              class="protocol-copy-btn"
              :title="`复制 ${option.label} 地址`"
              :aria-label="`复制 ${option.label} 地址`"
              @mousedown.stop.prevent
              @click.stop="emit('copyProtocol', option.value)"
            >
              <Copy :size="13" />
            </button>
          </div>
        </a-option>
      </a-select>
    </div>
    <div class="switcher-right">
      <button
        v-for="option in shortcutOptions"
        :key="option.value"
        class="proto-btn"
        :class="{ active: protocol === option.value }"
        :disabled="!protocolUrls[option.value]"
        @click="emit('switchProtocol', option.value)"
      >
        {{ option.label }}
      </button>
    </div>
  </div>
</template>

<style scoped lang="scss">
.protocol-switcher {
  display: flex;
  flex-wrap: wrap;
  gap: 12px;
  align-items: center;
  padding: 10px 14px;
  background: rgb(15 23 42 / 92%);
  border-top: 1px solid rgb(255 255 255 / 8%);
  border-bottom-right-radius: 13px;
  border-bottom-left-radius: 13px;
  backdrop-filter: blur(12px);
}
.switcher-left {
  display: flex;
  gap: 10px;
  align-items: center;
}
.switcher-left .kicker {
  font-size: 11px;
  color: rgb(203 213 225 / 72%);
  letter-spacing: 0.03em;
  white-space: nowrap;
}
.switcher-right {
  display: flex;
  gap: 6px;
}
.proto-btn {
  padding: 5px 12px;
  font-size: 11px;
  font-weight: 500;
  color: rgb(219 234 254 / 68%);
  cursor: pointer;
  background: rgb(255 255 255 / 4%);
  border: 1px solid rgb(255 255 255 / 8%);
  border-radius: 6px;
  transition: all 0.15s ease;
}
.proto-btn:hover {
  color: var(--uvp-brand);
  background: var(--uvp-brand-soft);
  border-color: color-mix(in srgb, var(--uvp-brand) 32%, transparent);
}
.proto-btn.active {
  color: var(--uvp-brand);
  background: var(--uvp-brand-soft);
  border-color: color-mix(in srgb, var(--uvp-brand) 42%, transparent);
}
.protocol-option {
  box-sizing: border-box;
  display: grid;
  grid-template-columns: max-content minmax(0, 1fr) 28px;
  gap: 8px;
  align-items: center;
  width: min(600px, calc(100vw - 80px));
  padding: 2px 0;
}
.protocol-option strong {
  min-width: 0;
  font-size: 11px;
  font-weight: 500;
  line-height: 1.4;
}
.protocol-url {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  font-family: var(--uvp-font-mono, ui-monospace, SFMono-Regular, Menlo, monospace);
  font-size: 11px;
  line-height: 1.4;
  color: var(--uvp-text-secondary);
  white-space: nowrap;
}
.protocol-copy-btn {
  position: relative;
  z-index: 1;
  display: inline-grid;
  place-items: center;
  width: 28px;
  height: 28px;
  padding: 0;
  color: var(--uvp-text-tertiary);
  cursor: pointer;
  background: transparent;
  border: 0;
  border-radius: 5px;
  transition:
    color 0.15s ease,
    background 0.15s ease;
}
.protocol-copy-btn:hover {
  color: var(--uvp-brand);
  background: var(--uvp-brand-soft);
}
</style>
