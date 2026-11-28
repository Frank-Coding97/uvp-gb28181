<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { Message, Modal } from "@arco-design/web-vue";
import { Activity, Bell, CalendarDays, Camera, HardDrive, Settings2, Siren } from "@lucide/vue";
import DeviceConfigDrawer from "./DeviceConfigDrawer.vue";
import DeviceStatusFactsPanel from "./DeviceStatusFactsPanel.vue";
import SnapshotConfigPanel from "./SnapshotConfigPanel.vue";
import StorageCardStatusPanel from "./StorageCardStatusPanel.vue";
import type { FactChannelOption } from "./deviceFactChannel";

const props = defineProps<{
  channelId: number;
  channelName: string;
  deviceCode: string;
  online: boolean;
  channelOptions: FactChannelOption[];
  canRead: boolean;
  canApply: boolean;
  canSnapshot: boolean;
  canFormat: boolean;
}>();

const CONFIG_GROUPS = ["record-plan", "alarm-record", "alarm-report", "basic"];
const sections = [
  {
    label: "录像",
    items: [
      {
        key: "record-plan",
        label: "录像计划",
        icon: CalendarDays,
        description: "设置录像码流与每周录像时段。修改后需下发，结果以设备回读为准。"
      },
      { key: "alarm-record", label: "报警录像", icon: Siren, description: "设置报警触发后的录像规则与时间安排。" }
    ]
  },
  {
    label: "报警",
    items: [{ key: "alarm-report", label: "报警上报", icon: Bell, description: "设置设备向平台上报报警的规则。" }]
  },
  {
    label: "抓拍",
    items: [
      { key: "snapshot", label: "图像抓拍", icon: Camera, description: "发起一次抓拍任务，按设定张数与间隔接收设备上传的图片。" }
    ]
  },
  {
    label: "状态与维护",
    items: [
      {
        key: "status",
        label: "设备状态",
        icon: Activity,
        description: "查看设备最近上报的运行状态与报警事实，可主动向设备查询。"
      },
      { key: "storage", label: "存储卡", icon: HardDrive, description: "查询存储容量与运行状态，按需维护存储卡。" },
      { key: "basic", label: "基本参数", icon: Settings2, description: "查看与修改设备返回的基本参数。" }
    ]
  }
];
const activeKey = ref(props.canRead ? "record-plan" : props.canSnapshot ? "snapshot" : "status");
const activeItem = computed(() => sections.flatMap(section => section.items).find(item => item.key === activeKey.value)!);
const isConfig = computed(() => CONFIG_GROUPS.includes(activeKey.value));
const configKey = ref("record-plan");
const snapshotVisited = ref(false);
const drawer = ref<InstanceType<typeof DeviceConfigDrawer> | null>(null);
watch(
  activeKey,
  key => {
    if (CONFIG_GROUPS.includes(key)) configKey.value = key;
    if (key === "snapshot") snapshotVisited.value = true;
  },
  { immediate: true }
);

const dirtyCount = computed(() => drawer.value?.dirtyCount ?? 0);
const busy = computed(() => drawer.value?.busy ?? false);
let leaveConfirmation: Promise<boolean> | null = null;
function beforeLeave(): Promise<boolean> {
  if (leaveConfirmation) return leaveConfirmation;
  if (busy.value) {
    Message.warning("正在下发配置，请稍后再切换或关闭");
    return Promise.resolve(false);
  }
  if (!dirtyCount.value) return Promise.resolve(true);
  leaveConfirmation = new Promise<boolean>(resolve => {
    Modal.confirm({
      title: "放弃未下发的修改？",
      content: `当前通道有 ${dirtyCount.value} 项修改尚未下发。离开后这些修改将丢失。`,
      okText: "放弃修改并离开",
      cancelText: "继续编辑",
      onOk: () => resolve(true),
      onCancel: () => resolve(false),
      onClose: () => resolve(false)
    });
  }).finally(() => {
    leaveConfirmation = null;
  });
  return leaveConfirmation;
}

