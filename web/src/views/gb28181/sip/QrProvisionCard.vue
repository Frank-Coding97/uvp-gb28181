<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from "vue";
import { Message } from "@arco-design/web-vue";
import { Clock, QrCode, RefreshCw, ShieldAlert, Smartphone } from "lucide-vue-next";
import { generateSipQrToken } from "@/api/gb28181";
import { buildQrUrl, formatCountdown, normalizeBaseUrl, validateBaseUrl } from "./qrProvisionState";

const loading = ref(false);
const token = ref("");
const secondsLeft = ref(0);
// 平台访问地址.默认当前站点 origin —— 设备扫码后要用它兑换,必须是设备能访问到的地址.
const baseUrl = ref(normalizeBaseUrl(window.location.origin));
const touched = ref({ baseUrl: false });
let timer: ReturnType<typeof setInterval> | null = null;
// 自动续码失败后的退避重试:已重试次数 + 挂起的定时器.
// 失败后按 5s→10s→20s 退避,最多 3 次,之后停在失效态等手动点击.
const renewAttempts = ref(0);
const renewFailed = ref(false);
let renewTimer: ReturnType<typeof setTimeout> | null = null;
// 卸载守卫:在途请求返回后不得再调度重试
let mounted = false;

// 退避间隔(ms):第 1/2/3 次重试
const RENEW_BACKOFF_MS = [5000, 10000, 20000];

const baseUrlError = computed(() => (touched.value.baseUrl ? validateBaseUrl(baseUrl.value) : ""));
const baseUrlValid = computed(() => validateBaseUrl(baseUrl.value) === "");
const expired = computed(() => Boolean(token.value) && secondsLeft.value <= 0);
// 校验不过就不出码 —— 宁可不出码,不能出一个扫了会失败的码.
const qrUrl = computed(() => (token.value && baseUrlValid.value ? buildQrUrl(baseUrl.value, token.value) : ""));
const showQr = computed(() => Boolean(qrUrl.value) && !expired.value);
const countdownText = computed(() => formatCountdown(secondsLeft.value));

function stopTimer() {
    if (timer !== null) {
        clearInterval(timer);
        timer = null;
    }
}

function stopRenewTimer() {
    if (renewTimer !== null) {
        clearTimeout(renewTimer);
        renewTimer = null;
    }
}

// 倒计时以响应到达时刻起算 expiresInSeconds,不用绝对时间戳 —— 免受客户端时钟偏移影响.
function startCountdown(expiresInSeconds: number) {
    stopTimer();
    secondsLeft.value = Math.max(0, expiresInSeconds);
    timer = setInterval(() => {
        secondsLeft.value = Math.max(0, secondsLeft.value - 1);
        if (secondsLeft.value === 0) {
            stopTimer();
            // 二维码过期后自动续码,省去手动操作;地址不合法时停在失效态等用户修正.
            if (baseUrlValid.value) void autoRenew();
        }
    }, 1000);
}

// 自动续码:失败后按退避策略重试,避免一次网络抖动就把页面停在失效态.
async function autoRenew() {
    const ok = await generate(true);
    if (!mounted) return; // 卸载后在途请求返回,不再调度任何状态更新或重试
    if (ok) {
        renewAttempts.value = 0;
        renewFailed.value = false;
        return;
    }
    renewFailed.value = true;
    if (renewAttempts.value >= RENEW_BACKOFF_MS.length) return;
    const delayMs = RENEW_BACKOFF_MS[renewAttempts.value];
    renewAttempts.value += 1;
    renewTimer = setTimeout(() => {
        renewTimer = null;
        if (!mounted) return;
        void autoRenew();
    }, delayMs);
}

