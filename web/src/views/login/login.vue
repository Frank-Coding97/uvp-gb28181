<template>
  <div class="login-page" :class="{ 'dark-mode': darkMode }">
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
      <circle class="primary" style="animation-delay: -0.2s" cx="260" cy="180" r="11" fill="url(#g-node)" />
      <circle class="primary" style="animation-delay: -0.9s" cx="820" cy="120" r="11" fill="url(#g-node)" />
      <circle class="primary" style="animation-delay: -1.6s" cx="1340" cy="200" r="11" fill="url(#g-node)" />
      <circle class="primary" style="animation-delay: -2.3s" cx="1500" cy="460" r="11" fill="url(#g-node)" />
      <circle class="primary" style="animation-delay: -3s" cx="1380" cy="820" r="11" fill="url(#g-node)" />
      <circle class="primary" style="animation-delay: -3.7s" cx="820" cy="900" r="11" fill="url(#g-node)" />
      <circle class="primary" style="animation-delay: -4.4s" cx="260" cy="820" r="11" fill="url(#g-node)" />
      <circle class="primary" style="animation-delay: -0.5s" cx="120" cy="480" r="11" fill="url(#g-node)" />
      <circle class="secondary" style="animation-delay: -0.1s" cx="80" cy="360" r="6" fill="url(#g-node)" />
      <circle class="secondary" style="animation-delay: -0.7s" cx="460" cy="80" r="6" fill="url(#g-node)" />
      <circle class="secondary" style="animation-delay: -1.3s" cx="640" cy="60" r="6" fill="url(#g-node)" />
      <circle class="secondary" style="animation-delay: -1.9s" cx="980" cy="60" r="6" fill="url(#g-node)" />
      <circle class="secondary" style="animation-delay: -2.5s" cx="1180" cy="80" r="6" fill="url(#g-node)" />
      <circle class="secondary" style="animation-delay: -3.1s" cx="1540" cy="100" r="6" fill="url(#g-node)" />
      <circle class="secondary" style="animation-delay: -3.7s" cx="1560" cy="320" r="6" fill="url(#g-node)" />
      <circle class="secondary" style="animation-delay: -4.3s" cx="1560" cy="600" r="6" fill="url(#g-node)" />
      <circle class="secondary" style="animation-delay: -4.9s" cx="1540" cy="940" r="6" fill="url(#g-node)" />
      <circle class="secondary" style="animation-delay: -5.5s" cx="1180" cy="940" r="6" fill="url(#g-node)" />
      <circle class="secondary" style="animation-delay: -6.1s" cx="1000" cy="980" r="6" fill="url(#g-node)" />
      <circle class="secondary" style="animation-delay: -0.4s" cx="640" cy="940" r="6" fill="url(#g-node)" />
      <circle class="secondary" style="animation-delay: -1s" cx="80" cy="940" r="6" fill="url(#g-node)" />
      <circle class="secondary" style="animation-delay: -1.6s" cx="420" cy="940" r="6" fill="url(#g-node)" />
      <circle class="secondary" style="animation-delay: -2.2s" cx="40" cy="600" r="6" fill="url(#g-node)" />
      <circle class="secondary" style="animation-delay: -2.8s" cx="260" cy="500" r="6" fill="url(#g-node)" />
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

    <header class="page-meta">
      <span class="version-text">{{ APP_VERSION_TEXT }} · GB/T 28181-2022</span>
    </header>

    <!-- Floating card -->
    <main class="stage">
      <div class="float-card">
        <button
          id="login-theme-toggle"
          class="theme-toggle"
          type="button"
          :aria-label="darkMode ? '切换到明亮模式' : '切换到暗色模式'"
          :title="darkMode ? '切换到明亮模式' : '切换到暗色模式'"
          :aria-pressed="darkMode"
          @click="toggleThemeMode"
        >
          <Sun v-if="darkMode" :size="18" aria-hidden="true" />
          <Moon v-else :size="18" aria-hidden="true" />
        </button>
        <!-- LEFT: brand / slogan -->
        <aside class="brand-side">
          <div class="brand-heading">
            <img class="brand-mark" :src="uvpMark" alt="统一视频接入平台" />
            <div class="brand-eyebrow">Unified Video Platform</div>
          </div>
          <div class="brand-title" aria-label="统一视频接入平台-GB28181">
            <span>统一视频接入平台</span>
            <span>GB28181</span>
          </div>
          <p class="brand-lede">
            以国标接入为核心，集中管理设备、视频、录像与平台级联，结合信令追踪和媒体诊断，让运行状态与问题定位有据可依。
          </p>

          <div class="brand-features">
            <div class="feature">
              <span class="ic">
                <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="3">
                  <polyline points="20 6 9 17 4 12" />
                </svg>
              </span>
              <span>视频业务：实时预览 · 录像回放 · 级联共享</span>
            </div>
            <div class="feature">
              <span class="ic">
                <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="3">
                  <polyline points="20 6 9 17 4 12" />
                </svg>
              </span>
              <span>资源管理：通道授权 · 多媒体节点调度</span>
            </div>
            <div class="feature">
              <span class="ic">
                <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="3">
                  <polyline points="20 6 9 17 4 12" />
                </svg>
              </span>
              <span>运行追溯：SIP 追踪 · 媒体探针 · 日志审计</span>
            </div>
          </div>

          <div class="brand-foot">
            <span class="chip">GB/T 28181</span>
            <span class="chip">云端录像</span>
            <span class="chip">多节点</span>
            <span class="chip">OpenAPI</span>
          </div>
        </aside>

        <!-- RIGHT: form -->
        <section class="form-side">
          <div class="form-head">
            <h1>账号登录</h1>
            <p>请输入账号信息进入控制台</p>
          </div>
          <LoginForm />
        </section>
      </div>
    </main>

    <!-- Page footer -->
    <footer class="page-foot">
      <span>{{ displayCopyright }}</span>
      <span class="sep">·</span>
      <span>Powered by GB/T 28181-2022</span>
      <span v-if="displayRecordNo" class="sep">·</span>
      <span v-if="displayRecordNo">{{ displayRecordNo }}</span>
    </footer>
  </div>
