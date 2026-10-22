#!/usr/bin/env node
// 从 README.md（GitHub 主版）派生 README_zh.md（Gitee 展示版）。
//
// 为什么需要两份 README：
//   Gitee 的帮助文档写明了 README 的选取规则——"若有多个 README 文件，则优先显示
//   文件名包含 _zh / _cn 等中文标注的 README"。而 GitHub 只渲染 README.md。
//   于是可以做到：GitHub 看 README.md（保留完整联系方式与赞助入口），
//   Gitee 看 README_zh.md（去掉会被平台自动审核拦下的内容）。
//
// 为什么必须脚本生成、不能手工维护两份：
//   两份 95% 相同，剩下 5% 是联系方式/免责声明。手工维护的话，改功能章节时必然
//   只改一份，表现是**静默**的——GitHub 与 Gitee 长期展示不一致，谁都不会发现。
//   所以单一来源锁定为 README.md，README_zh.md 是产物，禁止手改。
//
// 为什么替换规则必须"命中且仅命中一次"：
//   README.md 改得对不上规则时，如果只是跳过，Gitee 版会悄悄保留敏感内容，
//   等平台再次拦了才发现。所以命中次数不等于 1 就直接报错退出。
//
// 用法：
//   node scripts/sync-gitee-readme.mjs          # 重新生成 README_zh.md
//   node scripts/sync-gitee-readme.mjs --check   # 只校验是否同步（质量门禁用）

