<script setup lang="ts">
import { inject } from "vue";
import { Info, Loader2, Plus, Settings, ShieldCheck, X } from "lucide-vue-next";
import ProbeTimelineDialog from "../ProbeTimelineDialog.vue";
import { PLAY_CONSOLE_CONTEXT } from "./playConsoleContext";

const context = inject(PLAY_CONSOLE_CONTEXT) as Record<string, any> | undefined;
if (!context) throw new Error("PlayConsoleDialogs must be rendered inside PlayConsoleLinked");

const {
  presets,
  canDiagnosePlayback,
  probeTimelineDialogVisible,
  probeResult,
  homeSettingsDialogVisible,
  closeHomeSettingsDialog,
  homeSettingsSubmitting,
  homeDraft,
  homeSettingsTouched,
  homePositionCanSave,
  submitHomeSettings,
  homeSettingsActionLabel,
  canSavePtzPreset,
  savePresetDialogVisible,
  presetDraft,
  presetNameError,
  handleSavePresetBeforeOk,
  closeSavePresetDialog,
  presetNameTouched,
  canControlPtzCruise,
  saveCruiseDialogVisible,
  cruiseDraft,
  cruiseDraftTouched,
  canCloseSaveCruiseDialog,
  handleSaveCruiseBeforeOk,
  closeSaveCruiseDialog,
  cruiseDraftError,
  cruiseDraftSubmitError,
  cruiseStopsListEl,
  moveCruiseStop,
  removeCruiseStop,
  addCruiseStop
} = context;
</script>

