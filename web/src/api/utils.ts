const appBaseUrl = import.meta.env.VITE_APP_BASE_URL || "";

export const baseUrlApi = (url: string) => `${appBaseUrl}/api/${url}`;
// 获取基础API URL
export const getBaseUrl = () => appBaseUrl;
