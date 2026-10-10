import { createApp } from "vue";
import "@/style/index.scss";
import "@/styles/zlm-tokens.css"; // ZLM 控制台 design tokens(2026-06-28 重设计)
import App from "@/App.vue";

// vue-router
import router from "@/router/index";
// pinia
import pinia from "@/store/index";
// 注册全局svg
import "virtual:svg-icons-register";
// 引入自定义指令
import directives from "@/directives/index";

// 同步加载核心依赖
import ArcoVue from "@arco-design/web-vue";
import ArcoVueIcon from "@arco-design/web-vue/es/icon";
// import "@arco-design/web-vue/dist/arco.css"; // 默认样式
import "@arco-themes/vue-gi-demo/css/arco.css"; // 自定义主题
import "@/styles/arco-overrides.scss"; // UVP 全局样式覆盖
import i18n from "@/lang/index";
import { startSessionHeartbeat } from "@/services/session-heartbeat";

const app = createApp(App);
app.use(pinia);
app.use(router);
app.use(directives);

app.use(ArcoVue, {
  componentPrefix: "arco"
});
app.use(ArcoVueIcon);
app.use(i18n);

// 立即挂载应用，不等待非关键依赖加载
app.mount("#app");
startSessionHeartbeat();

// 使用requestIdleCallback在浏览器空闲时加载非关键依赖
const loadNonCriticalDependencies = () => {
  const loadAsync = async () => {
    try {
      // 异步加载字体
      await import("@/assets/fonts/fonts.scss");
      // 异步加载主题
      // ⚠️ 必须先补齐 vchart 内置 dark 主题基底，再初始化 arco 主题桥接：
      //    vchart 1.13 起，内置 dark 主题仅由 core 模块的注册副作用提供，而生产构建
      //    依据 package.json 的 sideEffects 白名单（只含 vchart-all/vchart-simple 等）
      //    会将 core 内的该副作用裁剪掉；arco 主题桥接包注册 arcoDesignDark 时需要
      //    dark 基底来合并 component 段（自身体积很小、不含 component），基底缺失
      //    会合成出残缺主题 → 深色下所有 VChart 图表一渲染就抛错、画布全空白。
      const { ThemeManager, darkTheme } = await import("@visactor/vchart");
      if (!ThemeManager.themeExist("dark")) {
        ThemeManager.registerTheme("dark", darkTheme);
      }
      const { initVChartArcoTheme } = await import("@visactor/vchart-arco-theme");
      initVChartArcoTheme();
    } catch (error) {
      console.warn("Non-critical dependencies loading failed:", error);
    }
  };

  // 使用requestIdleCallback优化加载时机，降级使用setTimeout
  if (typeof window !== "undefined" && "requestIdleCallback" in window) {
    // 浏览器空闲时加载非关键依赖
    window.requestIdleCallback(loadAsync);
  } else {
    setTimeout(loadAsync, 100);
  }
};

// 在应用挂载后加载非关键依赖
loadNonCriticalDependencies();
