<template>
    <div class="login-page">
        <!-- Topology background — connections static, nodes twinkle -->
        <svg class="topology-bg" viewBox="0 0 1600 1000" preserveAspectRatio="xMidYMid slice" aria-hidden="true">
            <defs>
                <radialGradient id="g-hub" cx="50%" cy="50%" r="50%">
                    <stop offset="0%" stop-color="#69c0ff" stop-opacity="1" />
                    <stop offset="40%" stop-color="#1890ff" stop-opacity="0.55" />
                    <stop offset="100%" stop-color="#1890ff" stop-opacity="0" />
                </radialGradient>
                <radialGradient id="g-node" cx="50%" cy="50%" r="50%">
                    <stop offset="0%" stop-color="#40a9ff" stop-opacity="0.95" />
                    <stop offset="55%" stop-color="#1890ff" stop-opacity="0.35" />
                    <stop offset="100%" stop-color="#1890ff" stop-opacity="0" />
                </radialGradient>
            </defs>
            <g stroke="#1890ff" stroke-opacity="0.10" stroke-width="1" fill="none">
                <path d="M800 500 L260 180" />
                <path d="M800 500 L820 120" />
                <path d="M800 500 L1340 200" />
                <path d="M800 500 L1500 460" />
                <path d="M800 500 L1380 820" />
                <path d="M800 500 L820 900" />
                <path d="M800 500 L260 820" />
                <path d="M800 500 L120 480" />
                <path d="M260 180 L80 360" />
                <path d="M260 180 L460 80" />
                <path d="M820 120 L640 60" />
                <path d="M820 120 L980 60" />
                <path d="M1340 200 L1180 80" />
                <path d="M1340 200 L1540 100" />
                <path d="M1500 460 L1560 320" />
                <path d="M1500 460 L1560 600" />
                <path d="M1380 820 L1540 940" />
                <path d="M1380 820 L1180 940" />
                <path d="M820 900 L640 940" />
                <path d="M820 900 L1000 980" />
                <path d="M260 820 L80 940" />
                <path d="M260 820 L420 940" />
                <path d="M120 480 L40 600" />
            </g>
            <circle class="hub-node" cx="800" cy="500" r="36" fill="url(#g-hub)" />
            <circle class="primary" style="animation-delay:-0.2s" cx="260" cy="180" r="11" fill="url(#g-node)" />
            <circle class="primary" style="animation-delay:-0.9s" cx="820" cy="120" r="11" fill="url(#g-node)" />
            <circle class="primary" style="animation-delay:-1.6s" cx="1340" cy="200" r="11" fill="url(#g-node)" />
            <circle class="primary" style="animation-delay:-2.3s" cx="1500" cy="460" r="11" fill="url(#g-node)" />
            <circle class="primary" style="animation-delay:-3.0s" cx="1380" cy="820" r="11" fill="url(#g-node)" />
            <circle class="primary" style="animation-delay:-3.7s" cx="820" cy="900" r="11" fill="url(#g-node)" />
            <circle class="primary" style="animation-delay:-4.4s" cx="260" cy="820" r="11" fill="url(#g-node)" />
            <circle class="primary" style="animation-delay:-0.5s" cx="120" cy="480" r="11" fill="url(#g-node)" />
            <circle class="secondary" style="animation-delay:-0.1s" cx="80" cy="360" r="6" fill="url(#g-node)" />
            <circle class="secondary" style="animation-delay:-0.7s" cx="460" cy="80" r="6" fill="url(#g-node)" />
            <circle class="secondary" style="animation-delay:-1.3s" cx="640" cy="60" r="6" fill="url(#g-node)" />
            <circle class="secondary" style="animation-delay:-1.9s" cx="980" cy="60" r="6" fill="url(#g-node)" />
            <circle class="secondary" style="animation-delay:-2.5s" cx="1180" cy="80" r="6" fill="url(#g-node)" />
            <circle class="secondary" style="animation-delay:-3.1s" cx="1540" cy="100" r="6" fill="url(#g-node)" />
            <circle class="secondary" style="animation-delay:-3.7s" cx="1560" cy="320" r="6" fill="url(#g-node)" />
            <circle class="secondary" style="animation-delay:-4.3s" cx="1560" cy="600" r="6" fill="url(#g-node)" />
            <circle class="secondary" style="animation-delay:-4.9s" cx="1540" cy="940" r="6" fill="url(#g-node)" />
            <circle class="secondary" style="animation-delay:-5.5s" cx="1180" cy="940" r="6" fill="url(#g-node)" />
            <circle class="secondary" style="animation-delay:-6.1s" cx="1000" cy="980" r="6" fill="url(#g-node)" />
            <circle class="secondary" style="animation-delay:-0.4s" cx="640" cy="940" r="6" fill="url(#g-node)" />
            <circle class="secondary" style="animation-delay:-1.0s" cx="80" cy="940" r="6" fill="url(#g-node)" />
            <circle class="secondary" style="animation-delay:-1.6s" cx="420" cy="940" r="6" fill="url(#g-node)" />
            <circle class="secondary" style="animation-delay:-2.2s" cx="40" cy="600" r="6" fill="url(#g-node)" />
            <circle class="secondary" style="animation-delay:-2.8s" cx="260" cy="500" r="6" fill="url(#g-node)" />
            <g fill="#1890ff" opacity="0.55">
                <circle cx="260" cy="180" r="1.5" />
                <circle cx="820" cy="120" r="1.5" />
                <circle cx="1340" cy="200" r="1.5" />
                <circle cx="1500" cy="460" r="1.5" />
                <circle cx="1380" cy="820" r="1.5" />
                <circle cx="820" cy="900" r="1.5" />
                <circle cx="260" cy="820" r="1.5" />
                <circle cx="120" cy="480" r="1.5" />
            </g>
        </svg>

        <!-- Top brand bar -->
        <header class="brand-bar">
            <span class="brand-logo-text">UVP 统一视频接入平台</span>
            <span class="brand-meta">v2.3.0 · GB/T 28181-2022</span>
        </header>

        <!-- Floating card -->
        <main class="stage">
            <div class="float-card">
                <!-- LEFT: brand / slogan -->
                <aside class="brand-side">
                    <div class="brand-eyebrow">Unified Video Platform</div>
                    <div class="brand-title">把所有视频源<br />接入同一张控制台</div>
                    <p class="brand-lede">
                        国标 SIP 接入 · 视频转发 · 录像点播 · 级联调度,
                        一套平台覆盖从前端到上联的全链路。
                    </p>

                    <div class="brand-features">
                        <div class="feature">
                            <span class="ic">
                                <svg width="12" height="12" viewBox="0 0 24 24" fill="none"
                                    stroke="currentColor" stroke-width="3">
                                    <polyline points="20 6 9 17 4 12" />
                                </svg>
                            </span>
                            <span>GB/T 28181-2022 全字段兼容</span>
                        </div>
                        <div class="feature">
                            <span class="ic">
                                <svg width="12" height="12" viewBox="0 0 24 24" fill="none"
                                    stroke="currentColor" stroke-width="3">
                                    <polyline points="20 6 9 17 4 12" />
                                </svg>
                            </span>
                            <span>多租户隔离 · 设备权限粒度到通道</span>
                        </div>
                        <div class="feature">
                            <span class="ic">
                                <svg width="12" height="12" viewBox="0 0 24 24" fill="none"
                                    stroke="currentColor" stroke-width="3">
                                    <polyline points="20 6 9 17 4 12" />
                                </svg>
                            </span>
                            <span>ZLMediaKit 媒体集群 · p99 &lt; 200ms</span>
                        </div>
                    </div>

                    <div class="brand-foot">
                        <span class="chip">SIP / RTP</span>
                        <span class="chip">级联</span>
                        <span class="chip">回放点播</span>
                        <span class="chip">多租户</span>
                    </div>
                </aside>

                <!-- RIGHT: form -->
                <section class="form-side">
                    <div class="form-head">
                        <h1>账号登录</h1>
                        <p>欢迎回来,请输入您的账号信息</p>
                    </div>
                    <LoginForm />
                </section>
            </div>
        </main>

        <!-- Page footer -->
        <footer class="page-foot">
            <span v-if="systemCopyright">{{ systemCopyright }}</span>
            <span v-else>© 2026 UVP 统一视频接入平台</span>
            <span class="sep">·</span>
            <span>Powered by GB/T 28181-2022</span>
            <span v-if="systemRecordNo" class="sep">·</span>
            <span v-if="systemRecordNo">{{ systemRecordNo }}</span>
        </footer>
    </div>
