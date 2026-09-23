"""API 分组的共享映射：接口路径 → 受控分组名。

背景见 docs/api-group-normalization.md。「接口管理」的 api_group 过去是自由文本，
两个生成器各自写死兜底分组（`按钮权限目录` / `游客权限依赖`），把分组维度搞坏了。
现在生成器只允许写出受控清单里的值。

⛔⛔ 判定键的选择（2026-09-21 一次返工，别改回去）
--------------------------------------------------
**第一版用「按钮所在页面」推分组，结果跑偏**：把整片通道级设备操作
（云台/对讲/抓拍/配置下发/视频参数/目标跟踪/回放会话，共 34 个接口）算进了
「多屏播放」。原因很具体 ——「播放控制台」是个**工作台页面**，这些按钮全摆在它上面：

    迁移作者复用既有按钮 gb28181:ptz:control / ptz:view（不新造权限码）
      → 这两个按钮挂在 140368「多屏播放」下
      → 按「按钮在哪个页面」推 ⇒ 全判成「多屏播放」

**页面回答的是「谁在哪个界面用到它」，分组要回答的是「这个接口属于哪个功能域」**，
两者在工作台页面上必然打架。所以判定键改成**接口自己的路径**（后端自己声明的模块归属），
最长前缀匹配；按钮页面退为兜底与审计。

例外只有一处（下方 RULES 里有注释）：`device-mgmt/device/:id/sip-trace-capture*`
路径在 device-mgmt 下，但功能属「SIP 日志」，按钮 140362 就挂在日志中心页。

受控清单的唯一真源是 server/app/models/sysapigroup.go 的 sysApiGroups，
本模块的取值必须落在其中（由 scripts/button-catalog.test.py 交叉校验）。
"""

