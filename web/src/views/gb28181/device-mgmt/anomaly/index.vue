<script setup lang="ts">
/**
 * 异常治理页占位(tasks §3 C1)
 *
 * 完整列表 + 一键改类型/改挂载 在 B4 controllers 已就绪,
 * 但前端 UX 表格 + 批量操作 + 改类型弹窗等留在 P1.C 后期 / Phase 2 完整化。
 *
 * 本期占位:加载未处理 anomaly 数 + 标题。
 */
import { ref, onMounted } from "vue";
import { getAnomalyCount } from "../api/catalog";

const count = ref<number | null>(null);
const loading = ref(false);
const err = ref<string | null>(null);

onMounted(async () => {
  loading.value = true;
  try {
    const r = await getAnomalyCount();
    count.value = r?.data?.count ?? 0;
  } catch (e) {
    err.value = e instanceof Error ? e.message : "加载失败";
  } finally {
    loading.value = false;
  }
});
</script>

<template>
  <div class="dm-anomaly-page">
    <header class="dm-anomaly-page__head">
      <h2>目录异常治理</h2>
      <p class="hint">
        未处理:
        <span class="count" v-if="!loading && !err">{{ count }}</span>
        <span v-else-if="loading" class="muted">加载中…</span>
        <span v-else class="muted">{{ err }}</span>
      </p>
    </header>
    <section class="dm-anomaly-page__body">
      <p class="muted">完整列表 / 批量改类型 / 改挂载 UI 留下次 task(B4 后端已就绪)。</p>
    </section>
  </div>
</template>

<style lang="scss" scoped>
@use "@/style/var/index.scss" as *;

.dm-anomaly-page {
  padding: var(--space-6);
  height: 100%;
  overflow: auto;

  &__head {
    margin-bottom: var(--space-6);

    h2 {
      font-size: var(--font-20);
      color: var(--color-text-1);
      margin-bottom: var(--space-2);
    }
  }

  .count {
    color: var(--status-warning);
    font-weight: 600;
    font-family: var(--font-mono);
    margin-left: var(--space-1);
  }

  .muted {
    color: var(--color-text-4);
  }

  .hint {
    color: var(--color-text-3);
    font-size: var(--font-13);
  }
}
</style>
