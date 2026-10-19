<template>
  <div class="upstream-subscriptions" aria-label="上级对本平台的订阅">
    <div class="subscription-summary">
      <div v-for="item in types" :key="item.key" class="subscription-type">
        <span>{{ item.label }}</span>
        <a-tag size="small" :color="presentation(item.key).color">{{ presentation(item.key).label }}</a-tag>
      </div>
    </div>
    <details v-if="sessions.length" class="subscription-details">
      <summary>订阅明细（{{ sessions.length }}）</summary>
      <p>仅显示当前有效会话；已订阅不代表每条通知均送达。</p>
      <dl v-for="(session, index) in sessions" :key="index">
        <dt>类型 / 目标</dt>
        <dd>
          {{ session.label }} / <code>{{ session.target }}</code>
        </dd>
        <dt>事件</dt>
        <dd>{{ session.event }}</dd>
        <dt>建立时间</dt>
        <dd>{{ formatTime(session.subscribedAt) }}</dd>
        <dt>最近续订</dt>
        <dd>{{ formatTime(session.renewedAt) }}</dd>
        <dt>到期时间</dt>
        <dd>{{ formatTime(session.expiresAt) }}</dd>
        <dt>最近通知</dt>
        <dd>{{ notifyResult(session) }}<br />{{ formatTime(session.lastNotifyAt) }}</dd>
      </dl>
    </details>
  </div>
</template>

<script setup lang="ts">
import { computed } from "vue";
import dayjs from "dayjs";
import type { CascadeSubscriptions, CascadeSubscriptionSession } from "@/api/gb28181";

const props = defineProps<{ subscriptions?: CascadeSubscriptions; stale?: boolean }>();
const types = [
  { key: "catalog", label: "目录" },
  { key: "mobilePosition", label: "位置" },
  { key: "ptzPosition", label: "PTZ 精准" },
  { key: "alarm", label: "告警" }
] as const;

function presentation(key: keyof CascadeSubscriptions) {
  const status = props.stale ? undefined : props.subscriptions?.[key];
  switch (status?.state) {
    case "active":
      return { label: `已订阅（${status.sessions?.length ?? 0}）`, color: "green" };
    case "inactive":
      return { label: "未订阅", color: "gray" };
    case "unsupported":
      return { label: "尚未接通", color: "gray" };
    case "unavailable":
      return { label: "运行时不可用", color: "orange" };
    default:
      return { label: "状态未知", color: "orange" };
  }
}
const sessions = computed(() =>
  props.stale
    ? []
    : types.flatMap(item => {
        const status = props.subscriptions?.[item.key];
        return status?.state === "active" ? (status.sessions ?? []).map(session => ({ ...session, label: item.label })) : [];
      })
);

function formatTime(value?: string) {
  return value ? dayjs(value).format("YYYY-MM-DD HH:mm:ss") : "暂无记录";
}
function notifyResult(session: CascadeSubscriptionSession) {
  const code = session.lastNotifyCode ? `（${session.lastNotifyCode}）` : "";
  if (session.lastNotifyStatus === "success") return `成功${code}`;
  if (session.lastNotifyStatus === "failed") return `失败${code}`;
  return "等待通知";
}
</script>

<style scoped lang="scss">
.subscription-summary {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 6px 10px;
}
.subscription-type {
  display: flex;
  flex-direction: column;
  gap: 3px;
  align-items: flex-start;
  font-size: 12px;
}
.subscription-details {
  margin-top: 8px;
  font-size: 12px;
  summary {
    color: rgb(var(--primary-6));
    cursor: pointer;
  }
  p {
    color: var(--color-text-3);
  }
  dl {
    display: grid;
    gap: 4px;
    padding-top: 8px;
    margin: 10px 0 0;
    border-top: 1px solid var(--color-border-2);
  }
  dt {
    color: var(--color-text-3);
  }
  dd {
    margin: 0;
    overflow-wrap: anywhere;
  }
}
</style>