<template>
  <!-- 帧到达时间线详情:侧栏概览条点击后打开。
                 逐帧散点需要的横向空间比概览条大得多,所以单独给弹窗。 -->
  <ProbeTimelineDialog v-if="canDiagnosePlayback" v-model:visible="probeTimelineDialogVisible" :snapshot="probeResult" />

  <a-modal
    v-if="homeSettingsDialogVisible"
    v-model:visible="homeSettingsDialogVisible"
    title="设置看守位"
    modal-class="uvp-system-dialog home-settings-modal"
    :width="430"
    :footer="false"
    :mask-closable="!homeSettingsSubmitting"
    :closable="!homeSettingsSubmitting"
    :esc-to-close="!homeSettingsSubmitting"
    unmount-on-close
    @cancel="closeHomeSettingsDialog"
    @close="closeHomeSettingsDialog"
  >
    <div class="home-settings-form" data-testid="home-settings-dialog">
      <div class="home-settings-field">
        <label for="home-position-preset">归位预置位 <span>*</span></label>
        <select
          id="home-position-preset"
          v-model.number="homeDraft.presetId"
          data-testid="home-preset"
          :disabled="homeSettingsSubmitting"
          @change="homeSettingsTouched = true"
        >
          <option :value="null">请选择预置位</option>
          <option v-for="p in presets" :key="p.id" :value="p.id">#{{ p.id }} · {{ p.name }}</option>
        </select>
      </div>
      <div class="home-settings-field">
        <label for="home-position-reset-time">无云台操作后 <span>*</span></label>
        <div class="home-settings-time">
          <input
            id="home-position-reset-time"
            v-model.number="homeDraft.resetTime"
            data-testid="home-reset-time"
            type="number"
            min="10"
            max="3600"
            :disabled="homeSettingsSubmitting"
            @input="homeSettingsTouched = true"
          />
          <span>秒自动归位</span>
        </div>
      </div>
      <p class="home-settings-description">连续无云台操作达到指定时间后，设备将自动返回所选预置位。</p>
      <p v-if="homeSettingsTouched && !homePositionCanSave" class="home-settings-error" data-testid="home-validation">
        请选择一个已存在的预置位，等待时间必须是 10 至 3600 秒整数。
      </p>
      <div class="home-settings-actions">
        <button class="btn-ghost sm" :disabled="homeSettingsSubmitting" @click="closeHomeSettingsDialog">取消</button>
        <button
          class="btn-primary sm"
          data-testid="home-dialog-submit"
          :disabled="homeSettingsSubmitting || !homePositionCanSave"
          @click="submitHomeSettings"
        >
          <Loader2 v-if="homeSettingsSubmitting" :size="13" class="spin" />
          <ShieldCheck v-else :size="13" />
          {{ homeSettingsActionLabel }}
        </button>
      </div>
    </div>
  </a-modal>

  <a-modal
    v-if="canSavePtzPreset && savePresetDialogVisible"
    v-model:visible="savePresetDialogVisible"
    title="保存预置位"
    ok-text="保存"
    cancel-text="取消"
    modal-class="uvp-system-dialog preset-save-modal"
    :width="380"
    :mask-closable="false"
    :ok-loading="presetDraft?.submitting || false"
    :on-before-ok="handleSavePresetBeforeOk"
    unmount-on-close
    @cancel="closeSavePresetDialog"
    @close="closeSavePresetDialog"
  >
    <div v-if="presetDraft" class="preset-save-form" data-testid="preset-save-dialog">
      <div class="preset-save-row">
        <label class="preset-save-label">编号</label>
        <span class="preset-save-index">#{{ presetDraft.id }}</span>
      </div>
      <div class="preset-save-row">
        <label class="preset-save-label">名称</label>
        <div class="preset-save-field">
          <a-input
            v-model="presetDraft.name"
            allow-clear
            :max-length="16"
            :placeholder="`预置位 ${presetDraft.id}`"
            :disabled="presetDraft.submitting"
            :error="!!presetNameError"
            data-testid="preset-save-name-input"
            @blur="presetNameTouched = true"
            @press-enter="presetNameTouched = true"
          >
            <template #suffix>
              <span
                class="preset-save-count"
                :class="{ ok: presetDraft.name.trim().length > 0 && presetDraft.name.trim().length <= 16 }"
              >
                {{ presetDraft.name.length }}/16
              </span>
            </template>
          </a-input>
          <p v-if="presetNameError" class="preset-save-error">{{ presetNameError }}</p>
          <p v-else class="preset-save-hint">留空将使用默认名「预置位 {{ presetDraft.id }}」</p>
        </div>
      </div>
    </div>
  </a-modal>

  <a-modal
    v-if="canControlPtzCruise && saveCruiseDialogVisible"
    v-model:visible="saveCruiseDialogVisible"
    title="新建巡航轨迹"
    ok-text="创建并下发"
    cancel-text="取消"
    modal-class="uvp-system-dialog cruise-save-modal"
    :width="480"
    :mask-closable="false"
    :closable="!cruiseDraft?.submitting"
    :esc-to-close="!cruiseDraft?.submitting"
    :cancel-button-props="{ disabled: cruiseDraft?.submitting || false }"
    :ok-loading="cruiseDraft?.submitting || false"
    :on-before-ok="handleSaveCruiseBeforeOk"
    :on-before-cancel="canCloseSaveCruiseDialog"
    unmount-on-close
    @cancel="closeSaveCruiseDialog"
    @close="closeSaveCruiseDialog"
  >
    <div v-if="cruiseDraft" class="cruise-save-form" data-testid="cruise-save-dialog">
      <div class="cruise-save-notice">
        <Info :size="14" />
        <span
          >设备会按下面列出的顺序依次走到每个预置位、各停一会儿,然后循环执行。配置会直接写进设备;部分老设备不回传确认,下发后可用「试运行」核对。</span
        >
      </div>
      <div class="cruise-save-row">
        <label class="cruise-save-label">名称</label>
        <div class="cruise-save-field">
          <a-input
            v-model="cruiseDraft.name"
            allow-clear
            :max-length="32"
            :placeholder="`巡航 ${cruiseDraft.trackId}`"
            :disabled="cruiseDraft.submitting"
            data-testid="cruise-save-name-input"
            @blur="cruiseDraftTouched = true"
          >
            <template #suffix>
              <span
                class="preset-save-count"
                :class="{ ok: cruiseDraft.name.trim().length > 0 && cruiseDraft.name.trim().length <= 32 }"
              >
                {{ cruiseDraft.name.length }}/32
              </span>
            </template>
          </a-input>
          <p class="preset-save-hint">名称仅在本平台显示,不会同步到设备。</p>
        </div>
      </div>
      <div class="cruise-save-row">
        <label class="cruise-save-label">巡航点</label>
        <div class="cruise-save-field">
          <div ref="cruiseStopsListEl" class="cruise-stops-list" data-testid="cruise-stops-list">
            <div v-for="(stop, index) in cruiseDraft.stops" :key="stop.key" class="cruise-stop-row" data-testid="cruise-stop-row">
              <span class="cruise-stop-idx">{{ index + 1 }}</span>
              <a-select
                v-model="stop.presetId"
                :style="{ flex: '1 1 auto', minWidth: '0' }"
                :disabled="cruiseDraft.submitting"
                data-testid="cruise-stop-select"
                placeholder="选择预置位"
              >
                <a-option v-for="p in presets" :key="p.id" :value="p.id">#{{ p.id }} · {{ p.name }}</a-option>
              </a-select>
              <button
                class="cruise-stop-move"
                :disabled="cruiseDraft.submitting || index === 0"
                title="上移"
                aria-label="上移巡航点"
                @click="moveCruiseStop(index, -1)"
              >
                ↑
              </button>
              <button
                class="cruise-stop-move"
                :disabled="cruiseDraft.submitting || index === cruiseDraft.stops.length - 1"
                title="下移"
                aria-label="下移巡航点"
                @click="moveCruiseStop(index, 1)"
              >
                ↓
              </button>
              <button
                class="cruise-stop-del"
                :disabled="cruiseDraft.submitting || cruiseDraft.stops.length <= 1"
                title="删除该巡航点"
                aria-label="删除巡航点"
                @click="removeCruiseStop(index)"
              >
                <X :size="11" />
              </button>
            </div>
          </div>
          <button
            class="cruise-stop-add"
            data-testid="cruise-stop-add-btn"
            :disabled="cruiseDraft.submitting || cruiseDraft.stops.length >= 32"
            @click="addCruiseStop"
          >
            <Plus v-if="cruiseDraft.stops.length < 32" :size="14" />
            <span>{{ cruiseDraft.stops.length >= 32 ? "已达到 32 站上限" : "添加巡航点" }}</span>
            <small v-if="cruiseDraft.stops.length < 32" class="cruise-stop-add-count"
              >还可添加 {{ 32 - cruiseDraft.stops.length }} 个</small
            >
          </button>
          <p class="preset-save-hint">
            已选 {{ cruiseDraft.stops.length }} 个巡航点。列表从上到下就是设备实际走的顺序,可用 ↑ ↓ 调整。
          </p>
        </div>
      </div>
      <div class="cruise-save-row">
        <label class="cruise-save-label">巡航速度</label>
        <div class="cruise-save-field">
          <div class="cruise-param-line">
            <a-input-number
              v-model="cruiseDraft.speed"
              :min="1"
              :max="4095"
              :step="1"
              :style="{ width: '112px' }"
              :disabled="cruiseDraft.submitting || !cruiseDraft.sendSpeed"
              data-testid="cruise-save-speed"
              @blur="cruiseDraftTouched = true"
            />
            <span class="cruise-param-mode">
              <a-switch
                v-model="cruiseDraft.sendSpeed"
                size="small"
                :disabled="cruiseDraft.submitting"
                aria-label="是否下发巡航速度设置"
                data-testid="cruise-send-speed"
              />
              <span>{{ cruiseDraft.sendSpeed ? "下发设置" : "不下发" }}</span>
            </span>
          </div>
          <p class="preset-save-hint">
            {{
              cruiseDraft.sendSpeed
                ? "取值范围 1-4095,整条轨迹共用一个值(协议按组下发,不支持逐点设置)。快慢由设备自己解释,没有统一物理单位,不同厂家同一个数的实际转速可能不同。"
                : "本次不下发速度设置,设备保持当前设置。"
            }}
          </p>
        </div>
      </div>
      <div class="cruise-save-row">
        <label class="cruise-save-label">每站停留</label>
        <div class="cruise-save-field">
          <div class="cruise-param-line">
            <a-input-number
              v-model="cruiseDraft.dwellSec"
              :min="1"
              :max="4095"
              :step="1"
              :style="{ width: '112px' }"
              :disabled="cruiseDraft.submitting || !cruiseDraft.sendDwell"
              data-testid="cruise-save-dwell"
              @blur="cruiseDraftTouched = true"
            />
            <span class="cruise-save-unit">秒</span>
            <span class="cruise-param-mode">
              <a-switch
                v-model="cruiseDraft.sendDwell"
                size="small"
                :disabled="cruiseDraft.submitting"
                aria-label="是否下发巡航停留时间设置"
                data-testid="cruise-send-dwell"
              />
              <span>{{ cruiseDraft.sendDwell ? "下发设置" : "不下发" }}</span>
            </span>
          </div>
          <p class="preset-save-hint">
            {{
              cruiseDraft.sendDwell
                ? "单位是秒,范围 1-4095(最长约 68 分钟)。每个预置位停多久由这一个值决定 —— 整条轨迹共用,不支持逐点设置。"
                : "本次不下发停留时间设置,设备保持当前设置。"
            }}
          </p>
        </div>
      </div>
      <details class="cruise-save-advanced">
        <summary><Settings :size="13" />高级设置</summary>
        <div class="cruise-save-advanced-body">
          <div class="cruise-save-row">
            <label class="cruise-save-label">编号</label>
            <div class="cruise-save-field">
              <a-input-number
                v-model="cruiseDraft.trackId"
                :min="0"
                :max="255"
                :step="1"
                :style="{ width: '96px' }"
                :disabled="cruiseDraft.submitting"
                data-testid="cruise-save-track-id"
                @blur="cruiseDraftTouched = true"
              />
              <p class="preset-save-hint">轨迹编号由平台自动分配,通常无需修改。</p>
            </div>
          </div>
          <div class="cruise-save-row">
            <label class="cruise-save-label">覆盖</label>
            <div class="cruise-save-field">
              <label class="cruise-save-replace">
                <input
                  v-model="cruiseDraft.replaceExisting"
                  type="checkbox"
                  :disabled="cruiseDraft.submitting"
                  data-testid="cruise-save-replace"
                />
                <span>覆盖同编号轨迹</span>
              </label>
              <p class="preset-save-hint">启用后会先清空设备中的同编号轨迹,此操作不可撤销。</p>
            </div>
          </div>
        </div>
      </details>
      <p v-if="cruiseDraftError || cruiseDraftSubmitError" class="preset-save-error" data-testid="cruise-save-error">
        {{ cruiseDraftError || cruiseDraftSubmitError }}
      </p>
    </div>
  </a-modal>
