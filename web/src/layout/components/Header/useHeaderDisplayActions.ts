import { storeToRefs } from "pinia";
import { onBeforeUnmount, onMounted, ref } from "vue";
import { useThemeConfig } from "@/store/modules/theme-config";
import { useThemeMethods } from "@/hooks/useThemeMethods";

export function useHeaderDisplayActions() {
  const { darkMode } = storeToRefs(useThemeConfig());
  const toggleThemeMode = () => {
    darkMode.value = !darkMode.value;
    const { setDarkMode } = useThemeMethods();
    setDarkMode();
  };

  const fullScreen = ref(!document.fullscreenElement);
  const syncFullScreen = () => {
    fullScreen.value = !document.fullscreenElement;
  };
  const onFullScreen = async () => {
    if (!document.fullscreenElement) {
      await document.documentElement.requestFullscreen().catch(() => undefined);
    } else if (document.exitFullscreen) {
      await document.exitFullscreen().catch(() => undefined);
    }
    syncFullScreen();
  };

  onMounted(() => document.addEventListener("fullscreenchange", syncFullScreen));
  onBeforeUnmount(() => document.removeEventListener("fullscreenchange", syncFullScreen));

  return { darkMode, toggleThemeMode, fullScreen, onFullScreen };
}
