#!/usr/bin/env node
// 发布 UVP-GB28181 独立部署包：上传 GitHub 与 Gitee Release，并回写官网下载中心数据。
//
// 用法：
//   node scripts/publish-release.mjs --version 1.1.4
//   node scripts/publish-release.mjs --version 1.1.4 --dry-run
//   node scripts/publish-release.mjs --version 1.1.4 --only gitee --skip-site
//
// 需要的环境变量（--dry-run 时不需要）：
//   GITHUB_TOKEN   GitHub 个人访问令牌，需要 repo 写权限
//   GITEE_TOKEN    Gitee 私人令牌，需要仓库写权限
// 也可以写进 ~/.workbuddy/uvp-release.env（`export GITHUB_TOKEN=...` 格式），
// 脚本会自动加载；环境变量优先，命令行里不要出现令牌。

import { createHash } from 'node:crypto';
import { readFileSync, writeFileSync, statSync, existsSync, rmSync } from 'node:fs';
import { execFileSync } from 'node:child_process';
import os from 'node:os';
import path from 'node:path';

const REPO_ROOT = path.resolve(import.meta.dirname, '..');
const OUTPUT_DIR = path.join(REPO_ROOT, 'release-output');
const CREDENTIAL_FILE = path.join(os.homedir(), '.workbuddy', 'uvp-release.env');

