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

// ---------------------------------------------------------------------------
// 契约：启动收尾的**版式与顺序**。
//
// 顺序：端口规划（技术明细，要拿去开防火墙）在前，启动完成 + 访问地址在最后。
//   理由：装完之后用户唯一想抄走的就是那个 URL，它必须停在屏幕最后一行，
//   而不是被十几行端口明细冲到上面去。
// 版式：访问地址单独用一个不封口的横线框圈起来并加粗 —— 且**颜色必须可关**，
//   输出重定向成日志时只剩框线与空行，照样能看出"到这里就装完了"。
// ---------------------------------------------------------------------------

test('Linux：端口规划必须排在启动完成之前（地址留在屏幕最后）', () => {
  const branch = ctlSh.slice(ctlSh.indexOf('case "${1:-start}" in'));
  assert.ok(branch.length > 0, 'ctl.sh 里找不到 case "${1:-start}" in');
  const planAt = branch.indexOf('print_port_plan');
  const doneAt = branch.indexOf('print_start_summary');
  assert.ok(planAt > 0, 'start 分支没调用 print_port_plan');
  assert.ok(doneAt > 0, 'start 分支没调用 print_start_summary');
  assert.ok(planAt < doneAt, '启动收尾顺序反了：应当先打端口规划，再打「启动完成 + 访问地址」');
});

test('Windows：端口规划同样排在启动完成之前', () => {
  const branch = ctlPs1.slice(ctlPs1.indexOf('function Invoke-Start {'));
  const planAt = branch.indexOf('Write-PortPlan -Ports');
  const doneAt = branch.indexOf('Write-StartSummary -Ports');
  assert.ok(planAt > 0, 'Invoke-Start 没调用 Write-PortPlan');
  assert.ok(doneAt > 0, 'Invoke-Start 没调用 Write-StartSummary');
  assert.ok(planAt < doneAt, 'Windows 启动收尾顺序与 Linux 不一致');
});

test('访问地址用独立高亮色，且无彩色终端时该色必须清零', () => {
  // 有 tty 才给加粗亮绿；否则空串 —— 否则日志里会混进 \033[1;32m 噪声。
  assert.match(ctlSh, /C_HL=\$'\\033\[1;32m'/, 'ctl.sh 里没有定义高亮色 C_HL（应以加粗亮绿为准）');
  assert.match(ctlSh, /else\s*\n\s*C_TAG='';[^\n]*C_HL='';?[^\n]*C_RST=''/, 'ctl.sh 的 else 分支没有清空 C_HL —— 重定向到文件时会漏转义序列');
  assert.match(ctlPs1, /\$C\.hl\s*=\s*\[char\]27 \+ '\[1;32m'/, 'ctl.ps1 里 $C.hl 不是加粗亮绿');
  assert.match(ctlPs1, /\$C\s*=\s*@\{[^}]*hl\s*=\s*''/, 'ctl.ps1 的 $C 没给 hl 默认空串');
});

test('收尾块用不封口的横线框圈住地址行（无彩色时也能一眼认出）', () => {
  const body = ctlSh.match(/^print_start_summary\(\) \{[\s\S]*?^\}/m)?.[0];
  assert.ok(body, 'ctl.sh 里找不到 print_start_summary');
  assert.match(body, /rule="--+"/, 'print_start_summary 没有横线 rule');
  const ruleLen = body.match(/rule="(-+)"/)[1].length;
  assert.ok(ruleLen >= 48, `横线只有 ${ruleLen} 格，圈不住下面的地址行`);
  // ⛔ 只许用光秃秃的短横线：中文双宽 + 无 wcwidth ⇒ 任何"左右封口"的框都会歪。
  assert.doesNotMatch(body, /[│┃|]\s*$/m, '收尾框画了右边线，中文环境下必然错位');
  assert.match(body, /访问地址/, '收尾块里没有"访问地址"字样');
  assert.match(body, /登录账号/, '收尾块里没有登录账号');
});

