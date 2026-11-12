import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

async function source(relativePath) {
  return readFile(new URL(`../${relativePath}`, import.meta.url), "utf8");
}

// 2026-11-09 实测：220 上 53004 被一条常驻的 `java -> 127.0.0.1:6379` 出站连接占着，
// 旧预检只看 LISTEN，报"130 个端口空闲" → 装到 ZLM 启动就
// `Listen on :: 53004 failed: address already in use` 无限重启，安装停在 zlm unhealthy。
// 所以两类占用都必须拦，且文案要分开（下一步动作不同）。
test("容器版安装预检同时拦截监听占用与出站连接占用", async () => {
  const install = await source("deploy/compose/install.sh");

  // ① 监听占用：保留原有 ss -ltn / -lun 两条判据
  assert.match(install, /ss_ports\("-ltn"\)/);
  assert.match(install, /ss_ports\("-lun"\)/);
  // ② 出站占用：必须显式取 ESTAB 的本地端口，并单独归到 busy_outbound
  assert.match(install, /ss_ports\("-tn", "state", "established"\)/);
  assert.match(install, /busy_outbound/);
  // ③ 两类必须分开报（合成一句"已被占用"现场就不知道该等还是该换段）
  assert.match(install, /服务监听\*\*占用/);
  assert.match(install, /本机出站连接\*\*占着/);
  // ④ 出站占用时给出"预留端口"这条永久出路
  assert.match(install, /ip_local_reserved_ports = \{start\}-\{end\}/);
});

// 默认 51000 / 推荐的 53000 都落在 ip_local_port_range(32768-60999) 内，
// 机器跑久了出站连接随机占段内端口 ⇒ 重启后偶发 ZLM 起不来。装的时候就要提醒。
test("容器版安装预检在段与临时端口范围重叠时提示预留", async () => {
  const install = await source("deploy/compose/install.sh");

  assert.match(install, /\/proc\/sys\/net\/ipv4\/ip_local_port_range/);
  assert.match(install, /与系统临时端口范围/);
});

// .env.example 里的 UVP_VERSION 曾是硬编码 1.1.4：1.1.19 的包里还是它，
// 客户照着样例建 .env 就去拉不存在的旧镜像（manifest unknown / pull access denied）。
test("容器版离线包把样例里的版本改写成包的真实版本", async () => {
  const pack = await source("deploy/compose/package-offline.sh");
  const example = await source("deploy/compose/.env.example");

  // 打包时用 awk 重写（跨平台，避开 BSD sed -i 必须带后缀的坑）
  assert.match(pack, /awk -v v="\$VERSION" '\/\^UVP_VERSION=\//);
  assert.match(pack, /UVP_VERSION=" v/);
  // 打完自检，防回归
  assert.match(pack, /grep -qx "UVP_VERSION=\$\{VERSION\}" "\$PKG\/\.env\.example"/);
  // 仓库里的样例仍要有这一行占位，否则 awk 无从改写
  assert.match(example, /^UVP_VERSION=/m);
});