</template>

<script setup lang="ts">
import { computed } from "vue";
import LoginForm from "@/views/login/components/login-form.vue";
import { useSysConfigStore } from "@/store/modules/sys-config";
import { storeToRefs } from "pinia";

const sysConfigStore = useSysConfigStore();
const { systemConfig } = storeToRefs(sysConfigStore);

const systemCopyright = computed(() => systemConfig.value?.systemCopyright || "");
const systemRecordNo = computed(() => systemConfig.value?.systemRecordNo || "");
</script>

<style lang="scss" scoped>
$primary: #1890ff;
$primary-light: #69c0ff;

.login-page {
    position: relative;
    min-height: 100vh;
    background:
        radial-gradient(ellipse at 50% 50%, rgba(24, 144, 255, .06), transparent 65%),
        linear-gradient(135deg, #0a1424 0%, #0f1d33 50%, #122242 100%);
    display: flex;
    flex-direction: column;
    overflow: hidden;
    color: #fff;
    font-family: -apple-system, "PingFang SC", "Microsoft YaHei", "Helvetica Neue", sans-serif;

    &::after {
        content: "";
        position: absolute;
        inset: 0;
        background: radial-gradient(ellipse 50% 40% at 50% 50%, rgba(15, 29, 51, .55), transparent 70%);
        pointer-events: none;
        z-index: 0;
    }
}

/* Topology */
.topology-bg {
    position: absolute;
    inset: 0;
    width: 100%;
    height: 100%;
    pointer-events: none;
    z-index: 0;

    .hub-node { animation: star-pulse-hub 6s ease-in-out infinite; }
    .primary { animation: star-pulse 5s ease-in-out infinite; }
    .secondary { animation: star-pulse 6.5s ease-in-out infinite; }
}

@keyframes star-pulse {
    0%, 100% { opacity: .30; }
    50%      { opacity: 1; }
}
@keyframes star-pulse-hub {
    0%, 100% { opacity: .65; }
    50%      { opacity: 1; }
}
@media (prefers-reduced-motion: reduce) {
    .topology-bg .hub-node,
    .topology-bg .primary,
    .topology-bg .secondary { animation: none; }
}

/* Brand bar */
.brand-bar {
    position: relative;
    z-index: 2;
    height: 64px;
    display: flex;
    align-items: center;
    padding: 0 32px;
    flex-shrink: 0;
}
.brand-logo-text {
    font-size: 16px;
    font-weight: 600;
    letter-spacing: .5px;
    color: #fff;
}
.brand-meta {
    margin-left: auto;
    font-size: 12px;
    color: rgba(255, 255, 255, .62);
    font-family: "JetBrains Mono", "SFMono-Regular", Menlo, monospace;
    letter-spacing: .08em;
}

/* Stage */
.stage {
    position: relative;
    z-index: 1;
    flex: 1;
    display: flex;
    align-items: center;
    justify-content: center;
    padding: 32px 24px 56px;
}

.float-card {
    width: 920px;
    max-width: 100%;
    min-height: 520px;
    background: #fff;
    color: #333;
    border-radius: 12px;
    box-shadow: 0 24px 64px rgba(0, 0, 0, .28),
                0 8px 24px rgba(0, 0, 0, .10);
    display: grid;
    grid-template-columns: 1.05fr 1fr;
    overflow: hidden;
}

/* Brand side */
.brand-side {
    position: relative;
    background:
        radial-gradient(ellipse at 30% 20%, rgba(64, 169, 255, .20), transparent 60%),
        radial-gradient(ellipse at 75% 90%, rgba(11, 77, 153, .40), transparent 55%),
        linear-gradient(140deg, #0d2547 0%, #1a3d6e 100%);
    color: #fff;
    padding: 52px 40px 36px;
    display: flex;
    flex-direction: column;
    overflow: hidden;

    &::before {
        content: "";
        position: absolute;
        inset: 0;
        background-image:
            linear-gradient(rgba(255, 255, 255, .04) 1px, transparent 1px),
            linear-gradient(90deg, rgba(255, 255, 255, .04) 1px, transparent 1px);
        background-size: 32px 32px;
        pointer-events: none;
    }
}
.brand-eyebrow {
    position: relative;
    font-family: "JetBrains Mono", "SFMono-Regular", Menlo, monospace;
    font-size: 11px;
    letter-spacing: .22em;
    color: rgba(255, 255, 255, .7);
    text-transform: uppercase;
    margin-bottom: 14px;
}
.brand-title {
    position: relative;
    font-size: 32px;
    font-weight: 700;
    line-height: 1.3;
    letter-spacing: -.3px;
    margin-bottom: 14px;
}
.brand-lede {
    position: relative;
    font-size: 14px;
    line-height: 1.7;
    color: rgba(255, 255, 255, .82);
    margin-bottom: auto;
    max-width: 360px;
}
.brand-features {
    position: relative;
    margin-top: 32px;
    display: flex;
    flex-direction: column;
    gap: 12px;

    .feature {
        display: flex;
        align-items: center;
        gap: 10px;
        font-size: 13px;
        color: rgba(255, 255, 255, .88);

        .ic {
            width: 22px;
            height: 22px;
            border-radius: 6px;
            background: rgba(105, 192, 255, .16);
            border: 1px solid rgba(105, 192, 255, .32);
            display: flex;
            align-items: center;
            justify-content: center;
            color: $primary-light;
            flex-shrink: 0;
        }
    }
}
.brand-foot {
    position: relative;
    margin-top: 28px;
    padding-top: 20px;
    border-top: 1px solid rgba(255, 255, 255, .16);
    display: flex;
    flex-wrap: wrap;
    gap: 6px;

    .chip {
        font-size: 11px;
        padding: 3px 9px;
        border-radius: 99px;
        background: rgba(255, 255, 255, .08);
        border: 1px solid rgba(255, 255, 255, .18);
        color: rgba(255, 255, 255, .86);
        letter-spacing: .3px;
    }
}

/* Form side */
.form-side {
    padding: 48px 48px 40px;
    display: flex;
    flex-direction: column;
    justify-content: center;
}
.form-head {
    margin-bottom: 8px;

    h1 {
        font-size: 22px;
        font-weight: 600;
        color: #333;
        margin-bottom: 6px;
    }
    p {
        font-size: 13px;
        color: #999;
    }
}

/* Override Arco primary button inside login form — make it weighty + on-brand */
.form-side :deep(.arco-btn-primary) {
    height: 44px;
    border-radius: 8px;
    font-size: 15px;
    font-weight: 600;
    letter-spacing: 4px;
    background: linear-gradient(135deg, #1890ff 0%, #096dd9 100%);
    border: none;
    box-shadow:
        0 8px 20px rgba(24, 144, 255, .32),
        inset 0 1px 0 rgba(255, 255, 255, .18);
    transition: transform .15s ease, box-shadow .2s ease, background .2s ease;
}
.form-side :deep(.arco-btn-primary:hover) {
    background: linear-gradient(135deg, #40a9ff 0%, #1890ff 100%);
    box-shadow:
        0 12px 28px rgba(24, 144, 255, .42),
        inset 0 1px 0 rgba(255, 255, 255, .22);
    transform: translateY(-1px);
}
.form-side :deep(.arco-btn-primary:active) {
    background: linear-gradient(135deg, #096dd9 0%, #0050b3 100%);
    box-shadow: 0 4px 12px rgba(24, 144, 255, .32);
    transform: translateY(0);
}
.form-side :deep(.arco-btn-primary[disabled]),
.form-side :deep(.arco-btn-primary.arco-btn-loading) {
    opacity: .85;
    transform: none;
}

/* Page footer */
.page-foot {
    position: relative;
    z-index: 2;
    text-align: center;
    padding: 16px 24px 24px;
    font-size: 12px;
    color: rgba(255, 255, 255, .55);
    flex-shrink: 0;

    .sep {
        margin: 0 8px;
        color: rgba(255, 255, 255, .25);
    }
}

/* Responsive */
@media (max-width: 880px) {
    .float-card {
        grid-template-columns: 1fr;
        width: 420px;
        min-height: 0;
    }
    .brand-side {
        padding: 32px 28px;

        .brand-features,
        .brand-foot { display: none; }
    }
    .form-side { padding: 32px 28px; }
}
</style>
