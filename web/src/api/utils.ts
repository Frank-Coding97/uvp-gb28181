
// 是否开启本地mock, 可根据该配置调整url
// const MOCK_FLAG = import.meta.env.VITE_APP_OPEN_MOCK === "true";

const appBaseUrl = import.meta.env.VITE_APP_BASE_URL || "";

export const baseUrlApi = (url: string) => `${appBaseUrl}/api/${url}`;
// 获取基础API URL
export const getBaseUrl = () => appBaseUrl;
