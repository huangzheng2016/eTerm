# eTerm

基于 [Bubble Tea](https://github.com/charmbracelet/bubbletea) 与 [Lip Gloss](https://github.com/charmbracelet/lipgloss) 的现代终端 SSH 客户端。

## 功能

- **SSH 终端** -- 交互式 Shell，支持保活、断线重连、滚动回看
- **SFTP** -- 双栏文件管理器，上传/下载/删除/建目录/重命名
- **主机管理** -- 别名、分组、标签、搜索、克隆、导入导出 `~/.ssh/config`
- **会话记录** -- 断开连接后保存终端文本快照，在 Sessions 标签中按主机查看历史输出
- **网格卡片** -- 多列主机展示，实时在线状态探测；可在设置中开启网格状态文字（ON/OFF/?），默认仅颜色圆点
- **认证** -- 密码、密钥、Agent、键盘交互、Kerberos/GSSAPI
- **密钥库** -- 生成、导入、导出 SSH 密钥，本地加密存储
- **网络** -- ProxyJump、ProxyCommand、HTTP/SOCKS5 代理、本地/远程/动态端口转发
- **命令片段** -- 可复用命令模板，一键粘贴到 SSH 会话
- **多设备同步** -- 通过 SSH stdio 或 HTTP/HTTPS 同步主机、密钥、片段等数据到远程服务端；age 加密传输，服务端只存密文
- **可配置快捷键** -- 所有快捷键均可在设置页修改，支持多键绑定
- **主密码管理** -- 设置页可修改主密码，自动重加密所有敏感字段
- **隐藏主机** -- 给主机打 `hidden` 标签隐藏，`H` 切换显示
- **CLI 直连** -- `eterm [user@]host[:port] [-p port]`，不存在的主机自动创建
- **更新检查** -- 解锁后异步检查 GitHub 新版本（6 小时节流；`--no-update-check` 或 `ETERM_NO_UPDATE_CHECK` 关闭）
- **多标签页、鼠标支持、应用锁定**

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

**从源码构建**

```bash
go build -o eterm .
./eterm
```

同步服务端（可选）：

```bash
go build -o etermsyncd ./cmd/etermsyncd
```

## 快速开始

```bash
# TUI 模式
./eterm

# 直连（不存在则自动创建主机）
./eterm root@192.168.1.1
./eterm deploy@prod.example.com -p 2222
```

## 命令行参数


| 参数                  | 说明                             |
| ------------------- | ------------------------------ |
| `-c path`           | SQLite 数据库路径                   |
| `-p port`           | 与直连主机一起使用时指定端口                 |
| `-v`                | 打印版本并退出                        |
| `--version-json`    | 以 JSON 打印 version / commit 并退出 |
| `--no-update-check` | 禁用解锁后的 GitHub 版本检查             |


## 快捷键

在非 SSH 标签页按 `?` 查看完整快捷键（与设置中的改键一致）。常用：


| 按键              | 功能                 |
| --------------- | ------------------ |
| `Enter`         | SSH 连接             |
| `n` / `e` / `d` | 新建 / 编辑 / 删除       |
| `s`             | SFTP               |
| `c`             | 复制 SSH 命令          |
| `C`             | 克隆主机               |
| `h` / `H`       | 隐藏主机 / 切换隐藏可见      |
| `/`             | 搜索                 |
| `t`             | 分组 / 标签视图          |
| `q`             | 快速连接               |
| `Esc`           | 打开菜单（退出 / 设置 / 同步） |
| `Ctrl+Shift+H`  | 当前主机会话历史           |
| `Ctrl+Space`    | 多选开关（批量打标签）        |
| `Ctrl+Shift+G`  | 批量添加标签             |
| `Ctrl+Tab`      | 下一标签页              |
| `?`             | 所有快捷键              |


导入 `~/.ssh/config` 时若存在重名主机，可选择跳过或覆盖。

## 多设备同步

ESC 菜单 -> Sync 打开同步设置。支持两种传输模式：

**SSH stdio 模式**（推荐）：客户端通过 SSH 在远程临时启动 etermsyncd，通过 stdin/stdout 交换 JSON，同步完成后进程退出。不需要常驻服务，SSH 本身提供认证和加密。

```bash
# 远程机器上部署 etermsyncd 二进制即可
scp etermsyncd remote-host:~/bin/
```

**HTTP/HTTPS 模式**：etermsyncd 作为常驻服务运行，客户端通过 HTTP API 同步。需要 Bearer token 鉴权。

```bash
# 启动服务端（HTTP 模式必须设置 API key）
etermsyncd -listen :8443 -db ./sync.db -api-key <token>

# HTTPS
etermsyncd -listen :8443 -db ./sync.db -api-key <token> -cert server.crt -key server.key
```

同步范围：Host、SSHKey（仅 database 模式）、Snippet、PortForward。HostFingerprint、ConnectionHistory、AppSetting 不同步。

数据在传输前经 age 加密（scrypt passphrase 模式），服务端只存密文，无法解密。

## 数据

默认路径：`~/.config/eterm/eterm.db`（SQLite），可用 `-c path` 指定。

敏感字段（密码、私钥）由主密码派生密钥加密。首次运行完成加密初始化，支持无密码模式。设置页可修改主密码。

偏好项（会话转录、网格状态文字）在设置标签页顶部 General 区切换，`Ctrl+S` 保存。

## 调试


| 变量                      | 作用                        |
| ----------------------- | ------------------------- |
| `ETERM_DEBUG_KEYS`      | 向 stderr 输出按键事件           |
| `ETERM_DEBUG_APP`       | 向 stderr 输出 SSH/SFTP 连接日志 |
| `ETERM_NO_UPDATE_CHECK` | 禁用 GitHub 版本检查            |


## 许可

[MIT](LICENSE)