# eTerm

基于 [Bubble Tea](https://github.com/charmbracelet/bubbletea) 与 [Lip Gloss](https://github.com/charmbracelet/lipgloss) 的现代终端 SSH 客户端。

[English](README_EN.md)

## 功能亮点

- **SSH / LocalShell** -- 多标签终端、滚动回看、断线提示与重连
- **主机与密钥管理** -- 主机分组、标签、搜索，内置 SSH 密钥库
- **导入** -- 支持导入 `~/.ssh/config` 与 Termius 主机数据
- **SFTP 与端口转发** -- 双栏文件管理，本地/远程/动态端口转发
- **同步与远程 Shell** -- 通过 syncd 在多台设备间同步配置，并打开同租户设备上的远程 Shell
- **AI 助手** -- `C-k` 打开全屏面板，直接操作终端标签页与远程 daemon，支持后台子代理与可选的本地工具
- **语音输入** -- `C-r` 语音转文字，本地 sherpa-onnx 离线识别或火山引擎云端识别
- **效率工具** -- 命令片段、会话记录、剪贴板链接粘贴、命令面板、可配置快捷键
- **安全存储** -- 主密码加密敏感字段；同步数据传输前加密，服务端只保存密文

## 演示

| 主机列表 | 主机编辑 |
|:---:|:---:|
| ![主机列表](doc/1.png) | ![主机编辑](doc/2.png) |

| SFTP 文件管理 | SSH 终端 |
|:---:|:---:|
| ![SFTP](doc/3.png) | ![SSH](doc/4.png) |

## 安装

从 [Releases](https://github.com/huangzheng2016/eTerm/releases) 下载与 CI 一致的压缩包，或一键安装（需已安装 `curl`，Windows 可选 Git Bash 或下方 PowerShell）：

**Linux / macOS / Windows (Git Bash)**

```bash
curl -fsSL https://raw.githubusercontent.com/huangzheng2016/eTerm/master/scripts/install.sh | sh
```

默认安装到 `~/.local/bin/eterm`（Windows 为 `~/bin/eterm.exe`）。可设置环境变量 `INSTALL_DIR` 指定目录。

**Windows (PowerShell)**

```powershell
iwr -useb https://raw.githubusercontent.com/huangzheng2016/eTerm/master/scripts/install.ps1 | iex
```

从源码构建见 [开发者文档](docs/dev.md#从源码构建)。

## 快速开始

```bash
# TUI 模式
./eterm

# 直连（不存在则自动创建主机）
./eterm root@192.168.1.1
./eterm deploy@prod.example.com -p 2222
```

## 快捷键

在非 SSH 标签页按 `?` 查看完整快捷键（与设置中的改键一致）。常用：

| 按键              | 功能                 |
| --------------- | ------------------ |
| `Enter`         | SSH 连接             |
| `n` / `e` / `d` | 新建 / 编辑 / 删除       |
| `s`             | SFTP               |
| `c`             | 复制 SSH 命令          |
| `C`             | 克隆主机               |
| `h` / `H`       | 切换隐藏可见 / 隐藏主机      |
| `/`             | 搜索                 |
| `Esc`           | 打开菜单（退出 / 设置 / 同步） |
| `C-S-i`         | 上传剪贴板文件/图片并粘贴链接   |
| `C-Tab`         | 下一标签页              |
| `C-k`           | AI 助手               |
| `C-p`           | 命令面板               |
| `C-r`           | 语音输入               |
| `?`             | 所有快捷键              |

快捷键显示使用短写：`C` = Ctrl，`S` = Shift，`A` = Alt，例如 `C-S-i` 表示 `Ctrl+Shift+I`。

从旧版本升级时键位自动迁移：AI 助手占用 `C-k` 后，原来在 `C-k` 的命令面板移到 `C-p`；若 `C-p` 已被其他功能占用，该功能恢复默认键。

Windows 默认使用 `A-S-字母` 代替 `C-S-字母`，因为 Windows 的终端输入可能丢失 Ctrl 与字母组合中的 Shift 状态。

## 详细文档

- **[AI 助手](docs/user.md#ai-助手)** -- 全屏面板操作终端与远程 daemon，斜杠命令、后台子代理、本地工具
- **[语音输入](docs/user.md#语音输入)** -- helper 与模型下载、本地/云端识别引擎、VAD 与句尾动作设置
- **[多设备同步](docs/user.md#多设备同步)** -- syncd 部署（HTTP / SSH 模式）、远程 Shell daemon、临时 Shell 分享
- **[剪贴板链接粘贴](docs/user.md#剪贴板链接粘贴)** -- 剪贴板文件/图片上传 syncd 后粘贴短链
- **[临时 Shell 分享](docs/user.md#临时-shell-分享)** -- 限时分享链接，浏览器经 xterm.js 直连远程 Shell
- **[终端 OSC 支持](docs/user.md#终端-osc-支持)** -- OSC 8/9/0/2/133 透传与 shell 集成
- **[推荐 tmux 配置](docs/user.md#推荐-tmux-配置)** -- 启用 OSC52 剪贴板与 extended keys
- **[Windows 与无 tmux 设备](docs/user.md#windows-与无-tmux-设备)** -- ConPTY 本地 Shell，daemon 托管会话替代 tmux
- **[开发者文档](docs/dev.md)** -- 完整命令行参数、从源码构建、数据目录、调试（pprof）

## 许可

[MIT](LICENSE)
