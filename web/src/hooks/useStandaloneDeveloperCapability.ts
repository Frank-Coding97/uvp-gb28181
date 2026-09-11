import { computed, ref } from "vue";
import { loadStandaloneSetupStatus, refreshStandaloneSetupStatus, type StandaloneSetupProbe } from "@/api/standalone-setup";

export type StandaloneDeveloperCapabilityState = "checking" | "supported" | "unsupported" | "unavailable";

function stateFromProbe(probe: StandaloneSetupProbe): StandaloneDeveloperCapabilityState {
  if (probe.kind === "legacy") return "supported";
  if (probe.kind === "standalone") return "unsupported";
  return "unavailable";
}

export function useStandaloneDeveloperCapability() {
  const state = ref<StandaloneDeveloperCapabilityState>("checking");
  const loading = ref(false);
  const canUse = computed(() => state.value === "supported");
  let requestId = 0;

  async function probe(loader: () => Promise<StandaloneSetupProbe>): Promise<StandaloneSetupProbe> {
    const currentRequest = ++requestId;
    loading.value = true;
    try {
      const result = await loader();
      if (currentRequest === requestId) state.value = stateFromProbe(result);
      return result;
    } catch {
      const result: StandaloneSetupProbe = { kind: "unavailable" };
      if (currentRequest === requestId) state.value = "unavailable";
      return result;
    } finally {
      if (currentRequest === requestId) loading.value = false;
    }
  }

  function load() {
    return probe(loadStandaloneSetupStatus);
  }

  function retry() {
    return probe(refreshStandaloneSetupStatus);
  }

  return { state, loading, canUse, load, retry };
}
