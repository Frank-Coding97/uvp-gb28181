<script setup lang="ts">
import { AlertTriangle, CheckCircle2, Circle, CircleSlash, Home, Info, Loader2, RefreshCcw, Settings } from "lucide-vue-next";
defineProps<{
  presentation: any;
  diagnosticsTitle: string;
  busy: boolean;
  lastConfirmedText: string;
  confirmedValuesText: string;
  confirmedAtText: string;
  outsideRange: boolean;
  noticeText: string;
  hasPresets: boolean;
  supportStatus: string;
  confirmed: any;
  canConfigure: boolean;
  canClose: boolean;
  canRefresh: boolean;
}>();
const emit = defineEmits<{ (e: "configure"): void; (e: "close"): void; (e: "refresh"): void }>();
</script>
<template>
  <section class="linked-section linked-card" data-testid="home-card">
    <div class="section-hd first">
      <span class="section-title"><Home :size="13" />看守位<span class="tag-2022">2022</span></span
      ><span class="home-diagnostics" :title="diagnosticsTitle" data-testid="home-diagnostics"><Info :size="12" /></span>
    </div>
    <div class="home-config" :class="`state-${presentation.tone}`" data-testid="home-status" aria-live="polite" :aria-busy="busy">
      <div class="home-state-row">
        <span class="home-state-icon" data-testid="home-state-icon" :data-icon="presentation.state"
          ><Loader2
            v-if="presentation.state === 'loading' || presentation.state === 'pending'"
            :size="15"
            class="spin" /><CircleSlash v-else-if="presentation.state === 'unsupported'" :size="15" /><CheckCircle2
            v-else-if="presentation.state === 'enabled'"
            :size="15" /><AlertTriangle
            v-else-if="presentation.state === 'error' || presentation.state === 'offline'"
            :size="15" /><Circle v-else :size="15"
        /></span>
        <div class="home-state-copy">
          <strong data-testid="home-phase">{{ presentation.label }}</strong
          ><span v-if="presentation.description">{{ presentation.description }}</span
          ><span v-if="lastConfirmedText" data-testid="home-confirmed-values">{{ lastConfirmedText }}</span
          ><span v-else-if="confirmedValuesText" data-testid="home-confirmed-values">{{ confirmedValuesText }}</span
          ><span v-if="confirmedAtText && !lastConfirmedText" class="home-confirmed-at">{{ confirmedAtText }}</span>
        </div>
      </div>
      <p v-if="outsideRange" class="home-warning" data-testid="home-range-warning">
        设备返回的归位配置不完整，修改后才能再次启用。
      </p>
      <p v-if="noticeText" class="home-error" data-testid="home-notice">{{ noticeText }}</p>
      <p v-if="!hasPresets && supportStatus !== 'unsupported'" class="home-hint" data-testid="home-preset-required">
        请先添加预置位，再配置看守位。
      </p>
      <div class="home-card-actions">
        <button
          v-if="
            presentation.showConfigure &&
            (presentation.state === 'enabled' ||
              presentation.state === 'disabled' ||
              presentation.state === 'unconfigured' ||
              (presentation.state === 'error' && confirmed))
          "
          class="btn-primary sm"
          data-testid="home-configure"
          :disabled="!canConfigure"
          :title="!hasPresets ? '请先添加预置位' : undefined"
          @click="emit('configure')"
        >
          <Settings :size="12" />{{ confirmed?.enabled ? "修改设置" : "配置并启用" }}</button
        ><button
          v-if="confirmed?.enabled"
          class="btn-ghost sm home-close-btn"
          data-testid="home-close"
          :disabled="!canClose"
          @click="emit('close')"
        >
          <CircleSlash :size="12" />关闭</button
        ><button
          v-if="presentation.showQuery"
          class="btn-ghost sm uvp-refresh-btn"
          data-testid="home-refresh"
          :disabled="!canRefresh"
          @click="emit('refresh')"
        >
          <RefreshCcw :size="12" />{{ presentation.queryLabel }}
        </button>
      </div>
    </div>
  </section>
</template>