</template>

<style scoped lang="scss">
/* 保存预置位对话框 */
.preset-save-form {
  display: grid;
  gap: 14px;
  padding: 4px 2px 0;
}
.preset-save-row {
  display: grid;
  grid-template-columns: 48px minmax(0, 1fr);
  gap: 12px;
  align-items: start;
}
.preset-save-label {
  padding-top: 6px;
  font-size: 12px;
  color: var(--uvp-text-secondary);
}
.preset-save-index {
  display: inline-flex;
  align-items: center;
  padding: 4px 10px;
  font-family: ui-monospace, Menlo, monospace;
  font-size: 12px;
  font-weight: 600;
  color: var(--uvp-brand);
  background: var(--uvp-brand-soft);
  border-radius: 5px;
}
.preset-save-field {
  display: grid;
  gap: 4px;
}
.preset-save-count {
  font-size: 11px;
  color: var(--uvp-text-tertiary);
}
.preset-save-count.ok {
  font-weight: 600;
  color: #059669;
}
.preset-save-hint {
  margin: 0;
  font-size: 11px;
  color: var(--uvp-text-tertiary);
}
.preset-save-error {
  margin: 0;
  font-size: 11px;
  color: var(--uvp-danger);
}

/* 新建巡航轨迹对话框 */
.cruise-save-form {
  display: grid;
  gap: 14px;
  max-height: calc(100dvh - 180px);
  padding: 4px 6px 2px 2px;
  overflow-y: auto;
  scrollbar-gutter: stable;
  overscroll-behavior: contain;
}
.cruise-save-notice {
  display: flex;
  gap: 8px;
  align-items: flex-start;
  padding: 9px 10px;
  font-size: 11.5px;
  line-height: 1.5;
  color: var(--uvp-text-secondary);
  background: var(--uvp-warning-soft);
  border: 1px solid var(--uvp-warning-border);
  border-radius: 6px;
}
.cruise-save-notice > svg {
  flex: 0 0 auto;
  margin-top: 1px;
  color: var(--uvp-warning);
}
.cruise-save-row {
  display: grid;
  grid-template-columns: 66px minmax(0, 1fr);
  gap: 12px;
  align-items: start;
}
.cruise-save-label {
  padding-top: 6px;
  font-size: 12px;
  color: var(--uvp-text-secondary);
}
.cruise-save-field {
  display: grid;
  gap: 4px;
  min-width: 0;
}
.cruise-param-line {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  align-items: center;
  min-height: 32px;
}
.cruise-param-mode {
  display: inline-flex;
  gap: 6px;
  align-items: center;
  margin-left: auto;
  font-size: 11.5px;
  color: var(--uvp-text-secondary);
  white-space: nowrap;
}
.cruise-save-field > .preset-save-hint {
  line-height: 1.45;
}
.cruise-save-unit {
  font-size: 11.5px;
  color: var(--uvp-text-secondary);
}
.cruise-save-replace {
  display: inline-flex;
  gap: 5px;
  align-items: center;
  font-size: 11.5px;
  color: var(--uvp-text-secondary);
  cursor: pointer;
}
.cruise-save-replace input {
  accent-color: var(--uvp-brand-cyan);
}
.cruise-stops-list {
  display: grid;
  gap: 5px;
  max-height: clamp(168px, 30vh, 260px);
  padding-right: 3px;
  overflow-y: auto;
  scrollbar-gutter: stable;
  overscroll-behavior: contain;
}
.cruise-stop-row {
  display: flex;
  gap: 6px;
  align-items: center;
  padding: 4px 6px;
  background: color-mix(in srgb, var(--uvp-brand-cyan) 3%, var(--uvp-panel-bg));
  border: 1px solid color-mix(in srgb, var(--uvp-brand-cyan) 16%, var(--uvp-panel-border));
  border-radius: 6px;
}
.cruise-stop-idx {
  display: inline-grid;
  place-items: center;
  width: 22px;
  height: 22px;
  font-family: ui-monospace, Menlo, monospace;
  font-size: 11px;
  font-weight: 600;
  color: var(--uvp-brand-cyan);
  background: color-mix(in srgb, var(--uvp-brand-cyan) 10%, transparent);
  border-radius: 50%;
}
.cruise-stop-move,
.cruise-stop-del {
  display: inline-grid;
  place-items: center;
  width: 24px;
  height: 24px;
  font-size: 12px;
  font-weight: 600;
  color: var(--uvp-text-tertiary);
  cursor: pointer;
  background: transparent;
  border: 1px solid transparent;
  border-radius: 5px;
  transition:
    color 0.12s ease,
    background 0.12s ease,
    border-color 0.12s ease;
}
.cruise-stop-move:hover:not(:disabled) {
  color: var(--uvp-brand-cyan);
  background: color-mix(in srgb, var(--uvp-brand-cyan) 10%, transparent);
}
.cruise-stop-del:hover:not(:disabled) {
  color: var(--uvp-danger);
  background: var(--uvp-danger-soft);
  border-color: var(--uvp-danger-border);
}
.cruise-stop-move:disabled,
.cruise-stop-del:disabled {
  cursor: not-allowed;
  opacity: 0.35;
}
.cruise-stop-add {
  display: inline-flex;
  gap: 6px;
  align-items: center;
  justify-content: center;
  width: 100%;
  min-height: 44px;
  padding: 8px 12px;
  margin-top: 3px;
  font-size: 12px;
  font-weight: 600;
  color: var(--uvp-brand);
  cursor: pointer;
  background: var(--uvp-brand-soft);
  border: 1px solid color-mix(in srgb, var(--uvp-brand) 44%, var(--uvp-panel-border));
  border-radius: 6px;
  transition:
    background 0.18s ease,
    border-color 0.18s ease,
    color 0.18s ease;
}
.cruise-stop-add:hover:not(:disabled) {
  color: #ffffff;
  background: var(--uvp-brand);
  border-color: var(--uvp-brand);
}
.cruise-stop-add:focus-visible {
  outline: 2px solid color-mix(in srgb, var(--uvp-brand) 52%, transparent);
  outline-offset: 2px;
}
.cruise-stop-add:disabled {
  cursor: not-allowed;
  opacity: 0.65;
}
.cruise-stop-add-count {
  margin-left: auto;
  font-size: 10px;
  font-weight: 400;
  color: currentColor;
  opacity: 0.72;
}
.cruise-save-advanced {
  padding-top: 8px;
  border-top: 1px solid var(--uvp-panel-border);
}
.cruise-save-advanced > summary {
  display: inline-flex;
  gap: 5px;
  align-items: center;
  font-size: 11.5px;
  font-weight: 600;
  color: var(--uvp-text-secondary);
  cursor: pointer;
}
.cruise-save-advanced > summary::marker {
  display: none;
}
.cruise-save-advanced-body {
  display: grid;
  gap: 12px;
  padding-top: 12px;
}