# 接口路径前缀 → 受控分组名。
# 匹配规则：**最长前缀优先**（由 ORDERED_RULES 排序实现），
# 例如 /api/gb28181/device-mgmt/channel/:id/ptz 优先于 /api/gb28181/device-mgmt。
# ⛔ 每条规则都必须能命中至少一行（button-catalog.test.py 从基线逐条反查），
#    命中 0 行说明规则过时了，要删掉而不是留着。
API_GROUP_RULES = [
    # ---------------- 平台管理域 ----------------
    ("/api/login", "认证管理"),
    ("/api/refreshToken", "认证管理"),
    ("/api/captcha", "认证管理"),
    # 「个人中心」的自助接口路径混在 /api/users 下，必须逐条列
    # （按钮都在 userinfo 页：140252 改密码 / 140264 改基本信息 / 140485 传头像 / 1007 当前用户信息）
    ("/api/users/profile", "个人中心"),
    ("/api/users/updateAccount", "个人中心"),
    ("/api/users/updateBasicInfo", "个人中心"),
    ("/api/users/uploadAvatar", "个人中心"),
    ("/api/users/logout", "认证管理"),
    ("/api/users", "用户管理"),
    ("/api/sysMenu", "菜单管理"),
    ("/api/sysRole", "角色管理"),
    ("/api/sysDepartment", "部门管理"),
    ("/api/sysDictItem", "字典管理"),
    ("/api/sysDict", "字典管理"),
    ("/api/sysOnlineUser", "在线用户"),
    ("/api/sysApi", "接口管理"),
    ("/api/sysAffix", "文件管理"),
    ("/api/config", "系统配置"),
    ("/api/sysParam", "系统配置"),
    ("/api/codegen", "代码生成"),
    ("/api/sysGen", "代码生成"),
    # 定时任务日志的查看页在「日志中心 / 定时任务日志」，删除按钮也挂在那一页
    ("/api/sysJobResults", "日志中心"),
    ("/api/sysJobs", "定时任务"),
    ("/api/sysLoginLog", "日志中心"),
    ("/api/sysOperationLog", "日志中心"),
    ("/api/pluginsmanager", "插件管理"),
    ("/api/plugins", "插件示例"),

    # ---------------- GB28181 域 ----------------
    ("/api/gb28181/home", "仪表盘"),

    # ⭐ 例外：SIP 诊断采集的路径在 device-mgmt 下，但功能属「SIP 日志」，
    #    按钮 140362 就挂在日志中心页；纯按路径会误判成设备管理。
    ("/api/gb28181/device-mgmt/device/:id/sip-trace-capture", "日志中心"),

    ("/api/gb28181/device-mgmt/permission-workbench", "设备权限工作台"),
    ("/api/gb28181/device-mgmt/snapshots", "图像库"),

    # 通道级「控制/操作」子路径 → 设备控制
    # （第一版被按钮页面带进「多屏播放」的就是这 34 条）
    ("/api/gb28181/device-mgmt/channel/:id/ptz", "设备控制"),
    ("/api/gb28181/device-mgmt/channel/:id/control-capabilities", "设备控制"),
    ("/api/gb28181/device-mgmt/channel/:id/device-status", "设备控制"),
    ("/api/gb28181/device-mgmt/channel/:id/device-control", "设备控制"),
    ("/api/gb28181/device-mgmt/channel/:id/talk-sessions", "设备控制"),
    ("/api/gb28181/device-mgmt/channel/:id/snapshot-sessions", "设备控制"),
    ("/api/gb28181/device-mgmt/channel/:id/video-params", "设备控制"),
    ("/api/gb28181/device-mgmt/channel/:id/device-configs", "设备控制"),
    ("/api/gb28181/device-mgmt/channel/:id/target-track", "设备控制"),
    ("/api/gb28181/device-mgmt/channel/:id/playback-sessions", "设备控制"),
    ("/api/gb28181/device-mgmt/channel/:id/download-sessions", "设备控制"),

    # 存储卡与「重启/升级/维护」同族；菜单里格式化存储卡就挂在设备列表下
    ("/api/gb28181/device-mgmt/channel/:id/storage-cards", "设备管理"),
    # 其余 device-mgmt（通道/设备的增删改查、目录、地图、分组…）
    ("/api/gb28181/device-mgmt", "设备管理"),
    ("/api/gb28181/device-traffic", "设备管理"),
    ("/api/gb28181/device/", "设备管理"),

    ("/api/gb28181/alarms", "告警管理"),
    ("/api/gb28181/cloud-recordings", "云端录像"),
    ("/api/gb28181/recording-plans", "录像计划"),
    ("/api/gb28181/cascade", "国标级联"),

    ("/api/gb28181/sip/platform", "SIP 接入信息"),
    ("/api/gb28181/sip/setup", "SIP 接入信息"),
    ("/api/gb28181/sip/qr", "SIP 接入信息"),
    ("/api/gb28181/sip/service-config", "国标服务配置"),
    ("/api/gb28181/sip/dashboard", "仪表盘"),

    ("/api/gb28181/sip-traces", "日志中心"),
    ("/api/gb28181/logs", "日志中心"),
    ("/api/gb28181/security", "国标接入安全"),
    ("/api/gb28181/zlm", "流媒体管理"),

    # ⭐ 媒体流层能力：后端是**独立子包** streamprobe / streammonitor（与 zlm 同级），
    #    观测的是「流」（帧到达时间线、卡顿、实时指标），与 /api/gb28181/zlm 同域。
    #    ⛔ 它们过去被归到「多屏播放」，理由是「按钮也挂在多屏播放页」—— 那个判据
    #      在这里不成立：140368「多屏播放」是**播放控制台的主入口页**，而控制台本身是
    #      layout 级全局宿主（web/src/layout/components/PlaybackConsoleHost.vue），
    #      从任何页面都能唤起 ⇒ 按钮挂在哪页**不代表功能属于哪个模块**。
    #      同一批按钮里路径带 device-mgmt 的 23 条已在上一轮挪去「设备控制」，
    #      这两族因为路径里没这个词被原地留下（老板 2026-09-21 报的正是这一处）。
    ("/api/gb28181/stream-probes", "流媒体管理"),
    ("/api/gb28181/play/:streamId/monitor", "流媒体管理"),

    # 真·多屏播放：分屏布局方案 + 通道收藏 + 播放地址/点播动作
    ("/api/gb28181/playback-schemes", "多屏播放"),
    ("/api/gb28181/channel-favorite-groups", "多屏播放"),
    ("/api/gb28181/play", "多屏播放"),
    ("/api/gb28181/openapi-clients", "OpenAPI 客户端"),
]

