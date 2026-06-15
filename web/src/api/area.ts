import { http } from "@/utils/http";

/**
 * 地区数据项接口
 */
export interface AreaItem {
  /** 地区编码 */
  value: string;
  /** 地区名称 */
  label: string;
  /** 级别（1:省/直辖市, 2:市, 3:区/县） */
  level: string;
  /** 父级编码 */
  parent: string;
  /** 子级地区 */
  children?: AreaItem[];
}

/**
 * 地区数据结果类型
 */
export type AreaResult = AreaItem[];

// 内存缓存：已加载的地区数据
let areaDataCache: AreaItem[] | null = null;

// Promise 缓存：正在进行的请求
let areaDataPromise: Promise<AreaItem[]> | null = null;

/**
 * 获取地区数据
 * 从服务器获取地区数据，并实现缓存机制避免重复请求
 * 
 * @returns Promise<AreaItem[]> 地区数据数组
 */
export async function getAreaData(): Promise<AreaItem[]> {
  // 如果已有缓存，直接返回
  if (areaDataCache) {
    return areaDataCache;
  }

  // 如果正在请求，返回同一个 Promise（避免并发请求）
  if (areaDataPromise) {
    return areaDataPromise;
  }

  // 发起请求并缓存 Promise
  areaDataPromise = fetchAreaDataFromServer()
    .then((data) => {
      areaDataCache = data;
      areaDataPromise = null; // 请求完成后清除 Promise 缓存
      return data;
    })
    .catch((error) => {
      areaDataPromise = null; // 请求失败后清除 Promise 缓存
      throw error;
    });

  return areaDataPromise;
}

/**
 * 从服务器获取地区数据
 * 
 * @returns Promise<AreaItem[]> 地区数据数组
 */
async function fetchAreaDataFromServer(): Promise<AreaItem[]> {
  const url = import.meta.env.VITE_APP_BASE_URL + "/public/area/area.json";
  
  try {
    const data = await http.request<AreaItem[]>("get", url);
    return data;
  } catch (error) {
    console.error("获取地区数据失败:", error);
    throw error;
  }
}

/**
 * 根据地区编码路径查找地区信息
 * 
 * @param areaData 地区数据数组
 * @param path 地区编码路径数组（如 ["11", "1101", "110101"]）
 * @returns AreaItem[] 匹配的地区信息数组
 */
export function findAreaByPath(
  areaData: AreaItem[],
  path: string[]
): AreaItem[] {
  const result: AreaItem[] = [];
  let currentLevel = areaData;

  for (const code of path) {
    const found = currentLevel.find((item) => item.value === code);
    if (found) {
      result.push(found);
      currentLevel = found.children || [];
    } else {
      break;
    }
  }

  return result;
}

/**
 * 清除地区数据缓存
 * 用于强制重新加载数据
 */
export function clearAreaDataCache(): void {
  areaDataCache = null;
  areaDataPromise = null;
}
