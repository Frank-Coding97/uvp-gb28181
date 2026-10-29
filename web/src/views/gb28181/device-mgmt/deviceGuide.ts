import type { GuideStepInput } from "@/components/s-guide/guide";

/**
 * 设备列表（device-mgmt）的新手引导步骤。
 *
 * 为什么单独成文件而不是写在 index.vue 里：index.vue 已经 8700 行，
 * 文案又是最常改的东西（产品/老板一句话就得动）。单独放一份，改文案不用在
 * 巨型 SFC 里翻；步骤内容也才好被测试直接引用。
 *
 * ⛔ 锚点全部走 `data-guide="xxx"` 而不是 class：
 *   页面右侧那堆 `.btn-primary` / `.toolbar-select` 是样式类，改版时最先被重命名；
 *   `data-guide` 只服务这套引导，语义唯一、不会误命中别的元素。
 * ⛔⛔ 删/改编号时**必须同步 index.vue 里的 data-guide 属性**，两边对不上时
 *    `buildGuideSteps` 会把该步静默丢掉（不报错），表现为「引导里少讲了一块」。
 * ⚠️ `view-list` / `view-card` / `view-map` 三个锚点是**模板里按 `viewOptions` 循环生成的**
 *    （`:data-guide="\`view-${item.value}\`"`），grep `index.vue` 搜不到字面量；
 *    要核对就去 `viewOptions` 里看 `value`（只有 list / card / map 三个）。
 */

/** 版本号进 key：引导内容大改（增删核心步骤）时改版本，老用户才会重新看一遍。 */
export const DEVICE_GUIDE_STORAGE_KEY = "uvp.gb28181.device-mgmt.guide.v1";

/**
 * 说明列里的按钮图例。
 *
 * 用 HTML 而不是纯文本：7 个按钮挤成一段自然语言没人读得下去，做成「键 + 释义」
 * 两列才扫得动。样子在 `src/style/model/uvp-guide.scss` 的 `uvp-guide-legend` 段。
 *
 * ⚠️ 列表视图是**文字链接**、卡片视图是**图标按钮**，两者的排列顺序不同；
 * 所以这里按「用途」讲、不按位置讲，两种视图看到的内容都对得上。
 */
const ROW_ACTIONS_LEGEND = `
<div class="uvp-guide-legend">
  <div class="uvp-guide-legend__row"><span class="uvp-guide-legend__key">通道</span><span class="uvp-guide-legend__desc">看它底下的通道，点播回放从这里进</span></div>
  <div class="uvp-guide-legend__row"><span class="uvp-guide-legend__key">刷新</span><span class="uvp-guide-legend__desc">让设备重新上报一遍目录；设备离线时点不动</span></div>
  <div class="uvp-guide-legend__row"><span class="uvp-guide-legend__key">订阅</span><span class="uvp-guide-legend__desc">管理报警、目录变更等订阅关系</span></div>
  <div class="uvp-guide-legend__row"><span class="uvp-guide-legend__key">详情</span><span class="uvp-guide-legend__desc">设备全貌，配置下发也在这个入口里</span></div>
  <div class="uvp-guide-legend__row"><span class="uvp-guide-legend__key">编辑</span><span class="uvp-guide-legend__desc">改名称、归属、经纬度等台账信息</span></div>
  <div class="uvp-guide-legend__row"><span class="uvp-guide-legend__key">删除</span><span class="uvp-guide-legend__desc">只从平台移除，不影响设备本身</span></div>
  <div class="uvp-guide-legend__row"><span class="uvp-guide-legend__key">维护</span><span class="uvp-guide-legend__desc">固件升级、设备重启、维护记录</span></div>
</div>
`;

/**
 * 「双击设备」那一步的高亮范围。
 *
 * ⛔ 锚点只能挂在设备名称那一格上，但真正的双击目标是**整行**（Arco 渲染出来的 `tr`）——
 *    只框住名字会让人以为「必须点在名字上」，所以往上找最近的行。
 *    卡片视图里没有 `tr`，`closest` 返回 null，自然退回卡片本身。
 */
function deviceRowTarget(): Element | null {
  const anchor = document.querySelector('[data-guide="dblclick-device"]');
  // `tr` 是 Arco 表格的行；`.arco-table-tr` 是它的行类名，两种情况都兜住。
  return anchor?.closest("tr, .arco-table-tr") ?? anchor ?? null;
}

export const DEVICE_GUIDE_STEPS: readonly GuideStepInput[] = [
  {
    target: '[data-guide="view-list"]',
    title: "列表：字段全，方便横着比",
    description:
      "编码、IP、状态、通道数并排一行，核对台账、查「哪台没上线」最快。" + "每行操作是文字链接，一列看下来不容易点错。",
    side: "bottom",
    align: "start"
  },
  {
    target: '[data-guide="view-card"]',
    title: "卡片：找画面最快",
    description: "每张卡片带一路缩略图和状态点，扫一眼就知道画面正不正常。" + "刚进这页默认就是它，看到目标再切列表看字段。",
    side: "bottom",
    align: "center"
  },
  {
    target: '[data-guide="view-map"]',
    title: "地图：只认填过经纬度的通道",
    description:
      "点位视角。左侧那棵树会换成「可定位通道树」——<b>没采过点的设备和通道在图上找不到，不是丢了</b>。" +
      "地图里也没有每行那一排按钮，是另一套交互。",
    side: "bottom",
    align: "end"
  },
  {
    target: '[data-guide="asset-device"]',
    title: "设备：管的是主机",
    description:
      "一台主机（硬盘录像机、摄像机主机）。台账、目录同步、配置下发、固件升级都在这层做。" +
      "但它本身不出画面 —— 想看视频要下钻到通道。",
    side: "bottom",
    align: "start"
  },
  {
    target: '[data-guide="asset-channel"]',
    title: "通道：管的是每一路视频",
    description:
      "设备底下的每一路视频。点播、回放、抓图、云台都作用在通道上。" + "某一路找不到时，回「设备」看它的目录有没有同步上来。",
    side: "bottom",
    align: "end"
  },
  {
    target: '[data-guide="directory"]',
    title: "左边的树：按组织或分组找设备",
    description:
      "行政区、部门、业务三种目录，还有自定义分组。点一个节点，右边列表和上面的在线率就一起只看这个范围。" +
      "找不到设备时，先确认它<b>有没有归到这个目录下</b>。",
    side: "right"
  },
  {
    target: '[data-guide="row-actions"]',
    title: "每行的按钮分别是什么意思",
    description:
      "列表视图是文字链接，卡片视图是图标按钮，用途一样。没有权限的按钮会置灰，" +
      "鼠标停上去会告诉你缺哪个权限。" +
      ROW_ACTIONS_LEGEND,
    side: "left",
    align: "center"
  },
  {
    target: deviceRowTarget,
    title: "双击设备，直接穿透到它的通道",
    description:
      "列表视图双击任意一行、卡片视图双击任意一张卡，就会切到<b>通道</b>并只看这台设备的通道 —— " +
      "比先点「通道」再按设备筛快一步。双击通道那一行则是直接点播。",
    side: "bottom",
    align: "start"
  }
];