# 页面路径 → 受控分组名。
# ⚠️ 现在只作**兜底与审计**用（路径规则命不中时才回落；以及测试里逐页体检）。
# ⛔ 不要再用它作为主判定键 —— 那正是上面注释里那次返工的根因。
GROUP_BY_PAGE = {
    "/home": "仪表盘",
    "/gb28181/device-mgmt/index": "设备管理",
    "/gb28181/multi-screen-playback": "多屏播放",
    "/gb28181/device-record-playback/:channelId": "多屏播放",
    "/gb28181/alarm-management": "告警管理",
    "/gb28181/cloud-recordings": "云端录像",
    "/gb28181/recording-schedules": "录像计划",
    "/gb28181/cascade": "国标级联",
    "/gb28181/sip/platform": "SIP 接入信息",
    "/gb28181/sip/config": "国标服务配置",
    "/security-preview": "国标接入安全",
    "/gb28181/device-assignment": "设备权限工作台",
    "/media/ingress": "流媒体管理",
    "/media/monitoring": "流媒体管理",
    "/media/nodes": "流媒体管理",
    "/media/scheduling": "流媒体管理",
    "/gb28181/sip-traces": "日志中心",
    "/system/login-log": "日志中心",
    "/system/log": "日志中心",
    "/system/joblog": "日志中心",
    "/system/account": "用户管理",
    "/system/role": "角色管理",
    "/system/menu": "菜单管理",
    "/system/division": "部门管理",
    "/system/dictionary": "字典管理",
    "/system/userinfo": "个人中心",
    "/system/online-user": "在线用户",
    "/system/api": "接口管理",
    "/system/affix": "文件管理",
    "/system/sysconfig": "系统配置",
    "/system/sysparam": "系统配置",
    "/system/codegen": "代码生成",
    "/system/sysjobslist": "定时任务",
    "/system/pluginsmanager": "插件管理",
    "/plugins/example": "插件示例",
    "/gb28181/snapshot-library": "图像库",
}

# 同一接口被多个页面共用、且**路径规则也判不出来**时的显式归属；
# 缺省表示"不该出现，出现就报错"。
# 目前为空 —— 上一版需要它，是因为判定键是按钮页面；换成路径后跨页面冲突自然消解
# （例如 sip/qr/token 同挂「国标服务配置」与「SIP 接入信息」，按路径 /sip/qr 唯一确定）。
API_GROUP_OVERRIDES = {}

# 不由按钮目录产生、且路径规则命不中的接口 → 显式归属。
# 目前为空（原先 10 条已全部被 API_GROUP_RULES 覆盖）。
API_GROUP_BY_ROUTE = {}


def _ordered_rules():
    """按前缀长度倒序排列：实现「最长前缀优先」。"""
    return sorted(API_GROUP_RULES, key=lambda kv: -len(kv[0]))


ORDERED_RULES = _ordered_rules()


def group_of(page_path):
    """页面路径 → 受控分组名（兜底/审计用）；未映射直接报错，不允许兜底。"""
    if page_path not in GROUP_BY_PAGE:
        raise SystemExit("页面 %r 未映射到受控分组；请补 GROUP_BY_PAGE" % page_path)
    return GROUP_BY_PAGE[page_path]


def match_rule(path):
    """返回 (分组名, 命中的前缀)；命不中返回 (None, None)。"""
    for prefix, group in ORDERED_RULES:
        if path.startswith(prefix):
            return group, prefix
    return None, None


def group_for_api(method, path, owners=None):
    """接口 → 受控分组名。

    判定顺序（**路径优先**，见模块头注释）：
      1. 显式 (method, path) 覆盖
      2. 接口路径前缀规则（最长匹配）  ← 主判定键
      3. 不由按钮目录产生的接口的显式表
      4. 按钮目录里的页面归属（owners 一致时）← 兜底
      5. 报错，不做默认兜底 —— 兜底正是这次要治的病
    """
    override = API_GROUP_OVERRIDES.get((method, path))
    if override:
        return override

    by_path, _ = match_rule(path)
    if by_path:
        return by_path

    route = API_GROUP_BY_ROUTE.get((method, path))
    if route:
        return route

    if owners:
        groups = sorted({group_of(b["parentPath"]) for b in owners})
        if len(groups) == 1:
            return groups[0]
        raise SystemExit(
            "接口 %s %s 跨多个分组 %s 且路径规则命不中；请在 API_GROUP_RULES 或 API_GROUP_OVERRIDES 里显式指定"
            % (method, path, groups)
        )

    raise SystemExit("无法推导接口 %s %s 的分组；请补 API_GROUP_RULES" % (method, path))