</template>

<script setup lang="ts">
import { computed } from "vue";
import LoginForm from "@/views/login/components/login-form.vue";
import { useSysConfigStore } from "@/store/modules/sys-config";
import { storeToRefs } from "pinia";
import { getDisplaySystemCopyright, getDisplaySystemRecordNo } from "@/utils/system-footer";
import { APP_VERSION_TEXT } from "@/config/version";
import uvpMark from "@/assets/logo/uvp-mark.svg";
import { Sun, Moon } from "lucide-vue-next";
import { useThemeConfig } from "@/store/modules/theme-config";
import { useThemeMethods } from "@/hooks/useThemeMethods";

const { darkMode } = storeToRefs(useThemeConfig());
const { setDarkMode } = useThemeMethods();
const toggleThemeMode = () => {
  darkMode.value = !darkMode.value;
  setDarkMode();
};

const sysConfigStore = useSysConfigStore();
const { systemConfig } = storeToRefs(sysConfigStore);

const displayCopyright = computed(() => {
  return getDisplaySystemCopyright(systemConfig.value?.systemCopyright);
});
const displayRecordNo = computed(() => {
  return getDisplaySystemRecordNo(systemConfig.value?.systemRecordNo);
});
</script>

<style lang="scss" scoped>
$primary: #1890ff;
$primary-light: #69c0ff;