defineExpose({ beforeLeave, dirtyCount, busy });
</script>

<template>
  <div class="channel-workspace">
    <nav class="channel-workspace-nav" aria-label="通道配置功能">
      <div v-for="section in sections" :key="section.label" class="channel-workspace-section">
        <span class="channel-workspace-section-label">{{ section.label }}</span>
        <button
          v-for="item in section.items"
          :key="item.key"
          type="button"
          class="channel-workspace-link"
          :class="{ 'is-active': activeKey === item.key }"
          :aria-current="activeKey === item.key ? 'page' : undefined"
          :disabled="item.key === 'snapshot' ? !canSnapshot : !canRead"
          :data-testid="`channel-nav-${item.key}`"
          @click="activeKey = item.key"
        >
          <component :is="item.icon" :size="15" />
          <span>{{ item.label }}</span>
          <span
            v-if="drawer?.dirtyGroups.includes(item.key)"
            class="channel-workspace-draft"
            title="有未下发的修改"
            aria-label="有未下发的修改"
          />
        </button>
      </div>
      <p class="channel-workspace-nav-note">{{ dirtyCount ? `${dirtyCount} 项修改未下发` : "配置以设备回读结果为准" }}</p>
    </nav>
    <section class="channel-workspace-main" :aria-label="activeItem.label">
      <header class="channel-workspace-heading">
        <div class="channel-workspace-heading-title">
          <h3>{{ activeItem.label }}</h3>
          <span v-if="isConfig && !canApply" class="channel-workspace-readonly">{{ online ? "只读" : "通道离线" }}</span>
        </div>
        <p>{{ activeItem.description }}</p>
      </header>
      <div v-if="canRead" v-show="isConfig" class="channel-workspace-editor">
        <DeviceConfigDrawer
          ref="drawer"
          embedded
          config-only
          nav-side
          external-navigation
          :visible="true"
          :group-keys="CONFIG_GROUPS"
          :active-group-key="configKey"
          :device-code="deviceCode"
          :online="online"
          :channel-id="channelId"
          :channel-name="channelName"
          :can-read="canRead"
          :can-apply="canApply && online"
        />
      </div>
      <div v-show="!isConfig" class="channel-workspace-facts">
        <SnapshotConfigPanel
          v-if="snapshotVisited && canSnapshot"
          v-show="activeKey === 'snapshot'"
          :channel-id="channelId"
          :channel-options="channelOptions"
          :show-picker="false"
          :can-snapshot="canSnapshot"
        />
        <DeviceStatusFactsPanel
          v-if="activeKey === 'status' && canRead"
          :channel-id="channelId"
          :channel-options="channelOptions"
          :active="true"
          :show-picker="false"
          :can-view="canRead"
        />
        <StorageCardStatusPanel
          v-if="activeKey === 'storage' && canRead"
          :channel-id="channelId"
          :channel-options="channelOptions"
          :active="true"
          :show-picker="false"
          :can-view="canRead"
          :can-format="canFormat"
          :device-label="deviceCode"
        />
      </div>
    </section>
  </div>
</template>

