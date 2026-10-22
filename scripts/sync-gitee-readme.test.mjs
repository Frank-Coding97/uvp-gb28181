// Gitee 展示版 README 的「结果断言」。
//
// 为什么除了生成脚本还要一个测试：
//   scripts/sync-gitee-readme.mjs 的替换规则只能保证「规则命中了」，
//   它管不住「规则写得对不对」。如果哪天有人往 README.md 里加了一段新的
//   二维码/招揽文案，却忘了给生成脚本补规则，Gitee 版会把这段原样带过去，
//   表现是：推上去几小时后 Gitee 首页又变成「内容可能含有违规信息」——
//   而本地一切都绿。所以这里按**结果**再守一道。
//
// ⛔ 本测试只读文本，不依赖任何第三方包，任何环境都能跑。

import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import test from 'node:test';

const ROOT = join(dirname(fileURLToPath(import.meta.url)), '..');
const read = (rel) => readFileSync(join(ROOT, rel), 'utf8');

const GITEE_README = read('README_zh.md');
const GITHUB_README = read('README.md');

/**
 * Gitee 的内容审核会拦「以推广曝光变现为目的」的导流，以及和解责声明无关的
 * 违法风险词。这些词一旦出现在 Gitee 展示版里，就可能整篇被隐藏。
 */
const FORBIDDEN_IN_GITEE = [
  '微信',
  '收款码',
  '打赏',
  '喝杯咖啡',
  '偷拍',
  '偷录',
  '窥探',
  '敲诈',
  '数据倒卖',
];

test('Gitee 版 README 不含会触发平台审核的变现与引流文案', () => {
  const hits = FORBIDDEN_IN_GITEE.filter((word) => GITEE_README.includes(word));
  assert.deepEqual(hits, [], `README_zh.md 仍含高风险词：${hits.join('、')}`);
});

test('Gitee 版 README 不引用 GitHub raw 外链（国内加载不出来）', () => {
  assert.equal(
    GITEE_README.includes('raw.githubusercontent.com'),
    false,
    'README_zh.md 仍引用 raw.githubusercontent.com，Gitee 上图片会加载失败',
  );
});

test('Gitee 版 README 保留全部功能与架构章节（防止生成时误删正文）', () => {
  const sections = [
    '## 项目亮点',
    '## 功能一览',
    '## 界面预览',
    '## 技术架构',
    '## 快速开始',
    '## 目录结构',
    '## 配置说明',
    '## 开发与质量',
    '## 文档索引',
    '## 常见问题',
    '## 版本与发布',
    '## 许可证',
    '## 法律声明与免责',
    '## 联系我',
  ];
  for (const section of sections) {
    assert.ok(
      GITEE_README.includes(section),
      `README_zh.md 缺少章节「${section}」——生成规则可能误删了正文`,
    );
  }
});

test('GitHub 版 README 仍然保留赞助与联系入口（两份定位不同，不能互相污染）', () => {
  assert.ok(
    GITHUB_README.includes('请作者喝杯咖啡'),
    'README.md 的赞助入口被误删了——它只应该从 Gitee 版里去掉',
  );
  assert.ok(
    GITHUB_README.includes('wechat-card.jpg'),
    'README.md 的微信名片被误删了——它只应该从 Gitee 版里去掉',
  );
});

test('Gitee 版 README 已存在且非空', () => {
  assert.ok(GITEE_README.length > 10000, 'README_zh.md 内容异常（长度不足），检查生成脚本');
});