function loadCredentials() {
  if (!existsSync(CREDENTIAL_FILE)) return;
  const text = readFileSync(CREDENTIAL_FILE, 'utf8');
  for (const line of text.split('\n')) {
    const match = line.match(/^\s*(?:export\s+)?([A-Z_][A-Z0-9_]*)\s*=\s*(.*?)\s*$/);
    if (!match) continue;
    const [, key, rawValue] = match;
    if (process.env[key]) continue; // 环境变量优先
    process.env[key] = rawValue.replace(/^['"]|['"]$/g, '');
  }
}

const PLATFORMS = [
  { key: 'linux-amd64', channel: 'Linux amd64 独立部署包', platform: 'Linux amd64', ext: 'tar.gz' },
  { key: 'linux-arm64', channel: 'Linux arm64 独立部署包', platform: 'Linux arm64', ext: 'tar.gz' },
  { key: 'windows-amd64', channel: 'Windows amd64 独立部署包', platform: 'Windows amd64', ext: 'zip' },
];

const DEFAULT_SITE_REPO = '/Users/menglulu/code/uvp/uvplatform-website';

function parseArgs(argv) {
  const args = { version: '', notes: '', siteRepo: DEFAULT_SITE_REPO, dryRun: false, only: '', host: 'gitee', skipSite: false, force: false };
  for (let i = 0; i < argv.length; i += 1) {
    const value = argv[i + 1];
    switch (argv[i]) {
      case '--version': args.version = value; i += 1; break;
      case '--force': args.force = true; break;
      case '--notes': args.notes = value; i += 1; break;
      case '--site-repo': args.siteRepo = value; i += 1; break;
      case '--dry-run': args.dryRun = true; break;
      case '--skip-site': args.skipSite = true; break;
      case '--only': args.only = value; i += 1; break;
      case '--host': args.host = value; i += 1; break;
      case '--help': printHelp(); process.exit(0); break;
      default: break;
    }
  }
  return args;
}

function printHelp() {
  console.log(`发布 UVP-GB28181 独立部署包

  node scripts/publish-release.mjs --version <版本号> [选项]

选项：
  --version <版本号>   要发布的版本，默认读取仓库根目录 VERSION 文件
  --notes <文件路径>   升版说明，默认 docs/releases/v<版本号>.md
  --site-repo <路径>   官网仓库路径，用于回写 data/releases.json
  --host <gitee|github> 官网下载按钮使用哪个站点的直链，默认 gitee
  --only <gitee|github> 只发布到指定站点
  --skip-site          跳过回写官网数据
  --force              已存在同名附件也重新上传
  --dry-run            只演练，不上传也不写文件

中断后可直接重跑：同名同体积的附件会自动跳过，不会重复传。
`);
}

function readVersion() {
  const versionFile = path.join(REPO_ROOT, 'VERSION');
  if (!existsSync(versionFile)) throw new Error('找不到 VERSION 文件，请用 --version 指定版本号');
  return readFileSync(versionFile, 'utf8').trim();
}

function resolveRemote(remoteName) {
  try {
    const url = execFileSync('git', ['-C', REPO_ROOT, 'remote', 'get-url', remoteName], { encoding: 'utf8' }).trim();
    const match = url.match(/[:/]([^/:]+)\/([^/]+?)(?:\.git)?$/);
    if (match) return { owner: match[1], repo: match[2] };
  } catch {
    /* 远端不存在时走默认值 */
  }
  return null;
}

function sha256Of(file) {
  const hash = createHash('sha256');
  hash.update(readFileSync(file));
  return hash.digest('hex');
}

function today() {
  const now = new Date();
  const pad = (n) => String(n).padStart(2, '0');
  return `${now.getFullYear()}-${pad(now.getMonth() + 1)}-${pad(now.getDate())}`;
}

async function githubRequest(token, method, url, { json, binary, contentType } = {}) {
  const headers = {
    Authorization: `Bearer ${token}`,
    Accept: 'application/vnd.github+json',
    'X-GitHub-Api-Version': '2022-11-28',
  };
  let body;
  if (binary) {
    headers['Content-Type'] = contentType || 'application/octet-stream';
    body = binary;
  } else if (json) {
    headers['Content-Type'] = 'application/json';
    body = JSON.stringify(json);
  }
  const res = await fetch(url, { method, headers, body });
  if (!res.ok) {
    const detail = await res.text();
    throw new Error(`GitHub ${method} ${url} 失败（${res.status}）：${detail.slice(0, 400)}`);
  }
  return res.status === 204 ? null : res.json();
}

// 大附件（几十 MB）用 fetch 上传在本机会中途断连，改用 curl 走 multipart / binary 直传。
function curlRequest({ args, label, retries = 3 }) {
  let lastError;
  for (let attempt = 1; attempt <= retries; attempt += 1) {
    const headerFile = path.join(os.tmpdir(), `uvp-upload-header-${process.pid}-${Date.now()}.txt`);
    const bodyFile = path.join(os.tmpdir(), `uvp-upload-body-${process.pid}-${Date.now()}.json`);
    try {
      execFileSync('curl', ['-sS', '-D', headerFile, '-o', bodyFile, ...args], { encoding: 'utf8', maxBuffer: 64 * 1024 * 1024 });
      const head = readFileSync(headerFile, 'utf8');
      const codes = head.match(/HTTP\/[\d.]+ (\d{3})/g) || [];
      const status = Number((codes[codes.length - 1] || '').split(' ')[1] || 0);
      const body = readFileSync(bodyFile, 'utf8');
      if (status >= 200 && status < 300) return body ? JSON.parse(body) : null;
      lastError = new Error(`${label} 失败（HTTP ${status}）：${body.slice(0, 300)}`);
    } catch (error) {
      lastError = new Error(`${label} 失败：${error.message.slice(0, 300)}`);
    } finally {
      for (const file of [headerFile, bodyFile]) { try { rmSync(file, { force: true }); } catch { /* 忽略清理失败 */ } }
    }
    if (attempt < retries) {
      console.log(`  第 ${attempt} 次上传失败，2 秒后重试：${lastError.message.slice(0, 120)}`);
      execFileSync('sleep', ['2']);
    }
  }
  throw lastError;
}

function uploadToGithub({ token, owner, repo, releaseId, asset }) {
  return curlRequest({
    label: `GitHub 上传 ${asset.filename}`,
    args: [
      '-X', 'POST',
      '-H', `Authorization: Bearer ${token}`,
      '-H', 'Accept: application/vnd.github+json',
      '-H', `Content-Type: ${asset.filename.endsWith('.zip') ? 'application/zip' : 'application/gzip'}`,
      '--data-binary', `@${asset.path}`,
      `https://uploads.github.com/repos/${owner}/${repo}/releases/${releaseId}/assets?name=${encodeURIComponent(asset.filename)}`,
    ],
  });
}

function uploadToGitee({ token, owner, repo, releaseId, asset }) {
  return curlRequest({
    label: `Gitee 上传 ${asset.filename}`,
    args: [
      '-X', 'POST',
      '-F', `access_token=${token}`,
      '-F', `file=@${asset.path}`,
      `https://gitee.com/api/v5/repos/${owner}/${repo}/releases/${releaseId}/attach_files`,
    ],
  });
}

async function giteeRequest(url, { json, form, method } = {}) {
  const options = { method: method || (form || json ? 'POST' : 'GET') };
  if (json) {
    options.headers = { 'Content-Type': 'application/json;charset=UTF-8' };
    options.body = JSON.stringify(json);
  } else if (form) {
    options.body = form;
  }
  const res = await fetch(url, options);
  if (!res.ok) {
    const detail = await res.text();
    throw new Error(`Gitee ${options.method} ${url} 失败（${res.status}）：${detail.slice(0, 400)}`);
  }
  return res.json();
}

async function publishToGithub({ token, owner, repo, tag, notes, assets, dryRun, force }) {
  const base = `https://api.github.com/repos/${owner}/${repo}`;
  console.log(`\n[GitHub] ${owner}/${repo} ${tag}`);
  if (dryRun) {
    for (const asset of assets) console.log(`  演练：上传 ${asset.filename}（${asset.sizeMB}）`);
    return { url: `https://github.com/${owner}/${repo}/releases/tag/${tag}`, assetUrls: {} };
  }

  let release;
  try {
    release = await githubRequest(token, 'GET', `${base}/releases/tags/${tag}`);
    console.log(`  已存在 Release #${release.id}，更新说明并复用`);
    await githubRequest(token, 'PATCH', `${base}/releases/${release.id}`, { json: { body: notes } });
  } catch {
    release = await githubRequest(token, 'POST', `${base}/releases`, {
      json: { tag_name: tag, name: tag, body: notes, draft: false, prerelease: false },
    });
    console.log(`  已创建 Release #${release.id}`);
  }

  const existing = new Map((release.assets || []).map((item) => [item.name, item]));
  const assetUrls = {};
  for (const asset of assets) {
    const already = existing.get(asset.filename);
    if (already && already.size === asset.bytes && !force) {
      console.log(`  已存在同名同体积资产，跳过 ${asset.filename}`);
      assetUrls[asset.filename] = `https://github.com/${owner}/${repo}/releases/download/${tag}/${asset.filename}`;
      continue;
    }
    if (already) {
      await githubRequest(token, 'DELETE', `${base}/releases/assets/${already.id}`);
      console.log(`  已删除旧资产 ${asset.filename}`);
    }
    await uploadToGithub({ token, owner, repo, releaseId: release.id, asset });
    assetUrls[asset.filename] = `https://github.com/${owner}/${repo}/releases/download/${tag}/${asset.filename}`;
    console.log(`  已上传 ${asset.filename}（${asset.sizeMB}）`);
  }
  return { url: release.html_url, assetUrls };
}

async function publishToGitee({ token, owner, repo, tag, notes, assets, dryRun, force }) {
  const base = `https://gitee.com/api/v5/repos/${owner}/${repo}`;
  console.log(`\n[Gitee] ${owner}/${repo} ${tag}`);
  if (dryRun) {
    for (const asset of assets) console.log(`  演练：上传 ${asset.filename}（${asset.sizeMB}）`);
    return { url: `https://gitee.com/${owner}/${repo}/releases/tag/${tag}`, assetUrls: {} };
  }

  let release;
  try {
    release = await giteeRequest(`${base}/releases/tags/${tag}?access_token=${token}`);
    console.log(`  已存在 Release #${release.id}，复用`);
  } catch {
    // Gitee 建 Release 必须显式指定 target_commitish，缺失会报 400
    let branch = 'master';
    try {
      const repo = await giteeRequest(`${base}?access_token=${token}`);
      if (repo.default_branch) branch = repo.default_branch;
    } catch { /* 拿不到就用默认分支名兜底 */ }
    release = await giteeRequest(`${base}/releases`, {
      json: { access_token: token, tag_name: tag, name: tag, body: notes, target_commitish: branch },
    });
    console.log(`  已创建 Release #${release.id}（基于 ${branch}）`);
  }

  // 复用已有 Release 时，顺带把正文同步成最新的升版说明
  await giteeRequest(`${base}/releases/${release.id}`, {
    method: 'PATCH',
    json: { access_token: token, name: tag, body: notes },
  }).catch(() => { /* 正文更新失败不阻断附件上传 */ });

  // ⛔⛔ releases/tags 返回的 assets 只有 name 与下载地址，**没有 size** ⇒
  //   `Number(already.size) === asset.bytes` 恒为 false，重跑一次就把同名附件再传一份，
  //   两份合计直接吃掉 Gitee 的 1GB 附件配额（2026-10-27 实测：1.1.14 的 amd64/arm64
  //   各存了两份，windows 因此永远传不上去）。附件明细必须走 attach_files 接口。
  const files = await giteeRequest(`${base}/releases/${release.id}/attach_files?access_token=${token}`).catch(() => null);
  const detail = await giteeRequest(`${base}/releases/tags/${tag}?access_token=${token}`).catch(() => null);
  const listed = Array.isArray(files) ? files : (detail && Array.isArray(detail.assets) ? detail.assets : []);
  const existing = new Map(listed.map((item) => [item.name, item]));
  const assetUrls = {};
  for (const asset of assets) {
    const already = existing.get(asset.filename);
    if (already && Number(already.size) === asset.bytes && !force) {
      console.log(`  已存在同名同体积附件，跳过 ${asset.filename}`);
      assetUrls[asset.filename] = `https://gitee.com/${owner}/${repo}/releases/download/${tag}/${asset.filename}`;
      continue;
    }
    await uploadToGitee({ token, owner, repo, releaseId: release.id, asset });
    assetUrls[asset.filename] = `https://gitee.com/${owner}/${repo}/releases/download/${tag}/${asset.filename}`;
    console.log(`  已上传 ${asset.filename}（${asset.sizeMB}）`);
  }
  return { url: release.html_url || `https://gitee.com/${owner}/${repo}/releases/tag/${tag}`, assetUrls };
}

function updateSiteData({ siteRepo, version, host, targets, urls, noteBullets, dryRun }) {
  const dataFile = path.join(siteRepo, 'data/releases.json');
  if (!existsSync(dataFile)) throw new Error(`官网数据文件不存在：${dataFile}`);
  const releases = JSON.parse(readFileSync(dataFile, 'utf8'));
  const releasedOn = today();

  for (const platform of PLATFORMS) {
    const target = targets.find((item) => item.key === platform.key);
    if (!target) continue;
    const url = urls[platform.key];
    const entry = {
      productId: 'uvp-gb28181',
      status: 'available',
      channel: platform.channel,
      platform: platform.platform,
      version,
      releasedOn,
      filename: target.filename,
      bytes: target.bytes,
      sha256: target.sha256,
    };
    if (noteBullets.length) entry.notes = noteBullets; // 官网下载页「更新」区用的就是这份
    if (url) entry.url = url;
    const index = releases.findIndex((item) => item.productId === entry.productId && item.channel === entry.channel && item.version === entry.version);
    if (index >= 0) releases[index] = { ...releases[index], ...entry };
    else releases.push(entry);
  }

  console.log(`\n[官网] ${dataFile}`);
  if (dryRun) {
    for (const platform of PLATFORMS) {
      const target = targets.find((item) => item.key === platform.key);
      if (target) console.log(`  演练：写入 ${platform.channel} ${version} ${target.sha256.slice(0, 12)}…`);
    }
    return;
  }
  writeFileSync(dataFile, `${JSON.stringify(releases, null, 2)}\n`);
  console.log(`  已写入 ${PLATFORMS.length} 个通道的 ${version} 数据，旧版本保留为历史版本`);
  console.log(`  下载直链使用：${host}`);
}

async function main() {
  loadCredentials();
  const args = parseArgs(process.argv.slice(2));
  const version = args.version || readVersion();
  const tag = version.startsWith('v') ? version : `v${version}`;
  const plainVersion = version.startsWith('v') ? version.slice(1) : version;
  const notesFile = args.notes || path.join(REPO_ROOT, 'docs', 'releases', `${tag}.md`);

  console.log(`版本号：${plainVersion}（tag ${tag}）`);

  if (!existsSync(notesFile)) {
    throw new Error(`找不到升版说明：${notesFile}\n请先按 docs/releases/README.md 的约定写好升版说明。`);
  }
  const notes = readFileSync(notesFile, 'utf8').trim();
  console.log(`升版说明：${path.relative(REPO_ROOT, notesFile)}（${notes.length} 字）`);

  const targets = [];
  for (const platform of PLATFORMS) {
    const filename = `uvp-gb28181-${platform.key}-${plainVersion}.${platform.ext}`;
    // 产物可能在 release-output/ 根目录，也可能在 release-output/<版本>/ 子目录下
    const candidates = [path.join(OUTPUT_DIR, filename), path.join(OUTPUT_DIR, plainVersion, filename)];
    const filePath = candidates.find((item) => existsSync(item));
    if (!filePath) {
      throw new Error(`缺少产物：${filename}\n已查找：\n  ${candidates.join('\n  ')}\n请先构建 ${platform.channel}，再执行发布。`);
    }
    const bytes = statSync(filePath).size;
    const sha256 = sha256Of(filePath);
    const sidecar = [path.join(OUTPUT_DIR, `${filename}.sha256`), path.join(OUTPUT_DIR, plainVersion, `${filename}.sha256`)].find((item) => existsSync(item));
    if (sidecar) {
      const recorded = readFileSync(sidecar, 'utf8').trim().split(/\s+/)[0];
      if (recorded.toLowerCase() !== sha256) {
        throw new Error(`${filename} 的 .sha256 文件记录为 ${recorded}，实测为 ${sha256}，不一致，已中止。`);
      }
    }
    targets.push({ ...platform, filename, path: filePath, bytes, sha256, sizeMB: `${(bytes / 1024 / 1024).toFixed(1)} MB` });
    console.log(`产物就绪：${filename} ${(bytes / 1024 / 1024).toFixed(1)} MB ${sha256.slice(0, 12)}…`);
  }

  const githubRepo = resolveRemote('origin') || { owner: 'Frank-Coding97', repo: 'uvp-gb28181' };
  const giteeRepo = resolveRemote('gitee') || { owner: 'Frank-Coding', repo: 'uvp-gb28181' };
  const wantGithub = args.only !== 'gitee';
  const wantGitee = args.only !== 'github';

  const githubToken = process.env.GITHUB_TOKEN || '';
  const giteeToken = process.env.GITEE_TOKEN || '';
  if (!args.dryRun) {
    const hint = `\n可写入 ${CREDENTIAL_FILE}（权限 600），或直接用环境变量传入。`;
    if (wantGithub && !githubToken) throw new Error(`缺少 GitHub 令牌 GITHUB_TOKEN。${hint}`);
    if (wantGitee && !giteeToken) throw new Error(`缺少 Gitee 令牌 GITEE_TOKEN。${hint}`);
  }

  const assetTargets = targets.map((item) => ({ ...item }));
  let githubResult = { assetUrls: {} };
  let giteeResult = { assetUrls: {} };
  if (wantGithub) {
    githubResult = await publishToGithub({ token: githubToken, owner: githubRepo.owner, repo: githubRepo.repo, tag, notes, assets: assetTargets, dryRun: args.dryRun, force: args.force });
  }
  if (wantGitee) {
    giteeResult = await publishToGitee({ token: giteeToken, owner: giteeRepo.owner, repo: giteeRepo.repo, tag, notes, assets: assetTargets, dryRun: args.dryRun, force: args.force });
  }

  if (!args.skipSite) {
    const source = args.host === 'github' ? githubResult.assetUrls : giteeResult.assetUrls;
    const urls = {};
    for (const target of targets) {
      urls[target.key] = source[target.filename]
        || (args.host === 'github'
          ? `https://github.com/${githubRepo.owner}/${githubRepo.repo}/releases/download/${tag}/${target.filename}`
          : `https://gitee.com/${giteeRepo.owner}/${giteeRepo.repo}/releases/download/${tag}/${target.filename}`);
    }
    updateSiteData({
      siteRepo: args.siteRepo,
      version: plainVersion,
      host: args.host,
      targets,
      urls,
      noteBullets: notes.split('\n').map((line) => line.trim()).filter((line) => line.startsWith('- ')).map((line) => line.slice(2).trim()),
      dryRun: args.dryRun,
    });
  }

  console.log(`\n${args.dryRun ? '演练完成，未做任何改动。' : '发布完成。'}`);
  if (!args.dryRun) {
    if (wantGithub) console.log(`GitHub：${githubResult.url}`);
    if (wantGitee) console.log(`Gitee：${giteeResult.url}`);
  }
}

main().catch((error) => {
  console.error(`\n发布中止：${error.message}`);
  process.exit(1);
});
