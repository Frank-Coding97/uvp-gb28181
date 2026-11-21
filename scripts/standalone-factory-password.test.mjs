import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import { fileURLToPath } from 'node:url';
import test from 'node:test';

// ============================================================================
// 契约：`uvp-gb28181-ctl.sh reset-password`（忘记登录口令时把账号还原成出厂口令）
//
// 这条命令只在**客户已经登不进去**的时候才被用到 —— 现场没有任何别的手段可以
// 交叉验证它做得对不对。它现在的分工是：
//
//   shell 侧（ctl.sh）只做一道**拦截**：读本机 config.yml 的
//     server.demoaccount.defaultpassword，如果不等于脚本里的 CTL_FACTORY_PASSWORD
//     就拒绝执行（"把口令重置成一个谁也猜不到的字符串"是最贵的一类故障）。
//   真正的重置动作在 bin/uvp-setup reset-admin 里（置 must_change_password=1、
//     写后回读校验、清 Redis 锁定键）—— 那部分行为由 Go 侧
//     server/cmd/uvp-setup/resetadmin_test.go 逐条锁住，本文件不再重复。
//
// 于是这里只剩**一条**必须跨语言守住的等式：
//     ctl.sh 的 CTL_FACTORY_PASSWORD  ==  config.example.yml 的
//     server.demoaccount.defaultpassword
//   （出包时 config.yml 由 make-config.py 从 example 复制而来，且**不会**改这个键，
//     所以 example 里的值就是客户机上那份 config.yml 的值。）
//
// ⛔ 两边不一致的后果：客户在全新安装的机器上跑 reset-password，被自己的脚本
//   拒绝执行，而报错文案说"本机的出厂口令与包不一致"——现场只会认为包坏了。
// ============================================================================

const ROOT = fileURLToPath(new URL('../', import.meta.url));
const CTL_PATH = join(ROOT, 'deploy/standalone/uvp-gb28181-ctl.sh');
const CONFIG_EXAMPLE = join(ROOT, 'server/config/config.example.yml');

const ctl = readFileSync(CTL_PATH, 'utf8');

function ctlConstant(name) {
  const m = ctl.match(new RegExp('^' + name + "='([^']*)'$", 'm'));
  assert.ok(m, CTL_PATH + ' 里找不到常量 ' + name + '（被改名/删掉了？）');
  return m[1];
}

/** 取 config.example.yml 里 `server:` → `demoaccount:` → `defaultpassword` 的值。 */
function exampleDefaultPassword() {
  const yml = readFileSync(CONFIG_EXAMPLE, 'utf8');
  const lines = yml.split('\n');

  const findSection = (section, from = 0) => {
    for (let i = from; i < lines.length; i += 1) {
      const line = lines[i];
      if (line.trim() === '' || line.trim().startsWith('#')) continue;
      if (line.trim() === section + ':') return i;
    }
    return -1;
  };
  const indentOf = (line) => line.length - line.trimStart().length;

  // ⛔ 必须逐层按缩进定位：别的段里也可能有叫 defaultpassword 的键。
  const serverAt = findSection('server');
  assert.ok(serverAt >= 0, 'config.example.yml 里找不到 server 段');
  const demoAt = findSection('demoaccount', serverAt + 1);
  assert.ok(demoAt >= 0, 'config.example.yml 里找不到 server.demoaccount 段');
  const demoIndent = indentOf(lines[demoAt]);

  for (let i = demoAt + 1; i < lines.length; i += 1) {
    const line = lines[i];
    if (line.trim() === '' || line.trim().startsWith('#')) continue;
    if (indentOf(line) <= demoIndent) break;
    const m = line.trim().match(/^defaultpassword\s*:\s*(.*)$/);
    if (m) return m[1].replace(/\s*#.*$/, '').trim().replace(/^"(.*)"$/, '$1');
  }
  assert.fail('config.example.yml 的 server.demoaccount 段里找不到 defaultpassword');
}

test('出厂口令常量与 config.example.yml 的 server.demoaccount.defaultpassword 同源', () => {
  const expected = exampleDefaultPassword();
  assert.equal(ctlConstant('CTL_FACTORY_PASSWORD'), expected,
    'ctl.sh 的出厂口令常量与 config.example.yml 不一致：\n' +
    '出包时 config.yml 就是从 example 复制的，所以客户机上那条拦截会**永远失败**，\n' +
    'reset-password 在全新机器上直接被自己的脚本拒绝执行。');
});

test('reset-password 的拦截逻辑还在：config.yml 的口令被改过就拒绝执行', () => {
  // ⛔ 这条拦截是"最后一道保险"：若它被删掉，重置就会把口令写成一个谁也不知道的
  //   值，而脚本仍然打印 config 里那个口令 ⇒ 重置完照样登不进去，且现场查不出来。
  const body = ctl.slice(ctl.indexOf('reset_admin_password()'));
  assert.match(body.slice(0, 2000), /if \[ "\$configured" != "\$CTL_FACTORY_PASSWORD" \]/,
    'reset_admin_password 里的"出厂口令被改过就拒绝"判断不见了');
  assert.match(body.slice(0, 2500), /拒绝执行/,
    '拒绝执行时的说明文案不见了（现场需要知道该怎么办）');
});

test('reset-password 复用 uvp-setup，不再自己写哈希（跨平台同一份实现）', () => {
  const body = ctl.slice(ctl.indexOf('reset_admin_password()'));
  assert.match(body.slice(0, 2000), /"\$SETUP_BIN" reset-admin --root "\$ROOT" --username "\$username"/,
    'Linux 侧必须调 bin/uvp-setup reset-admin（与 Windows 同一份实现）');
  // 老版在脚本里写死 $2a$ 哈希常量并自己 UPDATE 库 —— 那正是"两个平台各写一套"的来源。
  assert.doesNotMatch(ctl, /CTL_FACTORY_HASH/,
    'CTL_FACTORY_HASH 已废弃（哈希改由 uvp-setup 现场算），不该再出现在脚本里');
});
