<script setup lang="ts">
/**
 * DeviceConfigDemo - 设备配置中心的形态预览页（无菜单/无权限依赖）
 *
 * 直接访问 `/device-config-demo` 即可看效果：抽屉默认打开，关掉后可改
 * 「在线 / 协议版本 / 通道」再打开，用来对照不同状态下的呈现。
 *
 * ⛔ 本页只免「菜单与按钮权限」，**不免接口**：设备配置族 6 组已接真后端，
 *    所以这里的面板会真的去读 `device-configs`。未登录 / 通道不存在时，
 *    参数区会如实显示"读取失败"或"尚未读取" —— 那是**预期状态**，不是页面坏了。
 *    想只看形态就让它停在"尚未读取"态即可（不要点左栏「读取」）。
 */
import { ref } from "vue";
import DeviceConfigDrawer from "./device-mgmt/DeviceConfigDrawer.vue";

const visible = ref(true);
const online = ref(true);
const version = ref("2022");
const channelId = ref<number | null>(3539);
</script>

<template>
  <div class="dcd-page">
    <div v-if="!visible" class="dcd-panel">
      <h2>设备配置中心 · 形态预览</h2>
      <p>专业客户端形态：左栏预览 / 中栏分组 / 右栏参数（滑杆配数字微调）。</p>
      <p class="dcd-note">分组已全部接入后端：面板会真的读写设备配置。未登录时显示"尚未读取 / 读取失败"属预期。</p>
      <div class="dcd-controls">
        <label>
          <input v-model="online" type="checkbox" />
          设备在线
        </label>
        <label>
          协议版本
          <select v-model="version">
            <option value="2022">2022</option>
            <option value="2016">2016</option>
          </select>
        </label>
        <label>
          通道 ID
          <input v-model.number="channelId" type="number" />
        </label>
      </div>
      <button type="button" class="dcd-open" @click="visible = true">打开设备配置</button>
    </div>

    <DeviceConfigDrawer
      v-model:visible="visible"
      device-name="UVP-Sim"
      device-code="37010301021180000007"
      channel-name="前置摄像头"
      :online="online"
      :effective-version="version"
      :channel-id="channelId"
    />
  </div>
</template>

<style scoped lang="scss">
.dcd-page {
  min-height: 100vh;
  padding: 24px;
  background: #f1f5f9;
}

.dcd-panel {
  max-width: 460px;
  padding: 22px 24px;
  margin: 80px auto 0;
  background: #ffffff;
  border: 1px solid #e2e8f0;
  border-radius: 8px;

  h2 {
    margin: 0 0 6px;
    font-size: 15px;
    font-weight: 500;
    color: #1f2937;
  }

  p {
    margin: 0 0 16px;
    font-size: 12px;
    line-height: 1.6;
    color: #6b7280;
  }

  .dcd-note {
    padding: 7px 9px;
    margin-bottom: 16px;
    color: #b45309;
    background: #fffbeb;
    border: 1px dashed #fcd34d;
    border-radius: 4px;
  }
}

.dcd-controls {
  display: flex;
  flex-wrap: wrap;
  gap: 14px;
  margin-bottom: 18px;

  label {
    display: inline-flex;
    gap: 6px;
    align-items: center;
    font-size: 12px;
    color: #4b5563;
  }

  input[type="number"] {
    width: 86px;
    height: 26px;
    padding: 0 6px;
    border: 1px solid #dbe4f0;
    border-radius: 4px;
  }

  select {
    height: 26px;
    border: 1px solid #dbe4f0;
    border-radius: 4px;
  }
}

.dcd-open {
  height: 30px;
  padding: 0 16px;
  font-size: 12px;
  color: #ffffff;
  cursor: pointer;
  background: #2563eb;
  border: none;
  border-radius: 4px;

  &:hover {
    background: #1d4ed8;
  }
}
</style>
