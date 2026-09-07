<script setup lang="ts">
import { onBeforeUnmount, ref, watch } from "vue";
import { useRouter } from "vue-router";
import { refreshStandaloneSetupStatus } from "@/api/standalone-setup";
import SipSetupHost from "@/layout/components/SipSetupHost.vue";
import { useSipSetupStore } from "@/store/modules/sip-setup";

const router = useRouter();
const setupStore = useSipSetupStore();
const phaseError = ref("");
let modalWasOpen = false;
let phaseRefreshInFlight = false;

async function handleModalClosed() {
  if (phaseRefreshInFlight) return;
  phaseRefreshInFlight = true;
  phaseError.value = "";
  try {
    const probe = await refreshStandaloneSetupStatus();
    if (probe.kind === "standalone" && probe.status.phase === "complete") {
      await router.replace("/home");
      return;
    }
    if (probe.kind === "standalone" && probe.status.phase === "pending_sip") {
      setupStore.openModal();
      return;
    }
    phaseError.value = "暂时无法确认 SIP 配置状态，请稍后刷新页面";
  } catch {
    phaseError.value = "暂时无法确认 SIP 配置状态，请稍后刷新页面";
  } finally {
    phaseRefreshInFlight = false;
  }
}

const stopModalWatch = watch(
  () => setupStore.modalOpen,
  open => {
    if (open) {
      modalWasOpen = true;
      return;
    }
    if (modalWasOpen) void handleModalClosed();
  }
);

onBeforeUnmount(stopModalWatch);
</script>

<template>
  <main class="standalone-sip-setup-page" data-testid="standalone-sip-setup-page">
    <section class="standalone-sip-setup-card" aria-labelledby="standalone-sip-setup-title">
      <header class="standalone-sip-setup-header">
        <span class="standalone-sip-setup-eyebrow">LOCAL INSTALLATION</span>
        <h1 id="standalone-sip-setup-title">配置 SIP 服务</h1>
        <p>完成 SIP 参数配置并启动服务后，才能进入控制台。</p>
      </header>
      <p v-if="phaseError" class="standalone-sip-setup-error" role="alert">{{ phaseError }}</p>
      <SipSetupHost />
    </section>
  </main>
</template>

<style scoped>
.standalone-sip-setup-page {
  box-sizing: border-box;
  min-height: 100vh;
  display: grid;
  place-items: center;
  padding: 32px 20px;
  color: var(--uvp-text-primary, #1d2129);
  background: radial-gradient(circle at 20% 10%, rgba(var(--primary-1), 0.8), transparent 42%), var(--uvp-shell-bg, #f2f3f5);
}

.standalone-sip-setup-card {
  box-sizing: border-box;
  width: min(820px, 100%);
  min-height: 280px;
  padding: 36px 42px;
  border: 1px solid var(--uvp-workspace-border, #e5e6eb);
  border-radius: 16px;
  background: var(--uvp-workspace-bg, #fff);
  box-shadow: var(--uvp-workspace-shadow, 0 18px 50px rgb(29 33 41 / 12%));
}

.standalone-sip-setup-header {
  max-width: 680px;
}

.standalone-sip-setup-eyebrow {
  color: var(--uvp-brand, #2563eb);
  font-size: 11px;
  font-weight: 700;
  letter-spacing: 0.16em;
}

.standalone-sip-setup-header h1 {
  margin: 10px 0 8px;
  font-size: clamp(24px, 4vw, 34px);
}

.standalone-sip-setup-header p {
  margin: 0;
  color: var(--uvp-text-secondary, #4e5969);
  font-size: 14px;
}

.standalone-sip-setup-error {
  margin: 22px 0 0;
  padding: 12px 14px;
  border: 1px solid var(--uvp-danger-border, #f5c2c7);
  border-radius: 10px;
  color: var(--uvp-danger, #d03050);
  background: var(--uvp-danger-soft, #fff1f0);
}
</style>
