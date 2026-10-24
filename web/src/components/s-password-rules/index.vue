<script setup lang="ts">
/**
 * 密码「格式提示」= 强度条 + 规则清单（满足打对号）。
 *
 * 用法（清单数据由调用方算，本组件只负责渲染，因此平台账号策略与 SIP 接入策略都能用）：
 *   <s-password-rules :rules="rules" :filled="Boolean(password)" testid="xx-rule-list" />
 *
 * ⛔ 这是「密码格式提示」的唯一实现，消费方：平台接入向导 → 身份信息 → SIP 密码。
 * ⛔⛔ 登录页「修改初始密码」弹窗目前**仍持有同款副本**（`login/components/InitialPasswordDialog.vue`）：
 *     它的用例用 `readFileSync` 对源码字符串断言（`password-rules` / `password-rules__mark` /
 *     `data-rule="confirm-match"`），迁移必须同时改那些断言 ⇒ 未经确认别顺手迁。
 *     真要迁：DOM 级断言（`findAll('[data-testid=...] .password-rules__item')`）可原样复用，
 *     只有两处 `expect(source).toContain(...)` 需要改成渲染结果断言。
 */
import { computed } from "vue";
import { Check } from "lucide-vue-next";

const props = withDefaults(
  defineProps<{
    /** 规则清单：key 既是 v-for 的 :key，也渲染成 data-rule 供测试定位。 */
    rules: Array<{ key: string; label: string; satisfied: boolean }>;
    /** 是否渲染上方强度条（段数 = 规则条数）。单条清单（如「两次密码一致」）传 false。 */
    showStrength?: boolean;
    /** 强度条右侧文案；不传则按 filled / 满足条数自动推导。 */
    strengthLabel?: string;
    /** 密码框是否已输入内容 —— 只参与强度条文案的自动推导。 */
    filled?: boolean;
    /** 清单容器的 data-testid。 */
    testid?: string;
  }>(),
  { showStrength: true, strengthLabel: "", filled: false, testid: undefined }
);

const satisfiedCount = computed(() => props.rules.filter(rule => rule.satisfied).length);
const resolvedLabel = computed(() => {
  if (props.strengthLabel) return props.strengthLabel;
  if (props.rules.length > 0 && satisfiedCount.value === props.rules.length) return "符合要求";
  return props.filled ? "继续完善" : "请输入密码";
});
</script>

<template>
  <div class="s-password-rules">
    <div v-if="showStrength" class="password-strength" aria-live="polite">
      <span
        v-for="rule in rules"
        :key="`bar-${rule.key}`"
        class="password-strength__segment"
        :class="{ 'is-satisfied': rule.satisfied }"
      />
      <span class="password-strength__label">{{ resolvedLabel }}</span>
    </div>
    <ul class="password-rules" :data-testid="testid">
      <li
        v-for="rule in rules"
        :key="rule.key"
        class="password-rules__item"
        :class="{ 'is-satisfied': rule.satisfied }"
        :data-rule="rule.key"
      >
        <span class="password-rules__mark" aria-hidden="true">
          <Check v-if="rule.satisfied" :size="11" :stroke-width="3.5" />
        </span>
        <span>{{ rule.label }}</span>
      </li>
    </ul>
  </div>
</template>

<style scoped>
.s-password-rules {
  width: 100%;
  min-width: 0;
}

.password-strength {
  display: flex;
  gap: 6px;
  align-items: center;
  margin-top: 8px;
}

.password-strength__segment {
  flex: 1;
  height: 4px;
  background: var(--color-fill-3);
  border-radius: 999px;
  transition: background-color 0.18s ease;
}

.password-strength__segment.is-satisfied {
  background: #16a34a;
}

.password-strength__label {
  flex-shrink: 0;
  min-width: 52px;
  font-size: 12px;
  color: var(--color-text-3);
  text-align: right;
  white-space: nowrap;
}

.password-rules {
  display: flex;
  flex-direction: column;
  gap: 6px;
  padding: 0;
  margin: 10px 0 0;
  list-style: none;
}

.password-rules__item {
  display: flex;
  gap: 8px;
  align-items: center;
  font-size: 12px;
  line-height: 18px;
  color: var(--color-text-3);
  transition: color 0.18s ease;
}

.password-rules__item.is-satisfied {
  color: var(--color-text-2);
}

.password-rules__mark {
  display: inline-flex;
  flex-shrink: 0;
  align-items: center;
  justify-content: center;
  width: 14px;
  height: 14px;
  color: #ffffff;
  border: 1px solid var(--color-fill-4);
  border-radius: 50%;
  transition:
    background-color 0.18s ease,
    border-color 0.18s ease;
}

.password-rules__item.is-satisfied .password-rules__mark {
  background: #16a34a;
  border-color: #16a34a;
}
</style>
