"""Execute permission catalog migrations against existing authorization fixtures."""
import json
import re
import sqlite3
import sys
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
DATABASE = ROOT / "server/resource/database"
CATALOG = DATABASE / "gb28181/button-permissions.json"
VERSION = "2026-09-05-button-permission-catalog"
GROUP_ENUM = ROOT / "server/app/models/sysapigroup.go"


def backend_group_enum():
    """从 Go 的受控清单里取出分组名（唯一真源）。"""
    text = GROUP_ENUM.read_text()
    block = text.split("var sysApiGroups = []string{", 1)[1].split("}", 1)[0]
    return re.findall(r'"([^"]+)"', block)


# sys_api 语句起点。⛔ 必须带边界：否则 `sys_menu_api` 的尾串也会命中，
# 于是 casbin/菜单关联语句里的三连字面量会被误当成分组（`('p','role','/api/x','POST','*')`）。
API_STMT = re.compile(r"\b(?:INSERT\s+INTO|UPDATE)\s+[`\"\[]?sys_api[`\"\]]?(?![_A-Za-z0-9])", re.I)
# sys_api 行布局是 (…, title, path, method, api_group, …)：path/method/分组相邻
TRIPLE = re.compile(r"'(/api/[^']*)'\s*,\s*'(GET|POST|PUT|PATCH|DELETE)'\s*,\s*(N?)'([^']*)'")
# 「重定口径」段是 UPDATE … WHERE path=… AND method=… 形态（不是元组插入），
# 三方言的标识符引号不同：MySQL 反引号 / PG 双引号 / SQL Server 方括号。
UPDATE_ROW = re.compile(
    r"api_group[`\"\]]?\s*=\s*(N?)'([^']*)'\s+WHERE\s+[`\"\[]?path[`\"\]]?\s*=\s*'(/api/[^']*)'"
    r"\s+AND\s+[`\"\[]?method[`\"\]]?\s*=\s*'(GET|POST|PUT|PATCH|DELETE)'",
    re.I,
)
# 接口标题归一化段：`UPDATE sys_api SET title=… WHERE path=… AND method=…`
# ⛔ 三方言字面量前缀不同：MySQL/PG 用 `'x'`、SQL Server 用 `N'x'` ⇒ 三处都要 `(?:N)?`。
TITLE_ROW = re.compile(
    r"title[`\"\]]?\s*=\s*(?:N)?'((?:[^']|'')*)'\s+WHERE\s+[`\"\[]?path[`\"\]]?\s*=\s*(?:N)?'(/api/[^']*)'"
    r"\s+AND\s+[`\"\[]?method[`\"\]]?\s*=\s*(?:N)?'(GET|POST|PUT|PATCH|DELETE)'",
    re.I,
)
TITLE_VERSION = "2026-09-21-api-title-normalization"
GUEST_CATALOG = DATABASE / "gb28181/guest-permissions.json"

# 机械占位名：`<模块名> <METHOD> <path>`，由 2026-08-30-media-management 迁移的
# CONCAT('媒体管理 ',method,' ',path) 批量拼出（实测 58 行，全在 `/api/gb28181/zlm/*`）。
# ⛔ 这类名字**字符串唯一**，靠"重复检测"抓不到，必须单独断言。
MECHANICAL_TITLE = re.compile(r" (GET|POST|PUT|PATCH|DELETE) ")

# 「多屏播放」是**页面级**概念 —— 它是播放控制台（sys_menu 140368）这一页的标题。
# 只有三类接口真正属于它：分屏布局方案 / 通道收藏 / 播放地址与点播动作。
MULTI_SCREEN_OWN_PREFIXES = (
    "/api/gb28181/playback-schemes",
    "/api/gb28181/channel-favorite-groups",
    "/api/gb28181/play",
)

# 「工作台页面」：它们的组件挂在 layout 级全局宿主上（或跨页复用），
# 按钮摆在这类页面上**不构成**功能模块归属。
# ⛔ 本仓为此返工过两次，详见 test_workspace_pages_do_not_collapse_into_one_group 的注释。
WORKSPACE_PAGES = {
    "/gb28181/multi-screen-playback": {
        "own_group": "多屏播放",
        "why": "播放控制台：web/src/layout/components/PlaybackConsoleHost.vue（layout 级全局宿主，任何页面都能唤起）",
    },
}


