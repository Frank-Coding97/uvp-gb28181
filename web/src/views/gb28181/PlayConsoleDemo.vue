<script setup lang="ts">
/**
 * PlayConsoleDemo - 播放控制台静态原型演示页
 *
 * 访问方式: http://localhost:5177/play-console-demo
 * 作用: 独立预览 PlayConsole 静态原型,方便老板评审 UI/UX
 * 数据: 全 mock,不发任何真实请求
 */
import { ref } from "vue";
import PlayConsole from "./components/PlayConsole.vue";

const visible = ref(true);

// mock 通道数据 —— 模拟一个真实的 GB28181 通道
const mockChannel = ref({
  id: 1,
  channelId: "34020000001320000001",
  deviceId: "34020000001320000001",
  name: "前门摄像头",
  alias: "大门监控",
  manufacturer: "海康威视",
  model: "DS-2CD3T87WD-L",
  ptzType: 1,        // 1=球机, 支持完整云台
  status: 1,         // 1=在线
  streamTransport: "TCP-Passive"
});

// mock 通道预设列表(切换预览)
const mockChannels = [
  {
    id: 1,
    channelId: "34020000001320000001",
    deviceId: "34020000001320000001",
    name: "前门摄像头",
    alias: "大门监控",
    manufacturer: "海康威视",
    model: "DS-2CD3T87WD-L",
    ptzType: 1,
    status: 1,
    streamTransport: "TCP-Passive"
  },
  {
    id: 2,
    channelId: "34020000001320000002",
    deviceId: "34020000001320000002",
    name: "停车场监控",
    alias: "停车场枪机",
    manufacturer: "大华股份",
    model: "IPC-HFW5442E-Z",
    ptzType: 0,        // 0=固定,不支持云台
    status: 1,
    streamTransport: "UDP"
  },
  {
    id: 3,
    channelId: "34020000001320000003",
    deviceId: "34020000001320000003",
    name: "值班室内部",
    alias: "",
    manufacturer: "宇视科技",
    model: "IPC322L",
    ptzType: 0,
    status: 0,         // 0=离线
    streamTransport: "TCP-Active"
  }
];

const currentChannelIndex = ref(0);

function reopenConsole() {
  visible.value = false;
  setTimeout(() => {
    visible.value = true;
  }, 200);
}

function switchChannel(index: number) {
  currentChannelIndex.value = index;
  mockChannel.value = mockChannels[index];
  reopenConsole();
}
</script>

<template>
  <div class="demo-wrapper">
    <!-- 顶部工具栏:切换通道预览不同场景 -->
    <div class="demo-toolbar">
      <div class="toolbar-title">
        <span class="badge">DEMO</span>
        <strong>播放控制台 · 静态原型演示</strong>
        <small>所有数据均为 mock,用于评审 UI/UX 设计</small>
      </div>
      <div class="toolbar-actions">
        <button
          v-for="(ch, idx) in mockChannels"
          :key="ch.id"
          class="ch-btn"
          :class="{ active: currentChannelIndex === idx }"
          @click="switchChannel(idx)"
        >
          <span class="ch-name">{{ ch.alias || ch.name }}</span>
          <span class="ch-tag" :class="{ offline: ch.status !== 1 }">
            {{ ch.status === 1 ? "在线" : "离线" }}
          </span>
          <span class="ch-tag" :class="{ ptz: ch.ptzType === 1 }">
            {{ ch.ptzType === 1 ? "球机" : "固定" }}
          </span>
        </button>
        <button class="reopen-btn" @click="reopenConsole">
          重新打开弹窗
        </button>
      </div>
    </div>

    <!-- PlayConsole 弹窗 -->
    <PlayConsole
      :visible="visible"
      :channel="mockChannel"
      @update:visible="visible = $event"
    />

    <!-- 关闭后的占位提示 -->
    <div v-if="!visible" class="closed-hint">
      <p>弹窗已关闭</p>
      <button class="reopen-btn primary" @click="reopenConsole">重新打开</button>
    </div>
  </div>
</template>

<style scoped>
.demo-wrapper {
  min-height: 100vh;
  padding: 20px;
  background: linear-gradient(180deg, #0f172a 0%, #1e293b 100%);
}

.demo-toolbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  padding: 14px 18px;
  margin-bottom: 20px;
  background: rgba(15, 23, 42, 0.72);
  border: 1px solid rgba(148, 163, 184, 0.16);
  border-radius: 12px;
  backdrop-filter: blur(12px);
}

.toolbar-title {
  display: flex;
  align-items: center;
  gap: 10px;
}

.badge {
  padding: 3px 8px;
  background: linear-gradient(135deg, #6366f1 0%, #ec4899 100%);
  color: #fff;
  border-radius: 4px;
  font-size: 10px;
  font-weight: 700;
  letter-spacing: 0.08em;
}

.toolbar-title strong {
  color: #f1f5f9;
  font-size: 14px;
}

.toolbar-title small {
  color: #94a3b8;
  font-size: 11px;
}

.toolbar-actions {
  display: flex;
  gap: 8px;
  align-items: center;
}

.ch-btn {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  padding: 8px 12px;
  background: rgba(148, 163, 184, 0.08);
  border: 1px solid rgba(148, 163, 184, 0.16);
  border-radius: 8px;
  color: #cbd5e1;
  cursor: pointer;
  font-size: 12px;
  transition: all 0.15s ease;
}

.ch-btn:hover {
  background: rgba(96, 165, 250, 0.12);
  border-color: rgba(96, 165, 250, 0.4);
  color: #e0e7ff;
}

.ch-btn.active {
  background: rgba(96, 165, 250, 0.2);
  border-color: rgba(96, 165, 250, 0.6);
  color: #dbeafe;
  box-shadow: 0 0 0 3px rgba(96, 165, 250, 0.1);
}

.ch-name {
  font-weight: 500;
}

.ch-tag {
  padding: 1px 6px;
  background: rgba(148, 163, 184, 0.14);
  border-radius: 4px;
  font-size: 10px;
  color: #94a3b8;
}

.ch-tag:first-of-type {
  color: #34d399;
  background: rgba(52, 211, 153, 0.14);
}

.ch-tag.offline {
  color: #f87171;
  background: rgba(248, 113, 113, 0.14);
}

.ch-tag.ptz {
  color: #a78bfa;
  background: rgba(167, 139, 250, 0.14);
}

.reopen-btn {
  padding: 8px 14px;
  background: rgba(96, 165, 250, 0.14);
  border: 1px solid rgba(96, 165, 250, 0.36);
  border-radius: 8px;
  color: #93c5fd;
  cursor: pointer;
  font-size: 12px;
  font-weight: 500;
  transition: all 0.15s ease;
}

.reopen-btn:hover {
  background: rgba(96, 165, 250, 0.24);
  color: #dbeafe;
}

.reopen-btn.primary {
  background: #3b82f6;
  border-color: #3b82f6;
  color: #fff;
}

.reopen-btn.primary:hover {
  background: #2563eb;
}

.closed-hint {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 16px;
  min-height: 60vh;
  color: #94a3b8;
}

.closed-hint p {
  font-size: 14px;
}
</style>