async function generate(silent = false): Promise<boolean> {
    touched.value.baseUrl = true;
    // 手动生成时取消挂起的自动续码重试,避免新旧请求竞争
    if (!silent) {
        stopRenewTimer();
        renewAttempts.value = 0;
        renewFailed.value = false;
    }
    if (!baseUrlValid.value) {
        if (!silent) Message.warning(validateBaseUrl(baseUrl.value));
        return false;
    }
    loading.value = true;
    try {
        const res = await generateSipQrToken();
        if (res.code !== 0) throw new Error(res.message || "生成接入二维码失败");
        token.value = res.data.token;
        startCountdown(res.data.expiresInSeconds);
        return true;
    } catch (error: any) {
        if (!silent) Message.error(error?.message || "生成接入二维码失败");
        return false;
    } finally {
        loading.value = false;
    }
}

async function copyUrl() {
    if (!qrUrl.value) return;
    try {
        await navigator.clipboard.writeText(qrUrl.value);
        Message.success("已复制二维码链接");
    } catch {
        Message.warning("复制失败,请手动选中");
    }
}

onUnmounted(() => {
    mounted = false;
    stopTimer();
    stopRenewTimer();
});

// 内嵌在页面里,进入即出码;过期后自动续码.
onMounted(() => {
    mounted = true;
    void generate();
});
</script>

<template>
    <section class="qr-card">
        <header class="qr-card__header">
            <span class="qr-card__icon"><QrCode :size="15" /></span>
            <h4>扫码接入</h4>
            <span class="qr-card__header-hint">设备扫码后自动填入接入信息</span>
        </header>

        <div class="qr-note">
            <Smartphone :size="14" />
            <span>需配合 UVP 国标 28181 移动端国标模拟器扫码接入</span>
        </div>

        <a-form layout="vertical">
            <a-form-item
                label="平台访问地址"
                required
                :validate-status="baseUrlError ? 'error' : ''"
                :help="baseUrlError || '设备需要能访问到这个地址,跨网段时请改成设备侧可达的地址。'"
            >
                <a-input
                    v-model="baseUrl"
                    placeholder="http://192.168.1.10:8280"
                    allow-clear
                    @blur="touched.baseUrl = true"
                />
            </a-form-item>
        </a-form>

        <div class="qr-stage">
            <!-- :key 必须有: SQrcodeDraw 只在 onMounted 生成,不 watch text.
                 token 是异步拿到的,没有 :key 会渲染空白码. -->
            <div v-if="showQr" class="qr-canvas">
                <SQrcodeDraw :key="qrUrl" :text="qrUrl" :options="{ width: 220, margin: 1 }" />
            </div>
            <div v-else class="qr-placeholder" :class="{ 'is-expired': expired }">
                <span v-if="expired && renewFailed">二维码已失效,自动续码未成功</span>
                <span v-else-if="expired">二维码已失效</span>
                <span v-else-if="loading">正在生成…</span>
                <span v-else-if="!baseUrlValid">请先填写合法的平台访问地址</span>
                <span v-else>点击下方按钮生成二维码</span>
            </div>

            <div class="qr-meta">
                <Clock :size="14" />
                <span v-if="expired">已失效,请重新生成</span>
                <span v-else-if="token">{{ countdownText }} 后失效</span>
                <span v-else>尚未生成</span>
            </div>

            <div v-if="qrUrl" class="qr-url">
                <span class="qr-url__label">二维码链接</span>
                <code class="qr-url__value">{{ qrUrl }}</code>
                <a-button size="mini" type="text" @click="copyUrl">复制</a-button>
            </div>

            <a-button type="primary" class="qr-generate" :loading="loading" @click="generate()">
                <template #icon><RefreshCw :size="15" /></template>
                {{ token ? "重新生成" : "生成二维码" }}
            </a-button>
        </div>

        <div class="qr-tips">
            <span class="qr-tips__icon"><ShieldAlert :size="15" /></span>
            <div class="qr-tips__body">
                <span>二维码含平台接入凭据,请勿截图外传;建议在可信网络内使用。</span>
                <span>点击重新生成后,旧二维码在原到期时间前仍然有效。</span>
            </div>
        </div>
    </section>
</template>

