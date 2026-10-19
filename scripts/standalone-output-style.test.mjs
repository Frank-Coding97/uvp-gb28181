import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';

// ---------------------------------------------------------------------------
// 契约：安装输出的「阶段行 / 结果行 / 明细行」三级缩进必须对齐。
//
// 规则：结果行与明细行缩进的格数 == 阶段行标签的宽度（含标签后的那一个空格）。
//   标签 "[uvp-gb28181] " = 14 格  ⇒  缩进必须正好 14 格。
//
// ⛔ 为什么值得钉：这个宽度散在**三个程序**里 —— Linux 的 ctl.sh、Windows 的
//   ctl.ps1、以及打印建表/端口明细的 uvp-setup（Go）。任何一处跟不上，现场
//   输出就会参差不齐（结果行跑到阶段文字左边或右边），比完全不缩进还难看。
//   2026-10-20 老板要求把标签 "[uvp]" 改成 "[uvp-gb28181]"，正是这种改动。
// ---------------------------------------------------------------------------

const ctlSh = readFileSync(new URL('../deploy/standalone/uvp-gb28181-ctl.sh', import.meta.url), 'utf8');
const ctlPs1 = readFileSync(
  new URL('../deploy/standalone-windows/uvp-gb28181-ctl.ps1', import.meta.url),
  'utf8',
);
const setupGo = readFileSync(new URL('../server/cmd/uvp-setup/main.go', import.meta.url), 'utf8');

// 标签取自 log() 的实际格式串，避免测试自己写死一份"正确答案"。
function tagOf(source) {
  const m = source.match(/^log\(\)\s*\{\s*printf '%s\[([A-Za-z0-9._-]+)\]%s %s\\n'/m);
  assert.ok(m, 'ctl.sh 的 log() 里找不到 "[标签] " 格式串');
  return m[1];
}

test('Linux ctl.sh：缩进 == 标签宽度（含标签后的空格）', () => {
  const tag = tagOf(ctlSh);
  const ind = ctlSh.match(/^IND='( *)'$/m)?.[1];
  assert.ok(ind !== undefined, 'ctl.sh 里找不到 IND 定义');
  assert.equal(
    ind.length,
    tag.length + 3,
    `IND 是 ${ind.length} 格，但标签 "[${tag}] " 是 ${tag.length + 3} 格 —— 结果行会错位`,
  );
});

test('uvp-setup（Go）的 indent 与 shell 同宽', () => {
  const ind = ctlSh.match(/^IND='( *)'$/m)?.[1];
  const goInd = setupGo.match(/^const indent = "( *)"$/m)?.[1];
  assert.ok(goInd !== undefined, 'uvp-setup 里找不到 const indent');
  assert.equal(goInd.length, ind.length, 'Go 侧 indent 与 ctl.sh 的 IND 宽度不一致');
});

test('Windows ctl.ps1 的 $script:IND 与前两者同宽', () => {
  const ind = ctlSh.match(/^IND='( *)'$/m)?.[1];
  const psInd = ctlPs1.match(/^\$script:IND = '( *)'$/m)?.[1];
  assert.ok(psInd !== undefined, 'ps1 里找不到 $script:IND');
  assert.equal(psInd.length, ind.length, 'Windows 侧 IND 与 ctl.sh 的 IND 宽度不一致');
});

test('三个平台的标签字面量完全一致（不许某一边还留着旧标签）', () => {
  const tag = tagOf(ctlSh);
  assert.match(
    ctlPs1,
    new RegExp(`Write-UvpLog\\s*\\{.*\\[${tag}\\]`),
    'Windows 的 Write-UvpLog 标签与 Linux 不一致',
  );
  // ⛔ 旧标签 "[uvp]" 不能作为前缀残留（"[uvp-gb28181]" 里当然包含 "uvp"，
  //   所以要按"方括号紧跟非连字符"来判，不能简单查 substring）。
  for (const [name, src] of [
    ['ctl.sh', ctlSh.split('\n').filter((l) => !l.trim().startsWith('#')).join('\n')],
    ['ctl.ps1', ctlPs1.split('\n').filter((l) => !l.trim().startsWith('#')).join('\n')],
  ]) {
    assert.doesNotMatch(src, /\[uvp\](?!-)/, `${name} 里还留着旧的 [uvp] 标签`);
  }
});
