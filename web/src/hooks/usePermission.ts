import { useUserStoreHook } from "@/store/modules/user";

/**
 * 全平台**唯一**的权限判定入口。
 *
 * ⭐ 为什么要有这个文件：改造前有 20+ 个页面各自抄了一份一模一样的实现
 *    （`const hasPermission = (permission: string) => permissions.value.includes("*:*:*") || ...`），
 *    一旦通配规则或权限来源要改，就得全仓找一遍。收敛到这里以后，
 *    「通配符 `*:*:*` 放行」「从哪个 store 字段读」这两件事只有一处定义。
 *
 * ⚠️ 这是**普通函数**，不是 `computed`：调用点自己决定要不要包 `computed`
 *    （绝大多数是 `const canXxx = computed(() => hasPermission("xxx"))`）。
 *    函数体每次调用都现读 store，所以包在 `computed` / 模板里同样是响应式的。
 */

/** 超级管理员通配权限：持有它即视为拥有一切权限。 */
const ALL_PERMISSION = "*:*:*";

/**
 * 当前账号是否持有指定权限点。
 * @param permission 权限标识（如 `gb28181:device:add`）
 */
export function hasPermission(permission: string): boolean {
  const { permissions = [] } = useUserStoreHook().account;
  return permissions.includes(ALL_PERMISSION) || permissions.includes(permission);
}

/**
 * 是否命中其中**任一**权限（多个权限码之间是「或」关系）。
 * @param required 权限标识列表
 */
export function hasAnyPermission(required: string[]): boolean {
  return required.some(permission => hasPermission(permission));
}
