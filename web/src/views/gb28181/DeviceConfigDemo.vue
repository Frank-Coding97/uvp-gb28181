<script setup lang="ts">
/**
 * DeviceConfigDemo - 设备配置中心的形态预览页（无菜单/无权限依赖）
 *
 * 直接访问 `/device-config-demo` 即可看效果：抽屉默认打开，关掉后可改
 * 「在线 / 协议版本 / 通道」再打开，用来对照不同状态下的呈现。
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
    margin: 80px auto 0;
    padding: 22px 24px;
    background: #fff;
    border: 1px solid #e2e8f0;
    border-radius: 8px;

    h2 {
        margin: 0 0 6px;
        color: #1f2937;
        font-size: 15px;
        font-weight: 500;
    }

    p {
        margin: 0 0 16px;
        color: #6b7280;
        font-size: 12px;
        line-height: 1.6;
    }
}

.dcd-controls {
    display: flex;
    flex-wrap: wrap;
    gap: 14px;
    margin-bottom: 18px;

    label {
        display: inline-flex;
        align-items: center;
        gap: 6px;
        color: #4b5563;
        font-size: 12px;
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
    color: #fff;
    font-size: 12px;
    background: #2563eb;
    border: none;
    border-radius: 4px;
    cursor: pointer;

    &:hover {
        background: #1d4ed8;
    }
}
</style>