<style scoped>
/* 与同页 sip-card 视觉一致的内嵌卡片 */
.qr-card {
    display: flex;
    flex-direction: column;
    gap: 8px;
    padding: 12px 14px;
    background: var(--uvp-panel-bg, #ffffff);
    border: 1px solid var(--uvp-panel-border, #e8edf5);
    border-radius: 12px;
}

.qr-card__header {
    display: flex;
    align-items: center;
    gap: 8px;
    padding-bottom: 6px;
    border-bottom: 1px dashed var(--uvp-panel-border, #e8edf5);
}

.qr-card__header h4 {
    margin: 0;
    font-size: 13px;
    font-weight: 600;
    color: var(--uvp-text-primary, #1f2937);
}

.qr-card__header-hint {
    margin-left: auto;
    color: var(--uvp-text-tertiary, #6b7280);
    font-size: 11.5px;
}

.qr-card__icon {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 26px;
    height: 26px;
    color: var(--uvp-brand, #2563eb);
    background: var(--uvp-brand-soft, #e8f2ff);
    border-radius: 7px;
    flex-shrink: 0;
}

.qr-stage {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 7px;
}

.qr-canvas {
    display: flex;
    align-items: center;
    justify-content: center;
    width: 156px;
    height: 156px;
    padding: 6px;
    background: #ffffff;
    border: 1px solid var(--uvp-panel-border, #e8edf5);
    border-radius: 12px;
}

.qr-canvas img {
    width: 100%;
    height: 100%;
}

.qr-placeholder {
    display: flex;
    align-items: center;
    justify-content: center;
    width: 156px;
    height: 156px;
    color: var(--uvp-text-tertiary, #6b7280);
    font-size: 12px;
    text-align: center;
    background: var(--uvp-shell-muted, #eef4f8);
    border: 1px dashed var(--uvp-panel-border, #e8edf5);
    border-radius: 12px;
}

.qr-placeholder.is-expired {
    color: #b7791f;
}

.qr-meta {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    color: var(--uvp-text-tertiary, #6b7280);
    font-size: 12.5px;
}

.qr-url {
    display: flex;
    align-items: center;
    gap: 8px;
    width: 100%;
    min-width: 0;
    padding: 8px 10px;
    background: var(--uvp-shell-muted, #eef4f8);
    border-radius: 8px;
}

.qr-url__label {
    flex-shrink: 0;
    color: var(--uvp-text-tertiary, #6b7280);
    font-size: 12px;
}

.qr-url__value {
    flex: 1;
    min-width: 0;
    overflow-wrap: anywhere;
    color: var(--uvp-brand-strong, #1d4ed8);
    font-family: "SFMono-Regular", Consolas, Menlo, monospace;
    font-size: 12px;
    user-select: all;
}

.qr-generate {
    width: 100%;
}

.qr-tips {
    display: flex;
    gap: 8px;
    padding: 7px 9px;
    background: var(--uvp-warning-soft, #fffbeb);
    border: 1px solid rgb(183 121 31 / 22%);
    border-radius: 8px;
}

.qr-tips__icon {
    flex-shrink: 0;
    color: #b7791f;
    line-height: 1;
}

.qr-tips__body {
    display: grid;
    gap: 2px;
    color: var(--uvp-text-secondary, #4b5563);
    font-size: 12px;
    line-height: 1.5;
}

.qr-card :deep(.arco-form-item) {
    margin-bottom: 8px;
}

.qr-card :deep(.arco-form-message) {
    font-size: 11px;
}

.qr-note {
    display: flex;
    align-items: center;
    gap: 7px;
    padding: 6px 9px;
    color: var(--uvp-brand-strong, #1d4ed8);
    background: var(--uvp-brand-soft, #e8f2ff);
    border: 1px solid rgb(37 99 235 / 16%);
    border-radius: 7px;
    font-size: 11.5px;
    line-height: 1.4;
}

.qr-note > svg {
    flex-shrink: 0;
}
</style>