@media (width <= 560px) {
  .cruise-save-form {
    max-height: calc(100dvh - 210px);
  }
  .cruise-save-row {
    grid-template-columns: minmax(0, 1fr);
    gap: 5px;
  }
  .cruise-save-label {
    padding-top: 0;
  }
}

/* 主区(视频):stage 仅保留语义,子项直接参与外层网格 */

.home-settings-form {
  display: grid;
  gap: 18px;
  padding-top: 4px;
}
.home-settings-field {
  display: grid;
  gap: 7px;
}
.home-settings-field > label {
  font-size: 12px;
  font-weight: 600;
  color: var(--uvp-text-secondary);
}
.home-settings-field > label > span {
  color: var(--uvp-danger);
}
.home-settings-field select,
.home-settings-field input {
  box-sizing: border-box;
  width: 100%;
  height: 36px;
  padding: 0 10px;
  font-size: 12px;
  color: var(--uvp-text-primary);
  outline: none;
  background: var(--uvp-list-toolbar-bg);
  border: 1px solid var(--uvp-panel-border);
  border-radius: 6px;
}
.home-settings-field select:focus,
.home-settings-field input:focus {
  border-color: var(--uvp-brand);
}
.home-settings-time {
  display: grid;
  grid-template-columns: 110px minmax(0, 1fr);
  gap: 10px;
  align-items: center;
}
.home-settings-time > span {
  font-size: 12px;
  color: var(--uvp-text-secondary);
}
.home-settings-description,
.home-settings-error {
  margin: -4px 0 0;
  font-size: 11px;
  line-height: 1.6;
}
.home-settings-description {
  color: var(--uvp-text-tertiary);
}
.home-settings-error {
  color: var(--uvp-danger);
}
.home-settings-actions {
  display: flex;
  gap: 8px;
  justify-content: flex-end;
  padding-top: 2px;
}
.home-settings-actions button {
  display: inline-flex;
  gap: 5px;
  align-items: center;
  justify-content: center;
  min-width: 86px;
}
</style>
