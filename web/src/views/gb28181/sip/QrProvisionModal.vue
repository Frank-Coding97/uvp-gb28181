<script setup lang="ts">
import { computed, onUnmounted, ref, watch } from "vue";
import { Message } from "@arco-design/web-vue";
import { Clock, RefreshCw, ShieldAlert } from "lucide-vue-next";
import { generateSipQrToken } from "@/api/gb28181";
import { buildQrUrl, formatCountdown, normalizeBaseUrl, validateBaseUrl } from "./qrProvisionState";

const props = defineProps<{ visible: boolean }>();
const emit = defineEmits<{ "update:visible": [value: boolean] }>();

const loading = ref(false);
const token = ref("");
const secondsLeft = ref(0);
// 平台访问地址.默认当前站点 origin —— 设备扫码后要用它兑换,必须是设备能访问到的地址.
const baseUrl = ref(normalizeBaseUrl(window.location.origin));
const touched = ref({ baseUrl: false });
let timer: ReturnType<typeof setInterval> | null = null;

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

// 倒计时以响应到达时刻起算 expiresInSeconds,不用绝对时间戳 —— 免受客户端时钟偏移影响.
function startCountdown(expiresInSeconds: number) {
    stopTimer();
    secondsLeft.value = Math.max(0, expiresInSeconds);
    timer = setInterval(() => {
        secondsLeft.value = Math.max(0, secondsLeft.value - 1);
        if (secondsLeft.value === 0) stopTimer();
    }, 1000);
}

async function generate() {
    touched.value.baseUrl = true;
    if (!baseUrlValid.value) {
        Message.warning(validateBaseUrl(baseUrl.value));
        return;
    }
    loading.value = true;
    try {
        const res = await generateSipQrToken();
        if (res.code !== 0) throw new Error(res.message || "生成接入二维码失败");
        token.value = res.data.token;
        startCountdown(res.data.expiresInSeconds);
    } catch (error: any) {
        Message.error(error?.message || "生成接入二维码失败");
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

function close() {
    emit("update:visible", false);
}

watch(
    () => props.visible,
    visible => {
        if (visible) {
            generate();
            return;
        }
        // 关闭即丢弃当前码:凭据不留在内存里等下次打开
        stopTimer();
        token.value = "";
        secondsLeft.value = 0;
        touched.value.baseUrl = false;
    }
);

onUnmounted(stopTimer);
</script>

<template>
    <a-modal
        :visible="visible"
        title="扫码接入"
        width="min(520px, calc(100vw - 24px))"
        modal-class="uvp-system-dialog qr-provision-dialog"
        unmount-on-close
        @cancel="close"
    >
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
                <span v-if="expired">二维码已失效</span>
                <span v-else-if="loading">正在生成…</span>
                <span v-else-if="!baseUrlValid">请先填写合法的平台访问地址</span>
                <span v-else>暂无二维码</span>
            </div>

            <div class="qr-meta">
                <Clock :size="14" />
                <span v-if="expired">已失效,请重新生成</span>
                <span v-else>{{ countdownText }} 后失效</span>
            </div>

            <div v-if="qrUrl" class="qr-url">
                <span class="qr-url__label">二维码链接</span>
                <code class="qr-url__value">{{ qrUrl }}</code>
                <a-button size="mini" type="text" @click="copyUrl">复制</a-button>
            </div>
        </div>

        <div class="qr-tips">
            <span class="qr-tips__icon"><ShieldAlert :size="15" /></span>
            <div class="qr-tips__body">
                <span>二维码含平台接入凭据,请勿截图外传;建议在可信网络内使用。</span>
                <span>点击重新生成后,旧二维码在原到期时间前仍然有效。</span>
            </div>
        </div>

        <template #footer>
            <a-button type="text" @click="close">关闭</a-button>
            <span class="qr-footer-spacer" />
            <a-button type="primary" :loading="loading" @click="generate">
                <template #icon><RefreshCw :size="15" /></template>
                {{ token ? "重新生成" : "生成二维码" }}
            </a-button>
        </template>
    </a-modal>
</template>

<style lang="scss">
/* uvp-system-dialog 已提供弹窗骨架样式,这里只补扫码区专有排版. */
.qr-provision-dialog .qr-stage {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 10px;
}

.qr-provision-dialog .qr-canvas {
    display: flex;
    align-items: center;
    justify-content: center;
    width: 236px;
    height: 236px;
    padding: 8px;
    background: #ffffff;
    border: 1px solid var(--uvp-panel-border, #e8edf5);
    border-radius: 12px;
}

.qr-provision-dialog .qr-canvas img {
    width: 100%;
    height: 100%;
}

.qr-provision-dialog .qr-placeholder {
    display: flex;
    align-items: center;
    justify-content: center;
    width: 236px;
    height: 236px;
    color: var(--uvp-text-tertiary, #6b7280);
    font-size: 13px;
    text-align: center;
    background: var(--uvp-shell-muted, #eef4f8);
    border: 1px dashed var(--uvp-panel-border, #e8edf5);
    border-radius: 12px;
}

.qr-provision-dialog .qr-placeholder.is-expired {
    color: #b7791f;
}

.qr-provision-dialog .qr-meta {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    color: var(--uvp-text-tertiary, #6b7280);
    font-size: 12.5px;
}

.qr-provision-dialog .qr-url {
    display: flex;
    align-items: center;
    gap: 8px;
    width: 100%;
    min-width: 0;
    padding: 8px 10px;
    background: var(--uvp-shell-muted, #eef4f8);
    border-radius: 8px;
}

.qr-provision-dialog .qr-url__label {
    flex-shrink: 0;
    color: var(--uvp-text-tertiary, #6b7280);
    font-size: 12px;
}

.qr-provision-dialog .qr-url__value {
    flex: 1;
    min-width: 0;
    overflow-wrap: anywhere;
    color: var(--uvp-brand-strong, #1d4ed8);
    font-family: "SFMono-Regular", Consolas, Menlo, monospace;
    font-size: 12px;
    user-select: all;
}

.qr-provision-dialog .qr-tips {
    display: flex;
    gap: 8px;
    margin-top: 14px;
    padding: 10px 12px;
    background: var(--uvp-warning-soft, #fffbeb);
    border: 1px solid rgb(183 121 31 / 22%);
    border-radius: 8px;
}

.qr-provision-dialog .qr-tips__icon {
    flex-shrink: 0;
    color: #b7791f;
    line-height: 1;
}

.qr-provision-dialog .qr-tips__body {
    display: grid;
    gap: 4px;
    color: var(--uvp-text-secondary, #4b5563);
    font-size: 12px;
    line-height: 1.6;
}

.qr-provision-dialog .qr-footer-spacer {
    flex: 1;
}
</style>
