import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

const source = readFileSync(resolve(process.cwd(), "src/views/system/sysjobresults/sysjobresultslist.vue"), "utf8");

describe("定时任务执行结果摘要", () => {
  it("新增执行摘要列，普通任务没有摘要时显示占位符", () => {
    expect(source).toContain('title="执行摘要"');
    expect(source).toContain('record.summary?.trim() || "—"');
  });
});

describe("定时任务页面外壳", () => {
  /**
   * ⛔ 不能再用 `<a-card>` 包页面：Arco 卡片的暗色背景是**中性灰 `--color-bg-2`（#171719）**，
   * 而平台外壳是**蓝黑 `--uvp-shell-bg`（#101823）**。两者同屏时卡片区发灰、与搜索面板/表格
   * 对不上色，看起来像"背景不对"（实测分页条那一片就是 #171719）。
   * 基准页（sysapi / dictionary / log 等）一律是 snow-inner + uvp-page-shell-flat 直出。
   */
  it("不再套 Arco 卡片，避免暗色下露出中性灰底", () => {
    expect(source).not.toMatch(/<a-card[\s>]/);
  });

  it("操作列按钮采用「图标 + 文字」形式", () => {
    const actionCell = source.slice(source.indexOf('title="操作"'));
    expect(actionCell).toContain("<icon-delete />");
    expect(actionCell).toContain("<span>删除</span>");
  });

  it("操作列内容居中（与 sysjobs / sysapi 等基准页一致）", () => {
    expect(source).toMatch(/<a-table-column title="操作"[^>]*align="center"/);
  });
});

describe("一键清理", () => {
  it("搜索区有「一键清理」按钮，且带独立权限点", () => {
    expect(source).toContain('data-testid="sysjobresults-clear-all"');
    expect(source).toContain("<span>一键清理</span>");
    expect(source).toContain("v-hasPerm=\"['system:sysjobresults:clear']\"");
    // 危险操作走按钮真源的软红：status="danger"，且不能是 primary
    // （一个操作区最多 1 个 primary，查询已占；旧告警页的 primary+warning 是建真源前的写法）
    // ⛔ 切片必须在起点之后找结束标签：从文件开头 indexOf("</a-button>") 会拿到查询按钮那个
    const from = source.indexOf('data-testid="sysjobresults-clear-all"');
    const clearBtn = source.slice(from, source.indexOf("</a-button>", from));
    expect(clearBtn).toContain('status="danger"');
    expect(clearBtn).not.toContain('type="primary"');
    expect(clearBtn).not.toContain('status="warning"');
  });

  it("点击一键清理必须先过确认气泡，且文案说明是「全部」", () => {
    // ⛔ 2026-11-04 反转：全站删除统一改用 a-popconfirm 气泡（老板拍板），
    //    原来的「必须 Modal.warning」断言作废，改钉气泡这一侧。
    const from = source.indexOf('data-testid="sysjobresults-clear-all"');
    const popconfirm = source.slice(source.lastIndexOf("<a-popconfirm", from), from);
    expect(popconfirm).toContain("<a-popconfirm");
    expect(popconfirm).toContain("全部定时任务执行日志");
    expect(popconfirm).toContain(":ok-button-props=\"{ status: 'danger' }\"");
    // 确认之后才真正调接口
    expect(popconfirm).toContain('@ok="performClearAll"');
    expect(source).toContain("await clearData()");
    // 不许再退回弹窗形态
    expect(source).not.toContain("Modal.warning(");
  });
});

describe("单条删除", () => {
  /**
   * ⛔ 2026-11-04 反转：这里原本钉的是「必须 Modal.warning、不许用气泡」，
   * 理由是日志页误删代价高。老板要求全站删除统一成气泡，于是改钉气泡一侧；
   * 误删防护改由「气泡里必须写明后果（不可恢复）」+ 危险按钮承担。
   */
  it("删除走确认气泡，并写明后果与危险按钮", () => {
    expect(source).toMatch(/<a-popconfirm[\s>]/);
    const deleteBlock = source.slice(source.indexOf("<a-popconfirm"), source.indexOf("<script"));
    expect(deleteBlock).toContain("不可恢复");
    expect(deleteBlock).toContain(":ok-button-props=\"{ status: 'danger' }\"");
    expect(source).not.toContain("Modal.warning(");
  });

  it("删除按钮仍在操作列内、逐行单独删除", () => {
    const actionCell = source.slice(source.indexOf('title="操作"'));
    expect(actionCell).toContain('@ok="handleDelete(record.id)"');
    expect(actionCell).toContain("<span>删除</span>");
  });
});