<style scoped lang="scss">
.channel-workspace {
  display: grid;
  flex: 1;
  grid-template-columns: 172px minmax(0, 1fr);
  min-height: 0;
  color: var(--uvp-text-primary);
}
.channel-workspace-nav {
  display: flex;
  flex-direction: column;
  gap: 16px;
  padding: 18px 12px 12px;
  overflow-y: auto;
  background: var(--uvp-panel-bg);
  border-right: 1px solid var(--uvp-panel-border);
}
.channel-workspace-section {
  display: grid;
  gap: 4px;
}
.channel-workspace-section-label {
  padding: 0 10px 4px;
  font-size: 11.5px;
  color: var(--uvp-text-tertiary);
}
.channel-workspace-link {
  display: flex;
  gap: 9px;
  align-items: center;
  min-height: 38px;
  padding: 8px 10px;
  font: inherit;
  font-size: 12.5px;
  color: var(--uvp-text-secondary);
  text-align: left;
  cursor: pointer;
  background: transparent;
  border: 1px solid transparent;
  border-radius: 6px;
  transition: background-color 120ms ease;
}
.channel-workspace-link:hover:not(:disabled),
.channel-workspace-link.is-active {
  color: var(--uvp-brand);
  background: var(--uvp-brand-soft, rgb(59 130 246 / 10%));
}
.channel-workspace-link.is-active {
  font-weight: 600;
}
.channel-workspace-link:focus-visible {
  outline: 2px solid var(--uvp-brand);
  outline-offset: 2px;
}
.channel-workspace-link:disabled {
  cursor: not-allowed;
  opacity: 0.45;
}
.channel-workspace-draft {
  width: 5px;
  height: 5px;
  margin-left: auto;
  background: var(--uvp-warning);
  border-radius: 50%;
}
.channel-workspace-nav-note {
  padding-top: 16px;
  margin: auto 6px 0;
  font-size: 11px;
  line-height: 1.6;
  color: var(--uvp-text-tertiary);
}
.channel-workspace-main {
  display: flex;
  flex-direction: column;
  min-width: 0;
  min-height: 0;
}
.channel-workspace-heading {
  flex: none;
  padding: 20px 24px 18px;
  border-bottom: 1px solid var(--uvp-panel-border);
}
.channel-workspace-heading-title {
  display: flex;
  gap: 10px;
  align-items: center;
}
.channel-workspace-heading h3 {
  margin: 0;
  font-size: 16px;
  font-weight: 600;
}
.channel-workspace-heading p {
  margin: 8px 0 0;
  font-size: 12.5px;
  line-height: 1.6;
  color: var(--uvp-text-tertiary);
}
.channel-workspace-readonly {
  font-size: 11px;
  color: var(--uvp-text-tertiary);
}

/* ⛔⛔ 编辑区必须把高度**传到** DeviceConfigDrawer 里，否则底栏会浮在半截。
 *   链条：`.channel-config-shell`（定高 min(680px, 100dvh-160px)）→ `.channel-workspace`（flex:1）
 *   → `.channel-workspace-main`（flex 列）→ `.channel-workspace-editor`（flex:1，高度确定）
 *   → `.dcg-host--embedded`（`display:block`，**height 是 auto**）
 *   → `.dcg-window--embedded`（`height:100%`）。
 *   最后那一跳的百分比是拿**父级高度属性**算的，而它是 auto ⇒ `100%` 退化成 auto
 *   ⇒ 面板塌成"内容有多高就多高"，`.dcg-actions` 跟着停在内容下面，底下一大片空白
 *   （2026-11-03 截图即此：动作条悬在对话框腰上，下发键离窗口底边还有 400px）。
 *   给 host 一个确定的 flex 高度就闭合了，与上一级用的是同一个机制。
 * ⚠️ 只在这里加，**不要**去改 `.dcg-host--embedded`：播放控制台侧栏里那份是"内容撑高"，
 *   改成 100% 会把那边的滚动边界一起挪走。 */
.channel-workspace-editor {
  display: flex;
  flex: 1;
  flex-direction: column;
  min-height: 0;
}
.channel-workspace-editor :deep(.dcg-host) {
  flex: 1 1 auto;
  min-height: 0;
}
.channel-workspace-facts {
  flex: 1;
  min-height: 0;
  padding: 18px 22px;
  overflow-y: auto;
}

@media (width <= 700px) {
  .channel-workspace {
    grid-template-columns: 136px minmax(0, 1fr);
  }
  .channel-workspace-nav {
    padding: 12px 8px;
  }
  .channel-workspace-heading {
    padding: 14px;
  }
  .channel-workspace-facts {
    padding: 14px;
  }
}
</style>
