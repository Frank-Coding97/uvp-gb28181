import { computed } from "vue";
import { useDictItems } from "@/hooks/useDictOptions";
import {
  DICT_CODE_LOGIN_FAILURE_REASON,
  loginFailureReasonLabelFrom,
  loginFailureReasonLabelsFrom,
  loginFailureReasonOptionsFromLabels
} from "./loginFailureReason";

/** Vue 侧唯一出口：把 `login_failure_reason` 字典灌进纯函数。 */
export function useLoginFailureReason() {
  const items = useDictItems(DICT_CODE_LOGIN_FAILURE_REASON);
  const labels = computed(() => loginFailureReasonLabelsFrom(items.value));
  const options = computed(() => loginFailureReasonOptionsFromLabels(labels.value));
  return {
    labels,
    options,
    label: (reason?: string | null) => loginFailureReasonLabelFrom(labels.value, reason)
  };
}