def sys_api_statements(text):
    """切出所有 sys_api 语句文本（引号感知地按分号收尾）。"""
    out = []
    for m in API_STMT.finditer(text):
        i, quote = m.end(), None
        while i < len(text):
            c = text[i]
            if quote:
                if c == quote:
                    if i + 1 < len(text) and text[i + 1] == quote:
                        i += 2
                        continue
                    quote = None
            elif c in "'\"`":
                quote = c
            elif c == ";":
                out.append(text[m.start():i + 1])
                break
            i += 1
    return out


def sys_api_rows(text):
    """返回基线里所有 sys_api 行的影响集 {(method, path)}。"""
    rows = set()
    for stmt in sys_api_statements(text):
        for m in TRIPLE.finditer(stmt):
            rows.add((m.group(2), m.group(1)))
    return rows


class ButtonCatalogTest(unittest.TestCase):
    def setUp(self):
        self.catalog = json.loads(CATALOG.read_text())

    def test_catalog_has_unique_codes_and_source_evidence(self):
        buttons = self.catalog["buttons"]
        self.assertGreater(len(buttons), 98)
        codes = [row["permission"] for row in buttons]
        self.assertEqual(len(codes), len(set(codes)))
        for row in buttons:
            self.assertTrue(row["parentPath"], row)
            self.assertTrue(row["source"], row)
            for source in row["source"]:
                self.assertTrue((ROOT / source.split(":")[0]).is_file(), source)
            if not row["apis"]:
                self.assertTrue(row.get("notes"), row)
            for api in row["apis"]:
                self.assertIn(api["method"], ["GET", "POST", "PUT", "PATCH", "DELETE"])
                self.assertTrue(api["path"].startswith("/api/"), api)

    def test_frontend_permission_literals_are_registered(self):
        codes = {b["permission"] for b in self.catalog["buttons"]}
        codes.update(self.catalog.get("pagePermissions", []))
        for path in (ROOT / "web/src").rglob("*"):
            if path.suffix not in [".vue", ".ts", ".tsx"] or ".test." in path.name or "/mock/" in str(path):
                continue
            literals = re.findall(r'''['"]((?:gb28181|system|plugins):[A-Za-z0-9_:-]+)['"]''', path.read_text())
            self.assertFalse(set(literals) - codes, f"{path}: {set(literals) - codes}")

    def test_generated_groups_are_in_backend_controlled_list(self):
        """两个生成器写出的 api_group 必须是 Go 受控清单里的值。

        ⛔ 这条挡的是「兜底分组」复发：历史上 button-catalog.py 写死 `按钮权限目录`、
           guest-permissions.py 还额外写死 `游客权限依赖`，每加一批按钮就往里塞，
           最后漂到 87 + 2 行、跨 36 个页面，把分组维度彻底搞坏。
        """
        sys.path.insert(0, str(ROOT / "scripts"))
        import api_groups

        allowed = set(backend_group_enum())
        self.assertGreater(len(allowed), 0, f"{GROUP_ENUM} 里没解析出分组名")

        emitted = (
            set(api_groups.GROUP_BY_PAGE.values())
            | set(api_groups.API_GROUP_OVERRIDES.values())
            | set(api_groups.API_GROUP_BY_ROUTE.values())
            | {g for _, g in api_groups.API_GROUP_RULES}
        )
        self.assertFalse(emitted - allowed, f"生成器会写出清单外的分组: {sorted(emitted - allowed)}")

        # 按钮目录里出现的每个页面都必须能推导出分组
        for button in self.catalog["buttons"]:
            api_groups.group_of(button["parentPath"])

        # guest 迁移会补建的接口也必须能推导出分组
        guest = json.loads((DATABASE / "gb28181/guest-permissions.json").read_text())
        api_groups.group_for_api("POST", "/api/gb28181/stream-probes/:streamId")
        for binding in guest["bindings"]:
            for api in binding["apis"]:
                api_groups.group_for_api(api["method"], api["path"])

    def test_baselines_normalize_every_api_row_to_path_derived_group(self):
        """基线里出现的每个 sys_api 行，最终都要被「重定口径」段覆盖成路径推导值。

        ⛔ 这条挡的是「基线历史段带着旧分组交付」：基线是「建表 + 内联迁移历史」结构，
           历史段里的 INSERT 用的是当时的组名（`API管理` / `字典项管理` / `按钮权限目录`…）。
           上一版只重生成生成器产出块，主种子段与手写迁移块的 211 行**就这么漂着交付**，
           而当时没有任何断言发现 —— 新装库因此拿到的还是旧分组。

        判据：reroute 段的 (method,path) 集合 ⊇ 基线里所有 INSERT 的影响集，
              且 reroute 段写的值与 API_GROUP_RULES 一致。
        """
        sys.path.insert(0, str(ROOT / "scripts"))
        import api_groups

        for name in ["uvp-gb28181.sql", "postgresql_converted.sql", "sqlserver_converted.sql"]:
            with self.subTest(baseline=name):
                text = (DATABASE / name).read_text()
                inserted = sys_api_rows(text)
                self.assertTrue(inserted, f"{name}: 没解析到 sys_api 行，解析器失效")

                start = text.find("-- api-group-reroute:start")
                self.assertNotEqual(-1, start, f"{name}: 缺少 2026-09-21 API 分组重定口径段")
                rerouted = {
                    (m.group(4).upper(), m.group(3)): m.group(2)
                    for m in UPDATE_ROW.finditer(text[start:text.find("-- api-group-reroute:end", start)])
                }
                self.assertTrue(rerouted, f"{name}: reroute 段解析为空")

                missing = sorted(inserted - set(rerouted))
                self.assertFalse(missing, f"{name}: 这些接口行没被重定口径覆盖 → {missing[:8]}")

                for (method, path), group in rerouted.items():
                    want, rule = api_groups.match_rule(path)
                    self.assertIsNotNone(want, f"{name}: {method} {path} 命不中任何路径规则")
                    self.assertEqual(want, group, f"{name}: {method} {path} 在 reroute 段写成 {group}，规则要求 {want}")

    def test_multi_screen_group_holds_only_its_own_paths(self):
        """「多屏播放」组不得再当工作台的杂物筐。

        ⛔ 本仓在这里踩过两次，根因是同一个 —— 拿「按钮挂在多屏播放页」当分组判据：
           ① 第一轮把 `/api/gb28181/device-mgmt/channel/:id/*` 通道族 **34 条**
              吸进「多屏播放」，含路径明写 device-mgmt 的「下发目标跟踪」「下发设备配置」；
           ② 第二轮 `stream-probes` ×2 与 `play/:streamId/monitor` ×1 仍留在里面
              （老板 2026-09-21 报的正是这一处）。

        判据：规则表里**每一条指向「多屏播放」的规则**，其前缀必须落在
              `MULTI_SCREEN_OWN_PREFIXES` 内。往这个组里塞别的模块的路径，这条就红。
        """
        sys.path.insert(0, str(ROOT / "scripts"))
        import api_groups

        rules = [(p, g) for p, g in api_groups.API_GROUP_RULES if g == "多屏播放"]
        self.assertTrue(rules, "规则表里没有「多屏播放」的条目，规则表可能被误删")
        for prefix, _group in rules:
            self.assertTrue(
                any(prefix == own or prefix.startswith(own + "/") for own in MULTI_SCREEN_OWN_PREFIXES),
                "「多屏播放」多出了不属于自己的路径前缀 %r（只允许 %s）—— "
                "页面级分组不得承载别的模块的接口" % (prefix, list(MULTI_SCREEN_OWN_PREFIXES)),
            )

    def test_workspace_pages_do_not_collapse_into_one_group(self):
        """工作台页面的按钮**不得**把它们覆盖的接口压进同一个分组。

        ⭐ 为什么必须单独挡这条：`/gb28181/multi-screen-playback`（播放控制台）不是
           普通路由页 —— 它的宿主 `PlaybackConsoleHost.vue` 挂在 layout 上，
           **从任何页面都能唤起**。其上摆了 16 个按钮、覆盖 37 个接口，
           横跨「设备控制」「多屏播放」「流媒体管理」**三个模块**。
           若按「按钮在哪页」推分组，这 37 条会被压成一个组 —— 那就是上面两次返工。

        判据：该页按钮覆盖的接口必须落在 **≥2** 个分组里（工作台 ≠ 单模块），
              且每个接口都能命中路径规则（不许有「只能靠页面推」的行）。
        """
        sys.path.insert(0, str(ROOT / "scripts"))
        import api_groups

        for page, meta in WORKSPACE_PAGES.items():
            with self.subTest(page=page):
                apis = {}
                for button in self.catalog["buttons"]:
                    if button["parentPath"] != page:
                        continue
                    for api in button["apis"]:
                        apis[(api["method"].upper(), api["path"])] = button["title"]
                self.assertTrue(apis, f"{page} 下没解析到任何按钮接口（{meta['why']}）")

                groups = {}
                for _key, path in apis:
                    want, _rule = api_groups.match_rule(path)
                    self.assertIsNotNone(
                        want, f"{page}: {path} 命不中任何路径规则（分组不该靠页面兜底）")
                    groups.setdefault(want, 0)
                    groups[want] += 1

                self.assertGreater(
                    len(groups), 1,
                    "工作台页面 %s 的 %d 个接口全被压进同一个分组 %s —— "
                    "工作台不是模块，判据必须回到接口路径（%s）"
                    % (page, len(apis), sorted(groups), meta["why"]),
                )

    def test_multi_api_buttons_declare_every_api_title(self):
        """「1 个按钮挂多个接口」的按钮，其每个接口都必须有显式 `apis[].title`。

        ⛔ 这条挡的是「接口标题退化成按钮名」：
           `sys_api.title` 的库注释是「权限名称」，而生成器历史上直接拿归属按钮名填。
           按钮是**权限粒度**、接口是**接口粒度** ⇒ 一个 `gb28181:home:view`「查看仪表盘」
           要调 5 个接口（summary / layout / 3 个 drilldown），于是 5 行全叫「查看仪表盘」，
           接口管理页完全无法区分（老板 2026-09-21 报的正是这个）。
           库里有 70 行重复，生成器若全量插入则有 161 行重复。

        ⚠️ 只对「多接口」按钮强制：单接口按钮的回落名与接口一一对应，本来就不会歧义。
        """
        offenders = []
        for button in self.catalog["buttons"]:
            if len(button["apis"]) <= 1:
                continue
            for api in button["apis"]:
                if not (api.get("title") or "").strip():
                    offenders.append((button["permission"], api["method"], api["path"]))
        self.assertFalse(offenders, "这些『1 按钮多接口』的接口缺接口名 → " f"{offenders[:8]}")

    def test_emitted_titles_are_unique(self):
        """生成器会写出的接口标题必须**全局唯一**。

        ⛔ 为什么必须唯一而不是"和库里一致就行"：库里不重复只是因为那些行**恰好**被更早的
           来源（基线主种子 / 更早的手写迁移）用规范名插过，而所有 INSERT 都带
           `WHERE NOT EXISTS` 守卫 ⇒ **谁先插入谁定名**。那个"更早的来源"一变
           （迁移退役、路径改名、换新库），回落按钮名导致的重复立刻重现。
           把唯一性变成断言，才不依赖插入顺序。
        """
        emitted = {}
        for button in self.catalog["buttons"]:
            for api in button["apis"]:
                key = (api["method"].upper(), api["path"])
                emitted.setdefault(key, api.get("title") or button["title"])
        guest = json.loads(GUEST_CATALOG.read_text())
        for binding in guest.get("bindings", []):
            for api in binding["apis"]:
                key = (api["method"].upper(), api["path"])
                self.assertTrue(
                    (api.get("title") or "").strip(),
                    f"guest bindings 的 {key[0]} {key[1]} 缺接口名（guest 生成器会直接报错）",
                )
                emitted.setdefault(key, api["title"])
        self.assertGreater(len(emitted), 200, "接口集合解析异常")

        seen = {}
        dups = {}
        for key, title in sorted(emitted.items()):
            if title in seen:
                dups.setdefault(title, [seen[title]]).append(key[1])
            else:
                seen[title] = key[1]
        self.assertFalse(dups, f"生成器会写出重复的接口标题 → {dict(list(dups.items())[:5])}")

        # ⛔ 机械占位名守卫（本轮新增的回归类）。
        #    2026-08-30-media-management 迁移用 CONCAT('媒体管理 ',method,' ',path)
        #    给整个 `/api/gb28181/zlm/*` 族拼出了 58 行占位名，形如
        #    `媒体管理 POST zlm/nodes/:id/recordings/runtime/stop/preflight`。
        #    它们**字符串唯一 ⇒ 躲过了上面的重复检测**，但在接口管理页上完全不可读。
        #    最险的是：把它们当"库现值"抄进目录，等于**主动让生成器产出占位名**。
        mechanical = {
            key: title for key, title in emitted.items()
            if MECHANICAL_TITLE.search(title)
        }
        self.assertFalse(
            mechanical,
            f"生成器会写出机械占位名（应为接口功能名）→ {dict(list(mechanical.items())[:5])}",
        )

    def test_explicit_api_titles_do_not_conflict(self):
        """同一个 (path, method) 在目录里不得声明两个不同的接口名。

        （生成器自身也会报错，这条是把它钉进测试，避免只靠运行期发现。）
        """
        declared = {}
        for button in self.catalog["buttons"]:
            for api in button["apis"]:
                if not api.get("title"):
                    continue
                key = (api["method"].upper(), api["path"])
                declared.setdefault(key, set()).add(api["title"])
        conflicts = {k: sorted(v) for k, v in declared.items() if len(v) > 1}
        self.assertFalse(conflicts, f"同一接口被赋了多个名字 → {conflicts}")

    def test_title_migration_covers_every_declared_api(self):
        """接口标题归一化迁移必须覆盖**每个显式声明的接口名**，且目标值与目录一致。

        ⛔ 为什么必须有这条：两个生成器的 `sys_api` INSERT 都带 `WHERE NOT EXISTS`
           守卫、**没有 UPDATE 分支** ⇒ **改目录/改生成器修不了已有行**，
           必须配前向迁移（与 `api_group` 同一个坑）。目录加了新接口名而忘了同步迁移时，
           这条会红 —— 否则就是"新库对了、存量库还错着"的静默不一致。

        判据（双向）：
          ① 目录里每个**显式** `apis[].title` 都在迁移里有同值 UPDATE；
          ② 迁移里的每条 `(method,path)` 都在目录里有显式声明（不许出现野行）。
        ⚠️ 只比「显式声明」：单接口按钮的**回落按钮名**不落迁移 —— 那些行由
           基线/更早的迁移定义，生成器的 `WHERE NOT EXISTS` 从不会覆盖它们。
        """
        declared = {}
        for button in self.catalog["buttons"]:
            for api in button["apis"]:
                if api.get("title"):
                    declared[(api["method"].upper(), api["path"])] = api["title"]
        for binding in json.loads(GUEST_CATALOG.read_text()).get("bindings", []):
            for api in binding["apis"]:
                declared[(api["method"].upper(), api["path"])] = api["title"]
        # 无归属按钮的纯查询接口（`orphanApiTitles`）也要落迁移：
        # ⛔ 它们不是「可以不做」，而是**没有别的地方能覆盖** —— 没有任何按钮的
        #    apis[] 会包含它们（加进去会改变 button 的授权语义），
        #    而它们在库里由 2026-08-30-media-management 迁移用
        #    CONCAT('媒体管理 ',method,' ',path) 插入了机械占位名。
        #    有了这个键，迁移里的每一行都能回指到目录 ⇒ 反向「野行」断言才成立。
        for orphan in self.catalog.get("orphanApiTitles", []):
            declared[(orphan["method"].upper(), orphan["path"])] = orphan["title"]
        self.assertGreater(len(declared), 200, "显式接口名解析异常")

        for suffix in ["", "-postgresql", "-sqlserver"]:
            with self.subTest(dialect=suffix):
                path = DATABASE / "gb28181/migrations" / f"{TITLE_VERSION}{suffix}.sql"
                self.assertTrue(path.is_file(), f"缺少接口标题归一化迁移 {path.name}")
                rows = {
                    (m.group(3).upper(), m.group(2)): m.group(1).replace("''", "'")
                    for m in TITLE_ROW.finditer(path.read_text())
                }
                self.assertTrue(rows, f"{path.name}: 解析为空")
                missing = sorted(set(declared) - set(rows))
                self.assertFalse(missing, f"{path.name}: 这些接口没被标题迁移覆盖 → {missing[:8]}")
                stray = sorted(set(rows) - set(declared))
                self.assertFalse(stray, f"{path.name}: 迁移里有目录未声明的接口 → {stray[:8]}")
                wrong = {k: (v, rows[k]) for k, v in declared.items() if rows[k] != v}
                self.assertFalse(wrong, f"{path.name}: 迁移目标值与目录声明不一致 → {dict(list(wrong.items())[:5])}")

    def test_catalog_apis_are_registered_routes(self):
        routes = set()
        files = ["server/app/routes/routes.go", "server/app/gb28181/routes/routes.go", "server/plugins/example/routes/exampleroutes.go"]
        for filename in files:
            groups = {"engine": "", "protected": "/api", "public": "/api", "zlm": "/api/gb28181/zlm"}
            for line in (ROOT / filename).read_text().splitlines():
                group = re.search(r'(\w+)\s*:=\s*(\w+)\.Group\("([^"]*)"\)', line)
                if group and group[2] in groups:
                    groups[group[1]] = groups[group[2]] + group[3]
                route = re.search(r'(\w+)\.(GET|POST|PUT|PATCH|DELETE)\("([^"]*)"', line)
                if route and route[1] in groups:
                    routes.add((route[2], groups[route[1]] + route[3]))
        for button in self.catalog["buttons"]:
            for api in button["apis"]:
                self.assertIn((api["method"], api["path"]), routes, button["permission"])

    def database(self):
        db = sqlite3.connect(":memory:")
        db.executescript("""
        CREATE TABLE sys_menu(id INTEGER PRIMARY KEY AUTOINCREMENT,parent_id INTEGER,
          path TEXT,name TEXT,component TEXT,title TEXT,hide INTEGER,disable INTEGER,
          sort INTEGER,type INTEGER,permission TEXT,icon TEXT,created_at TEXT,
          updated_at TEXT,created_by INTEGER,deleted_at TEXT);
        CREATE TABLE sys_api(id INTEGER PRIMARY KEY AUTOINCREMENT,title TEXT,path TEXT,
          method TEXT,api_group TEXT,created_at TEXT,updated_at TEXT,created_by INTEGER,deleted_at TEXT);
        CREATE TABLE sys_menu_api(menu_id INTEGER,api_id INTEGER,PRIMARY KEY(menu_id,api_id));
        CREATE TABLE sys_role_menu(role_id INTEGER,menu_id INTEGER);
        CREATE TABLE sys_casbin_rule(ptype TEXT,v0 TEXT,v1 TEXT,v2 TEXT,v3 TEXT,v4 TEXT,v5 TEXT);
        INSERT INTO sys_menu(id,parent_id,path,type,permission) VALUES(1,0,'/system',1,''),
          (2,0,'/unrelated',2,''),(3,2,'',3,'unrelated:edit');
        INSERT INTO sys_api(id,title,path,method) VALUES(1,'old','/api/unrelated','PUT');
        INSERT INTO sys_menu_api VALUES(3,1);
        INSERT INTO sys_role_menu VALUES(1,3),(2,3);
        INSERT INTO sys_casbin_rule VALUES('p','role_2','/api/unrelated','PUT','*','','');
        """)
        paths = {b["parentPath"] for b in self.catalog["buttons"]}
        # Parent pages exist in a normal installation; dormant pages are seeded by this migration.
        dormant = {p["path"] for p in self.catalog.get("dormantPages", [])}
        for path in sorted(paths - dormant):
            db.execute("INSERT INTO sys_menu(parent_id,path,type,permission,disable) VALUES(0,?,2,'',0)", (path,))
        # Existing button and soft-deleted collision must not be conflated.
        first = self.catalog["buttons"][0]
        db.execute("INSERT INTO sys_menu(parent_id,path,type,permission,title) VALUES(2,'',3,?,'custom-title')", (first["permission"],))
        old_id = db.execute("SELECT last_insert_rowid()").fetchone()[0]
        db.execute("INSERT INTO sys_role_menu VALUES(2,?)", (old_id,))
        last = self.catalog["buttons"][-1]
        db.execute("INSERT INTO sys_menu(parent_id,path,type,permission,deleted_at) VALUES(2,'',3,?,'2020-01-01')", (last["permission"],))
        api = next(b["apis"][0] for b in self.catalog["buttons"] if b["apis"])
        db.execute("INSERT INTO sys_api(path,method,deleted_at) VALUES(?,?,'2020-01-01')", (api["path"], api["method"]))
        return db, first["permission"], old_id

    @staticmethod
    def snapshot(db, table):
        return db.execute(f"SELECT * FROM {table} ORDER BY 1,2").fetchall()

    def test_migrations_are_idempotent_and_preserve_grants(self):
        for suffix in ["", "-postgresql", "-sqlserver"]:
            with self.subTest(dialect=suffix):
                sql = (DATABASE / "gb28181/migrations" / f"{VERSION}{suffix}.sql").read_text()
                # SQL Server Unicode literals are normalized only for this SQLite semantic test.
                executable = re.sub(r"\bN'", "'", sql)
                db, existing_code, old_id = self.database()
                grants = {t: self.snapshot(db, t) for t in ["sys_role_menu", "sys_casbin_rule"]}
                db.executescript(executable)
                once = {t: self.snapshot(db, t) for t in ["sys_menu", "sys_api", "sys_menu_api"]}
                db.executescript(executable)
                for table, rows in once.items():
                    self.assertEqual(rows, self.snapshot(db, table), table)
                for table, rows in grants.items():
                    self.assertEqual(rows, self.snapshot(db, table), table)
                self.assertEqual([(3, 1)], db.execute("SELECT * FROM sys_menu_api WHERE menu_id=3").fetchall())
                self.assertEqual((old_id, "custom-title"), db.execute("SELECT id,title FROM sys_menu WHERE permission=? AND deleted_at IS NULL", (existing_code,)).fetchone())
                for b in self.catalog["buttons"]:
                    rows = db.execute("SELECT id,parent_id FROM sys_menu WHERE permission=? AND type=3 AND deleted_at IS NULL", (b["permission"],)).fetchall()
                    self.assertEqual(1, len(rows), b["permission"])
                    if b["permission"] != existing_code:
                        self.assertEqual(b["parentPath"], db.execute("SELECT path FROM sys_menu WHERE id=?", (rows[0][1],)).fetchone()[0])
                    links = set(db.execute("SELECT a.method,a.path FROM sys_menu_api ma JOIN sys_api a ON a.id=ma.api_id WHERE ma.menu_id=? AND a.deleted_at IS NULL", (rows[0][0],)))
                    expected = {(a["method"], a["path"]) for a in b["apis"]}
                    self.assertTrue(expected <= links, b["permission"])
                for page in self.catalog.get("dormantPages", []):
                    self.assertEqual((1, 1), db.execute("SELECT disable,hide FROM sys_menu WHERE path=?", (page["path"],)).fetchone())
                db.close()

    def test_missing_optional_page_does_not_create_orphan_buttons(self):
        db, _, _ = self.database()
        db.execute("DELETE FROM sys_menu WHERE path='/home'")
        sql = (DATABASE / "gb28181/migrations" / f"{VERSION}.sql").read_text()
        db.executescript(sql)
        self.assertEqual(0, db.execute("SELECT COUNT(*) FROM sys_menu WHERE permission LIKE 'gb28181:home:%'").fetchone()[0])
        self.assertEqual(0, db.execute("SELECT COUNT(*) FROM sys_api WHERE path LIKE '/api/gb28181/home/%' AND deleted_at IS NULL").fetchone()[0])
        db.close()

    def test_fresh_baselines_match_migrations(self):
        for suffix, name in [("", "uvp-gb28181.sql"), ("-postgresql", "postgresql_converted.sql"), ("-sqlserver", "sqlserver_converted.sql")]:
            migration = (DATABASE / "gb28181/migrations" / f"{VERSION}{suffix}.sql").read_text().strip()
            self.assertIn(migration, (DATABASE / name).read_text(), name)


if __name__ == "__main__":
    unittest.main()
