# UVP GB28181 前端

UVP 国标视频接入管理平台的 Web 前端。基于 Vue 3 + Vite + TypeScript + Arco Design 构建，
覆盖设备接入、实时预览、录像回放、图像库、流媒体节点管理、语音对讲、级联、
系统管理与运维可观测性等业务界面。

后端服务见同级目录 `server/`，完整项目说明见[仓库根 README](../README.md)。

## 技术栈

| 类别      | 技术                                |
| --------- | ----------------------------------- |
| 前端框架  | Vue 3（Composition API）            |
| 构建工具  | Vite 6.x                            |
| 编程语言  | TypeScript                          |
| 状态管理  | Pinia + pinia-plugin-persistedstate |
| 路由管理  | Vue Router 4.x                      |
| UI 组件库 | Arco Design Vue                     |
| 样式处理  | Sass（sass-embedded）、CSS Modules  |
| HTTP 请求 | Axios                               |
| 国际化    | vue-i18n                            |
| 视频播放  | EasyPlayer.js                       |
| 单元测试  | Vitest                              |
| 代码校验  | ESLint + Stylelint + Prettier       |
| Git 规范  | husky + lint-staged + commitlint    |

## 开发规范

组件选型、业务组件复用与统一封装要求见
[前端开发规范](../docs/frontend-development-guidelines.md)。

## 目录结构

```
web/
├── public/                  # 静态资源（不参与打包）
├── src/
│   ├── api/                 # 接口请求定义，按模块分类
│   ├── assets/              # 图片、字体等静态资源
│   ├── components/          # 全局通用组件
│   ├── config/              # 全局常量配置
│   ├── directives/          # 自定义指令
│   ├── globals/             # 全局挂载函数或属性
│   ├── hooks/               # Composition API 封装
│   ├── lang/                # 多语言配置
│   ├── layout/              # 主布局组件
│   ├── mock/                # 本地 Mock 数据服务
│   ├── router/              # 路由配置
│   ├── store/               # Pinia 状态管理
│   ├── style/               # 全局样式与 SCSS 变量
│   ├── typings/             # TypeScript 类型声明扩展
│   ├── utils/               # 工具函数库
│   ├── views/               # 页面视图组件
│   ├── main.ts              # 应用入口
│   └── App.vue              # 根组件
├── build/                   # Vite 构建相关配置
├── .husky/                  # Git 提交钩子
└── 各类 lint 配置文件       # ESLint、Prettier、Stylelint、commitlint
```

## 环境要求

- **Node.js**：>= 18.12.0（推荐 20.12.0+）
- **pnpm**：>= 8.7.0（项目强制使用 pnpm，`preinstall` 会校验）
- **git**：用于克隆项目代码和版本控制

## 快速开始

### 克隆项目

```bash
git clone git@github.com:Frank-Coding97/uvp-gb28181.git
cd uvp-gb28181/web
```

### 安装依赖

```bash
pnpm install
```

### 开发调试

```bash
pnpm dev
```

开发服务器默认监听 `5177` 端口，通过环境变量文件配置后端地址。

### 构建部署

```bash
# 开发环境构建
pnpm build:dev

# 测试环境构建
pnpm build:test

# 生产环境构建
pnpm build:prod

# 预览构建产物
pnpm preview
```

构建产物输出到 `web/dist/`，由后端静态目录或独立 nginx 托管。

## 常用命令

| 命令                 | 说明                   |
| -------------------- | ---------------------- |
| `pnpm dev`           | 启动开发服务器         |
| `pnpm build:prod`    | 生产构建（含类型检查） |
| `pnpm preview`       | 本地预览构建产物       |
| `pnpm test:unit`     | 运行 Vitest 单元测试   |
| `pnpm lint:eslint`   | ESLint 检查并自动修复  |
| `pnpm lint:prettier` | Prettier 格式化 `src/` |

## 功能特性

- 动态路由：支持后端下发的菜单权限生成路由
- 权限控制：RBAC 权限模型，菜单权限与按钮权限双向校验
- 多主题：支持亮色 / 暗色主题与自定义主题色
- 国际化：多语言支持（i18n）
- 标签页管理：多标签页操作与 `keep-alive` 页面状态保持
- 响应式布局：适配多种屏幕尺寸
- SVG 图标：图标系统支持

## 许可证

本项目基于 [MIT License](LICENSE) 开源。第三方组件的版权与许可声明见
[THIRD-PARTY-NOTICES.md](../THIRD-PARTY-NOTICES.md)。
