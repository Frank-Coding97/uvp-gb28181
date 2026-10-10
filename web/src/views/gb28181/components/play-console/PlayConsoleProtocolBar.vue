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
        class="protocol-select"
        :model-value="protocol"
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
            <a-button
              v-if="canSharePlayback"
              type="text"
              size="mini"
              shape="square"
              html-type="button"
              class="protocol-copy-btn"
              :title="`复制 ${option.label} 地址`"
              :aria-label="`复制 ${option.label} 地址`"
              @mousedown.stop.prevent
              @click.stop="emit('copyProtocol', option.value)"
            >
              <Copy :size="13" />
            </a-button>
          </div>
        </a-option>
      </a-select>
    </div>
    <div class="switcher-right">
      <a-button
        v-for="option in shortcutOptions"
        :key="option.value"
        type="secondary"
        size="mini"
        html-type="button"
        class="proto-btn"
        :class="{ active: protocol === option.value }"
        :disabled="!protocolUrls[option.value]"
        @click="emit('switchProtocol', option.value)"
      >
        {{ option.label }}
      </a-button>
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
  color: rgb(203 213 225 / 92%);
  letter-spacing: 0.03em;
  white-space: nowrap;
}

/**
 * 深色控制栏里的协议下拉：与右侧 .proto-btn.arco-btn[type="button"] 收敛到同一视觉语言。
 *
 * ⛔ 全局基准（`styles/arco-overrides.scss` L52-65）给 `.arco-select-view` 上了
 *    `--uvp-dialog-control-bg`（亮色 #f8fbff）的底 + 8px 圆角；尺寸则由 Arco 自带的
 *    `.arco-select-view-size-small` 给（实测 28px）。放进这条 `rgb(15 23 42 / 92%)`
 *    深色栏里，它比标签高出一圈（实测 28px vs 24.6px），又是整栏唯一的高亮色块 ⇒
 *    看着突兀（老板 2026-10-03 反馈「是不是高度太高了」）。这里按「栏内控件与标签
 *    同族」局部破例：同高（26px）、同圆角（6px）、玻璃底 + 浅色文字。
 * ⚠️ 别指望 `arco-overrides.scss:232` 的 `.arco-select-size-small{height:32px}` 出力 ——
 *    该类名在当前 Arco 版本里**根本不存在**（真实类名是 `arco-select-view-size-small`，
 *    且与 `.arco-select-view` 在**同一个元素**上、不是后代关系）⇒ 那条是死规则。
 *    本次覆盖因此只需压过 Arco 自带的 28px，`!important` + 三层 class 链稳赢。
 * ⛔ 缩到 118px 不会挤压下拉面板：面板宽度由 `autoFitPopupWidth: false` + option 内
 *    显式的 `width: min(600px, calc(100vw - 80px))` 决定，与触发器宽度无关。
 */
.protocol-switcher .switcher-left :deep(.protocol-select) {
  width: 118px;
}
.protocol-switcher .switcher-left :deep(.arco-select-view) {
  box-sizing: border-box;
  height: 26px !important;
  padding: 0 8px !important;
  background: rgb(255 255 255 / 6%) !important;
  border: 1px solid rgb(255 255 255 / 12%) !important;
  border-radius: 6px !important;
  box-shadow: none !important;
}
.protocol-switcher .switcher-left :deep(.arco-select-view:hover) {
  background: rgb(255 255 255 / 10%) !important;
  border-color: color-mix(in srgb, var(--uvp-brand) 46%, transparent) !important;
}
.protocol-switcher .switcher-left :deep(.arco-select-view-focus),
.protocol-switcher .switcher-left :deep(.arco-select-view.arco-select-view-focus) {
  background: rgb(255 255 255 / 10%) !important;
  border-color: var(--uvp-brand) !important;
  box-shadow: 0 0 0 2px color-mix(in srgb, var(--uvp-brand) 18%, transparent) !important;
}
.protocol-switcher .switcher-left :deep(.arco-select-view-value) {
  font-size: 11px !important;
  font-weight: 500 !important;
  color: rgb(226 236 254 / 92%) !important;
}
.protocol-switcher .switcher-left :deep(.arco-select-view-value-placeholder) {
  color: rgb(148 163 184 / 72%) !important;
}
.protocol-switcher .switcher-left :deep(.arco-select-view-suffix) {
  color: rgb(203 213 225 / 62%) !important;
}
.switcher-right {
  display: flex;
  gap: 6px;
}
.proto-btn.arco-btn[type="button"] {
  box-sizing: border-box;
  display: inline-flex;
  align-items: center;
  height: 26px;
  padding: 0 12px;
  font-size: 11px;
  font-weight: 500;
  color: rgb(219 234 254 / 92%);
  cursor: pointer;
  background: rgb(255 255 255 / 4%);
  border: 1px solid rgb(255 255 255 / 8%);
  border-radius: 6px;
  transition: all 0.15s ease;
}
.proto-btn.arco-btn[type="button"]:hover:not(:disabled) {
  color: var(--uvp-brand);
  background: var(--uvp-brand-soft);
  border-color: color-mix(in srgb, var(--uvp-brand) 32%, transparent);
}
.proto-btn.arco-btn[type="button"].active {
  color: var(--uvp-brand);
  background: var(--uvp-brand-soft);
  border-color: color-mix(in srgb, var(--uvp-brand) 42%, transparent);
}
.proto-btn.arco-btn[type="button"]:disabled {
  color: rgb(148 163 184 / 72%);
  cursor: not-allowed;
  background: rgb(255 255 255 / 4%);
  border-color: rgb(255 255 255 / 8%);
  opacity: 1;
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
.protocol-copy-btn.arco-btn[type="button"] {
  position: relative;
  z-index: 1;
  display: inline-grid;
  place-items: center;
  width: 28px;
  height: 28px;
  padding: 0;
  color: var(--uvp-text-secondary);
  cursor: pointer;
  background: transparent;
  border: 0;
  border-radius: 5px;
  transition:
    color 0.15s ease,
    background 0.15s ease;
}
.protocol-copy-btn.arco-btn[type="button"]:hover {
  color: var(--uvp-brand);
  background: var(--uvp-brand-soft);
}
</style>