.login-page {
  --login-bg: linear-gradient(135deg, #edf4fc 0%, #f5f8fd 50%, #e2edfa 100%);
  --login-overlay: rgb(255 255 255 / 40%);
  --login-surface: #ffffff;
  --login-text: #1f2d3d;
  --login-muted: #64748b;
  --login-page-muted: #64748b;
  --login-control-bg: linear-gradient(180deg, #f8fbff 0%, #f3f7fc 100%);
  --login-control-hover-bg: #f7fbff;
  --login-control-focus-bg: #ffffff;
  --login-control-border: #dbe6f4;
  --login-control-hover-border: #b8cce7;
  --login-control-shadow: inset 0 1px 0 rgb(255 255 255 / 90%);
  --login-placeholder: #718198;
  --login-icon: #64748b;
  --login-card-shadow: 0 24px 64px rgb(30 64 100 / 12%), 0 8px 24px rgb(30 64 100 / 8%);

  position: relative;
  display: flex;
  flex-direction: column;
  min-height: 100vh;
  overflow: hidden;
  font-family: -apple-system, "PingFang SC", "Microsoft YaHei", "Helvetica Neue", sans-serif;
  color: var(--login-text);
  background: radial-gradient(ellipse at 50% 50%, rgb(24 144 255 / 6%), transparent 65%), var(--login-bg);

  &::after {
    position: absolute;
    inset: 0;
    z-index: 0;
    pointer-events: none;
    content: "";
    background: radial-gradient(ellipse 50% 40% at 50% 50%, var(--login-overlay), transparent 70%);
  }
}

.login-page.dark-mode {
  --login-bg: linear-gradient(135deg, #0a1424 0%, #0f1d33 50%, #122242 100%);
  --login-overlay: rgb(15 29 51 / 55%);
  --login-page-muted: #a5b5ca;
  --login-card-shadow: 0 24px 64px rgb(0 0 0 / 28%), 0 8px 24px rgb(0 0 0 / 10%);

  color-scheme: dark;
}

/* Topology */
.topology-bg {
  position: absolute;
  inset: 0;
  z-index: 0;
  width: 100%;
  height: 100%;
  pointer-events: none;

  .hub-node {
    animation: star-pulse-hub 6s ease-in-out infinite;
  }
  .primary {
    animation: star-pulse 5s ease-in-out infinite;
  }
  .secondary {
    animation: star-pulse 6.5s ease-in-out infinite;
  }
}

@keyframes star-pulse {
  0%,
  100% {
    opacity: 0.3;
  }
  50% {
    opacity: 1;
  }
}

@keyframes star-pulse-hub {
  0%,
  100% {
    opacity: 0.65;
  }
  50% {
    opacity: 1;
  }
}

@media (prefers-reduced-motion: reduce) {
  .topology-bg .hub-node,
  .topology-bg .primary,
  .topology-bg .secondary {
    animation: none;
  }
}

.page-meta {
  position: relative;
  z-index: 2;
  display: flex;
  flex-shrink: 0;
  gap: 16px;
  align-items: center;
  justify-content: flex-end;
  height: 64px;
  padding: 0 32px;
  font-family: "JetBrains Mono", SFMono-Regular, Menlo, monospace;
  font-size: 12px;
  color: var(--login-page-muted);
  letter-spacing: 0;
}

.theme-toggle {
  position: absolute;
  top: 16px;
  right: 16px;
  z-index: 2;
  display: flex;
  flex-shrink: 0;
  align-items: center;
  justify-content: center;
  width: 40px;
  height: 40px;
  padding: 0;
  color: var(--login-text);
  cursor: pointer;
  background: var(--login-surface);
  border: 1px solid var(--login-control-border);
  border-radius: 8px;

  &:hover {
    background: var(--login-control-hover-bg);
    border-color: var(--login-control-hover-border);
  }

  &:focus-visible {
    outline: 2px solid $primary;
    outline-offset: 3px;
  }
}

/* Card branding */
.brand-heading {
  position: relative;
  display: flex;
  gap: 12px;
  align-items: center;
  margin-bottom: 18px;
}
.brand-mark {
  display: block;
  flex-shrink: 0;
  width: 36px;
  height: 36px;
  object-fit: contain;
}

/* Stage */
.stage {
  position: relative;
  z-index: 1;
  display: flex;
  flex: 1;
  align-items: center;
  justify-content: center;
  padding: 32px 24px 56px;
}

.float-card {
  position: relative;
  display: grid;
  grid-template-columns: 1.05fr 1fr;
  width: 920px;
  max-width: 100%;
  min-height: 520px;
  overflow: hidden;
  color: var(--login-text);
  color-scheme: light;
  background: var(--login-surface);
  border-radius: 12px;
  box-shadow: var(--login-card-shadow);
}

/* Brand side */
.brand-side {
  position: relative;
  display: flex;
  flex-direction: column;
  padding: 52px 40px 36px;
  overflow: hidden;
  color: #ffffff;
  background:
    radial-gradient(ellipse at 30% 20%, rgb(64 169 255 / 20%), transparent 60%),
    radial-gradient(ellipse at 75% 90%, rgb(11 77 153 / 40%), transparent 55%), linear-gradient(140deg, #0d2547 0%, #1a3d6e 100%);

  &::before {
    position: absolute;
    inset: 0;
    pointer-events: none;
    content: "";
    background-image:
      linear-gradient(rgb(255 255 255 / 4%) 1px, transparent 1px),
      linear-gradient(90deg, rgb(255 255 255 / 4%) 1px, transparent 1px);
    background-size: 32px 32px;
  }
}
.brand-eyebrow {
  position: relative;
  font-family: "JetBrains Mono", SFMono-Regular, Menlo, monospace;
  font-size: 11px;
  color: rgb(255 255 255 / 70%);
  text-transform: uppercase;
  letter-spacing: 0;
}
.brand-title {
  position: relative;
  margin-bottom: 14px;
  font-size: 32px;
  font-weight: 700;
  line-height: 1.3;
  letter-spacing: 0;

  span {
    display: block;
    white-space: nowrap;
  }
}
.brand-lede {
  position: relative;
  max-width: 360px;
  margin-bottom: auto;
  font-size: 14px;
  line-height: 1.7;
  color: rgb(255 255 255 / 82%);
}
.brand-features {
  position: relative;
  display: flex;
  flex-direction: column;
  gap: 12px;
  margin-top: 32px;

  .feature {
    display: flex;
    gap: 10px;
    align-items: center;
    font-size: 13px;
    color: rgb(255 255 255 / 88%);

    .ic {
      display: flex;
      flex-shrink: 0;
      align-items: center;
      justify-content: center;
      width: 22px;
      height: 22px;
      color: $primary-light;
      background: rgb(105 192 255 / 16%);
      border: 1px solid rgb(105 192 255 / 32%);
      border-radius: 6px;
    }
  }
}
.brand-foot {
  position: relative;
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  padding-top: 20px;
  margin-top: 28px;
  border-top: 1px solid rgb(255 255 255 / 16%);

  .chip {
    padding: 3px 9px;
    font-size: 11px;
    color: rgb(255 255 255 / 86%);
    letter-spacing: 0.3px;
    background: rgb(255 255 255 / 8%);
    border: 1px solid rgb(255 255 255 / 18%);
    border-radius: 99px;
  }
}

/* Form side */
.form-side {
  display: flex;
  flex-direction: column;
  justify-content: center;
  padding: 48px 48px 40px;
}
.form-head {
  margin-bottom: 8px;

  h1 {
    margin-bottom: 6px;
    font-size: 22px;
    font-weight: 600;
    color: var(--login-text);
  }
  p {
    font-size: 13px;
    color: var(--login-muted);
  }
}

/* Override Arco primary button inside login form — make it weighty + on-brand */
.form-side :deep(.arco-btn-primary) {
  height: 44px;
  font-size: 15px;
  font-weight: 600;
  letter-spacing: 4px;
  background: linear-gradient(135deg, #1890ff 0%, #096dd9 100%);
  border: none;
  border-radius: 8px;
  box-shadow:
    0 8px 20px rgb(24 144 255 / 32%),
    inset 0 1px 0 rgb(255 255 255 / 18%);
  transition:
    transform 0.15s ease,
    box-shadow 0.2s ease,
    background 0.2s ease;
}
.form-side :deep(.arco-btn-primary:hover) {
  background: linear-gradient(135deg, #40a9ff 0%, #1890ff 100%);
  box-shadow:
    0 12px 28px rgb(24 144 255 / 42%),
    inset 0 1px 0 rgb(255 255 255 / 22%);
  transform: translateY(-1px);
}
.form-side :deep(.arco-btn-primary:active) {
  background: linear-gradient(135deg, #096dd9 0%, #0050b3 100%);
  box-shadow: 0 4px 12px rgb(24 144 255 / 32%);
  transform: translateY(0);
}
.form-side :deep(.arco-btn-primary[disabled]),
.form-side :deep(.arco-btn-primary.arco-btn-loading) {
  opacity: 0.85;
  transform: none;
}

/* Page footer */
.page-foot {
  position: relative;
  z-index: 2;
  flex-shrink: 0;
  padding: 16px 24px 24px;
  font-size: 12px;
  color: var(--login-page-muted);
  text-align: center;

  .sep {
    margin: 0 8px;
    color: var(--login-page-muted);
  }
}

/* Responsive */
@media (width <= 880px) {
  .page-meta {
    height: 56px;
    padding: 0 24px;

    .version-text {
      display: none;
    }
  }
  .float-card {
    grid-template-columns: 1fr;
    width: 420px;
    min-height: 0;
  }
  .brand-side {
    padding: 64px 28px 32px;

    .brand-features,
    .brand-foot {
      display: none;
    }
  }
  .theme-toggle {
    color: #ffffff;
    background: rgb(255 255 255 / 8%);
    border-color: rgb(255 255 255 / 22%);

    &:hover {
      background: rgb(255 255 255 / 16%);
      border-color: rgb(255 255 255 / 40%);
    }
  }
  .form-side {
    padding: 32px 28px;
  }
}

@media (width <= 360px) {
  .brand-title {
    font-size: 24px;
  }
}
</style>
