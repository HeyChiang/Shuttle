# Shuttle

一个面向多服务器场景的桌面 SSH 管理工具，基于 [Wails v3](https://v3.wails.io/)、Go 与 Vue 3 构建。

[English](README.md) | 简体中文 | [Español](README.es.md)

## 功能

- **主机档案** — 保存连接信息（地址、端口、用户名、密码 / 私钥、终端类型、编码），并按分组管理。
- **多会话终端** — 同时打开任意多个 SSH 会话，通过标签栏切换，可合并为网格分屏，也可把输入同步广播到多个会话。
- **SFTP 工作区** — 左右双栏文件管理，两侧可各自选择本地磁盘或远程服务器，支持拖拽上传、下载以及服务器之间的直接传输。
- **代码块** — 保存常用命令脚本，在终端页通过右侧抽屉一键执行或粘贴到当前会话。
- **加密导入导出** — 以口令加密的文件备份与恢复主机列表。
- **主题与字号** — 明暗两种外观，终端字号可调。
- **三语界面** — English / 简体中文 / Español，可在「设置 → 语言」中即时切换。

## 环境要求

- Go 1.26+
- Node.js 18+ 与 npm
- [Wails v3 CLI](https://v3.wails.io/getting-started/installation/) 与 [Task](https://taskfile.dev/)

## 开发

```bash
# 安装前端依赖
cd frontend && npm install && cd ..

# 以开发模式运行，支持热重载
wails3 dev
```

## 构建

```bash
wails3 build
```

产物会生成在 `bin/` 目录。

## 项目结构

```
.
├── main.go              # 应用入口与窗口配置
├── sshservice.go        # 暴露给前端的 Wails 服务
├── internal/sshcore/    # SSH 会话、SFTP、档案存储与加密
├── frontend/src/        # Vue 3 前端
│   ├── App.vue          # 主界面、标签栏、主机列表、设置
│   ├── components/      # 终端面板、SFTP 面板、各类弹窗
│   └── locales/         # en / zh-CN / es 语言词条
└── build/               # 各平台构建资源与 Taskfile
```

## 多语言

界面文案全部位于 `frontend/src/locales`。首次启动时按浏览器语言自动选择，之后保存在 `localStorage` 的 `shuttle.locale` 中。新增语言只需两步：

1. 复制 `en.ts` 为 `frontend/src/locales/<locale>.ts` 并翻译。
2. 在 `frontend/src/i18n.ts` 的 `messages` 与 `localeOptions` 中注册。

## 参与贡献

欢迎提交 Issue 与 Pull Request。新增界面文案请写入语言词条，不要直接硬编码在组件里。
