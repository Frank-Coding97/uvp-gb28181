<script setup lang="ts">
/**
 * DeviceListRow — 设备列表行(C3)
 *
 * 视觉对照 mockup list.html(plan §5):
 * - col-check    | 复选框
 * - col-thumb    | 设备代表通道快照(取主挂载,无则厂商占位)
 * - col-name     | 设备名 + 国标编码(双行)
 * - col-vendor   | 厂商 + 型号
 * - col-addr     | IP : 端口
 * - col-channels | 通道在线 / 总数 胶囊
 * - col-online   | OnlineRing
 * - col-heart    | 最近心跳(相对时间)
 * - col-action   | hover 才出现的快捷操作按钮
 *
 * 离线设备:整行 grayscale 滤镜 + 灰色文字
 * anomaly:橙色左侧描边
 */
import { computed } from "vue";
import SnapThumb from "../shared/SnapThumb.vue";
import OnlineRing from "../shared/OnlineRing.vue";
import StatusDot from "../shared/StatusDot.vue";
import type { DeviceVO } from "../../api/device";

interface Props {
  device: DeviceVO;
  selected?: boolean;
}

const props = withDefaults(defineProps<Props>(), { selected: false });
const emit = defineEmits<{
  (e: "toggle-select", id: number): void;
  (e: "open", id: number): void;
}>();

const heartbeatLabel = computed(() => {
  const ts = props.device.keepaliveTime;
  if (!ts) return "—";
  const t = new Date(ts);
  const diff = Math.floor((Date.now() - t.getTime()) / 1000);
  if (diff < 60) return `${diff}s 前`;
  if (diff < 3600) return `${Math.floor(diff / 60)}m 前`;
  if (diff < 86400) return `${Math.floor(diff / 3600)}h 前`;
  return t.toLocaleDateString();
});

const isOnline = computed(() => props.device.online);
const channelTotal = computed(() => props.device.channelCount || 0);
const channelOnline = computed(() => props.device.channelOnlineCount || 0);
const channelOffline = computed(() => Math.max(0, channelTotal.value - channelOnline.value));

function onRowClick() {
  emit("open", props.device.id);
}

function onCheck(e: Event) {
  e.stopPropagation();
  emit("toggle-select", props.device.id);
}
</script>

<template>
  <tr
    :class="['device-row', { selected, offline: !isOnline }]"
    @click="onRowClick"
  >
    <td class="col-check">
      <input type="checkbox" :checked="selected" @click="onCheck" />
    </td>
    <td class="col-thumb">
      <SnapThumb :vendor-hint="device.manufacturer" :online="isOnline" />
    </td>
    <td class="col-name">
      <div class="name-cell">
        <span class="pri">
          <StatusDot :status="isOnline ? 'online' : 'offline'" />
          {{ device.name || device.deviceId }}
        </span>
        <span class="sec dm-mono">{{ device.deviceId }}</span>
      </div>
    </td>
    <td class="col-vendor">
      <div class="vendor-cell">
        <span class="v">{{ device.manufacturer || "—" }}</span>
        <span v-if="device.model" class="m">{{ device.model }}</span>
      </div>
    </td>
    <td class="col-addr">
      <span class="addr-cell dm-mono">
        {{ device.ip || "—" }}<span v-if="device.port" class="port">:{{ device.port }}</span>
      </span>
    </td>
    <td class="col-channels">
      <span class="channel-pill">
        <span class="online">{{ channelOnline }}</span>
        /
        <span class="total">{{ channelTotal }}</span>
        <span v-if="channelOffline > 0" class="off">↓{{ channelOffline }}</span>
      </span>
    </td>
    <td class="col-online">
      <OnlineRing :value="device.onlineRate || 0" />
    </td>
    <td class="col-heart">
      <span class="heart-cell">{{ heartbeatLabel }}</span>
    </td>
    <td class="col-action">
      <!-- 行 hover 才显;C4 后期接 BulkActionBar -->
      <button class="row-act" type="button" title="详情">详情</button>
    </td>
  </tr>
</template>

<style lang="scss" scoped>
.device-row {
  cursor: pointer;
  transition: background var(--duration-fast) var(--ease-out),
    transform var(--duration-fast) var(--ease-out);

  &:hover {
    background: var(--color-bg-3);
  }

  &.selected {
    background: var(--primary-fade-08);
  }

  &.offline {
    .name-cell .pri,
    .vendor-cell .v {
      color: var(--color-text-3);
    }
    .addr-cell,
    .heart-cell {
      color: var(--color-text-4);
    }
  }

  td {
    padding: var(--space-3) var(--space-3);
    border-bottom: 1px solid var(--color-border-1);
    font-size: var(--font-13);
    color: var(--color-text-2);
    vertical-align: middle;
  }

  .col-check {
    width: 36px;

    input[type="checkbox"] {
      cursor: pointer;
    }
  }

  .col-thumb {
    width: 64px;
  }

  .col-name {
    .name-cell {
      display: flex;
      flex-direction: column;
      gap: 2px;

      .pri {
        display: inline-flex;
        align-items: center;
        gap: var(--space-2);
        color: var(--color-text-1);
        font-weight: 500;
      }

      .sec {
        font-size: 11px;
        color: var(--color-text-4);
      }
    }
  }

  .col-vendor .vendor-cell {
    display: flex;
    flex-direction: column;
    gap: 2px;

    .v {
      color: var(--color-text-2);
    }

    .m {
      font-size: 11px;
      color: var(--color-text-4);
    }
  }

  .col-addr {
    width: 180px;

    .addr-cell {
      font-size: 12px;
      color: var(--color-text-3);
    }

    .port {
      color: var(--color-text-4);
    }
  }

  .col-channels {
    width: 100px;

    .channel-pill {
      display: inline-flex;
      align-items: center;
      gap: 4px;
      padding: 0 var(--space-2);
      height: 22px;
      border-radius: var(--dm-radius-pill);
      background: var(--color-bg-3);
      font-family: var(--font-mono);
      font-size: 12px;

      .online {
        color: var(--status-online);
        font-weight: 600;
      }

      .total {
        color: var(--color-text-3);
      }

      .off {
        color: var(--status-offline);
        font-size: 10px;
        margin-left: 2px;
      }
    }
  }

  .col-online {
    width: 60px;
  }

  .col-heart {
    width: 100px;
    color: var(--color-text-3);
    font-size: 12px;
  }

  .col-action {
    width: 80px;
    text-align: right;

    .row-act {
      visibility: hidden;
      padding: 4px 10px;
      background: var(--color-bg-3);
      border: 1px solid var(--color-border-2);
      border-radius: var(--dm-radius-1);
      color: var(--color-text-2);
      font-size: 12px;
      cursor: pointer;
      transition: all var(--duration-fast) var(--ease-out);

      &:hover {
        background: var(--primary-fade-16);
        color: var(--primary-5);
        border-color: var(--primary-6);
      }
    }
  }

  &:hover .col-action .row-act {
    visibility: visible;
  }
}
</style>
