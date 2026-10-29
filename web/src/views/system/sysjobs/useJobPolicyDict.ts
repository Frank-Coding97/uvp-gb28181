import { computed } from "vue";
import { useDictItems } from "@/hooks/useDictOptions";
import {
  DICT_CODE_JOB_BLOCKING_POLICY,
  DICT_CODE_JOB_EXECUTE_POLICY,
  JOB_BLOCKING_POLICY_LABEL_FALLBACK,
  JOB_EXECUTE_POLICY_LABEL_FALLBACK,
  jobPolicyLabelFrom,
  jobPolicyLabelsFrom,
  jobPolicyOptionsFromLabels
} from "./jobPolicy";

/** Vue 侧唯一出口：把 `job_execute_policy` 字典灌进纯函数。 */
export function useJobExecutePolicy() {
  const items = useDictItems(DICT_CODE_JOB_EXECUTE_POLICY);
  const labels = computed(() => jobPolicyLabelsFrom(items.value, JOB_EXECUTE_POLICY_LABEL_FALLBACK));
  const options = computed(() => jobPolicyOptionsFromLabels(labels.value, JOB_EXECUTE_POLICY_LABEL_FALLBACK));
  return { labels, options, label: (value: unknown) => jobPolicyLabelFrom(labels.value, value) };
}

/** Vue 侧唯一出口：把 `job_blocking_policy` 字典灌进纯函数。 */
export function useJobBlockingPolicy() {
  const items = useDictItems(DICT_CODE_JOB_BLOCKING_POLICY);
  const labels = computed(() => jobPolicyLabelsFrom(items.value, JOB_BLOCKING_POLICY_LABEL_FALLBACK));
  const options = computed(() => jobPolicyOptionsFromLabels(labels.value, JOB_BLOCKING_POLICY_LABEL_FALLBACK));
  return { labels, options, label: (value: unknown) => jobPolicyLabelFrom(labels.value, value) };
}