import { readFile, writeFile } from 'node:fs/promises'
import { existsSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { dirname, resolve } from 'node:path'

const ROOT = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const SOURCE = resolve(ROOT, 'README.md')
const TARGET = resolve(ROOT, 'README_zh.md')

/**
 * 每条规则把 README.md 里的一段"完整替换"为目标文本。
 * find 必须是 README.md 中**逐字**存在的片段（含全角引号、行尾空格），
 * 且全文中只出现一次，否则视为规则失效。
 */
const RULES = [
  {
    label: '顶部 logo：<picture> 双主题 + GitHub raw 直链 → 单张本地相对路径',
    find: `<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="https://raw.githubusercontent.com/Frank-Coding97/uvp-gb28181/develop/web/src/assets/logo/uvp-wordmark-light.svg">
    <img referrerpolicy="no-referrer" src="https://raw.githubusercontent.com/Frank-Coding97/uvp-gb28181/develop/web/src/assets/logo/uvp-full.svg" alt="UVP 统一视频接入平台" width="340">
  </picture>
</p>`,
    replace: `<!-- 本文件由 scripts/sync-gitee-readme.mjs 从 README.md 生成，请勿手工编辑。 -->
<!-- Gitee 优先渲染文件名带 _zh 的 README，GitHub 只渲染 README.md，因此两份内容分开维护。 -->

<p align="center">
  <img src="web/src/assets/logo/uvp-full.svg" alt="UVP 统一视频接入平台" width="340">
</p>`,
  },
  {
    label: '目录：摘掉「请作者喝杯咖啡」入口',
    find: `- [联系我](#联系我)
- [请作者喝杯咖啡](#请作者喝杯咖啡)`,
    replace: `- [联系我](#联系我)`,
  },
  {
    label: '项目背景·交流与合作：去掉商业服务招揽',
    find: '坦率地说，做这个项目也有一点自己的“私心”：我希望通过 UVP-GB28181，让更多人看到我在视频接入领域的专业能力和长期积累。如果你的团队有 GB/T 28181 平台二次开发、设备与协议适配、视频系统集成、私有化部署、疑难问题排查或其他软件开发需求，也欢迎与我交流合作。我希望能够用自己的专业经验，为项目提供真正可落地、可维护的技术服务。',
    replace: '坦率地说，做这个项目也有一点自己的“私心”：我希望通过 UVP-GB28181，让更多人看到我在视频接入领域的专业能力和长期积累。项目相关的技术讨论、方案探讨和问题反馈，都欢迎在 [Issue](https://github.com/Frank-Coding97/uvp-gb28181/issues) 区一起交流，这样讨论的过程也能被后来的人检索到。',
  },
  {
    label: '法律声明：开篇"监控授权"→"使用授权"',
    find: '> 本项目是**技术工具**，不是监控授权，也不是合规背书。',
    replace: '> 本项目是**技术工具**，不是使用授权，也不是合规背书。',
  },
  {
    label: '法律声明 1：法条名与"员工监控/未成年人隐私"改为中性表述',
    find: '**1. 合法使用是使用者的义务。** 使用本项目必须遵守所在国家或地区的法律法规，包括但不限于《网络安全法》《数据安全法》《个人信息保护法》《治安管理处罚法》以及视频监控、数据留存、跨境的行业监管要求。涉及公共区域监控、员工监控、未成年人或他人隐私的场景，应自行完成合规评估、备案与告知义务。',
    replace: '**1. 合法使用是使用者的义务。** 使用本项目必须遵守所在国家或地区的法律法规，包括但不限于网络与数据安全、个人信息保护方面的相关规定，以及视频数据留存、跨境传输的行业监管要求。在涉及公共场所、办公场所或他人权益的场景中部署，应自行完成合规评估、备案与告知义务。',
  },
  {
    label: '法律声明 2：负面词枚举（偷拍/窥探/敲诈/倒卖…）收敛为一句',
    find: '**2. 禁止用于侵犯他人权益的行为。** 严禁将本项目用于非法侵入他人网络或设备、未经授权接入或查看他人摄像头、偷拍偷录、窥探隐私、非法跟踪定位，以及任何形式的敲诈、骚扰或数据倒卖。作者不参与、不支持、也不为上述行为提供任何协助。',
    replace: '**2. 禁止用于侵犯他人权益的行为。** 严禁将本项目用于任何未经授权的设备接入、数据获取或隐私侵害行为。作者不参与、不支持、也不为上述行为提供任何协助。',
  },
  {
    label: '文末：删除微信名片与「请作者喝杯咖啡」整节（含两张收款码）',
    find: `遇到 Bug、想要新功能、做平台兼容性反馈，欢迎提 [Issue](https://github.com/Frank-Coding97/uvp-gb28181/issues) 或加微信交流。如果你的团队有国标平台二次开发、设备与协议适配、私有化部署或疑难问题排查的需求，也可以直接找我。

<p align="center">
  <img referrerpolicy="no-referrer" src="https://raw.githubusercontent.com/Frank-Coding97/uvp-gb28181/develop/docs/contact/wechat-card.jpg" alt="微信名片" width="280">
</p>

## 请作者喝杯咖啡

如果 UVP-GB28181 帮你节省了时间，或者在现场解决过一个让人头疼的老问题，欢迎请作者喝杯咖啡。

<table>
<tr>
<td align="center" width="50%"><b>微信</b><br/><img referrerpolicy="no-referrer" src="https://raw.githubusercontent.com/Frank-Coding97/uvp-gb28181/develop/docs/contact/wechat-pay.jpg" alt="微信收款码" width="280"/></td>
<td align="center" width="50%"><b>支付宝</b><br/><img referrerpolicy="no-referrer" src="https://raw.githubusercontent.com/Frank-Coding97/uvp-gb28181/develop/docs/contact/alipay-pay.jpg" alt="支付宝收款码" width="280"/></td>
</tr>
</table>

打赏完全自愿，不影响任何功能、许可证或技术支持。也欢迎用不花钱的方式支持：点个 Star、提一个 Issue、把这套平台推荐给身边做国标接入的同行，同样很有帮助。`,
    replace: `遇到 Bug、想要新功能、做平台兼容性反馈，欢迎提 [Issue](https://github.com/Frank-Coding97/uvp-gb28181/issues)。项目相关的技术问题会在 Issue 区一起讨论，也方便后来的人检索到同类问题的处理过程。

如果这个项目对你有帮助，欢迎点个 Star、提一个 Issue，或者把它推荐给身边做国标接入的同行，这些支持同样很有帮助。`,
  },
]

function applyRules(content) {
  let out = content
  const failures = []

  for (const rule of RULES) {
    const hits = out.split(rule.find).length - 1
    if (hits !== 1) {
      failures.push(`规则「${rule.label}」命中 ${hits} 次（应为 1 次）`)
      continue
    }
    out = out.replace(rule.find, rule.replace)
  }

  if (failures.length > 0) {
    console.error('[readme-sync] README.md 与本脚本的替换规则已对不上：')
    for (const item of failures) console.error(`  · ${item}`)
    console.error('  处理方式：确认 README.md 的改动是否应该带进 Gitee 版，')
    console.error('            需要带的话同步更新本脚本 RULES 里的 find 文本。')
    process.exit(1)
  }

  return out
}

async function main() {
  const checkOnly = process.argv.includes('--check')
  const source = await readFile(SOURCE, 'utf8')
  const generated = applyRules(source)

  if (checkOnly) {
    if (!existsSync(TARGET)) {
      console.error('[readme-sync] 缺少 README_zh.md，请运行 node scripts/sync-gitee-readme.mjs')
      process.exit(1)
    }
    const current = await readFile(TARGET, 'utf8')
    if (current !== generated) {
      console.error('[readme-sync] README_zh.md 与 README.md 不同步。')
      console.error('  README.md 改过之后需要重新生成：node scripts/sync-gitee-readme.mjs')
      process.exit(1)
    }
    console.log('[readme-sync] README_zh.md 已与 README.md 同步。')
    return
  }

  await writeFile(TARGET, generated, 'utf8')
  const bytes = Buffer.byteLength(generated, 'utf8')
  console.log(`[readme-sync] 已生成 README_zh.md（${generated.split('\n').length} 行 / ${bytes} 字节）`)
}

await main()
