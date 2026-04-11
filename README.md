# eTerm

基于 [Bubble Tea](https://github.com/charmbracelet/bubbletea) 与 [Lip Gloss](https://github.com/charmbracelet/lipgloss) 的现代终端 SSH 客户端。

## 功能

- **SSH 终端** — 交互式 Shell，支持保活、断线重连、滚动回看
- **SFTP** — 双栏文件管理器，上传/下载/删除/建目录/重命名
- **主机管理** — 别名、分组、标签、搜索、克隆、导入导出 `~/.ssh/config`
- **网格卡片** — 多列主机展示，实时在线状态探测（●）
- **认证** — 密码、密钥、Agent、键盘交互、Kerberos/GSSAPI
- **密钥库** — 生成、导入、导出 SSH 密钥，本地加密存储
- **网络** — ProxyJump、ProxyCommand、HTTP/SOCKS5 代理、本地/远程/动态端口转发
- **命令片段** — 可复用命令模板，一键粘贴到 SSH 会话
- **隐藏主机** — 给主机打 `hidden` 标签隐藏，`H` 切换显示
- **CLI 直连** — `eterm [user@]host[:port] [-p port]`，不存在的主机自动创建
- **更新检查** — 启动时异步检查 GitHub 新版本
- **多标签页、鼠标支持、应用锁定、连接历史**

## 演示Demo

| 主机列表 | 主机编辑 |
|:---:|:---:|
| ![主机列表](doc/1.png) | ![主机编辑](doc/2.png) |

| SFTP 文件管理 | SSH 终端 |
|:---:|:---:|
| ![SFTP](doc/3.png) | ![SSH](doc/4.png) |

## 安装

```bash
go build -o eterm .
./eterm
```

## 快速开始

```bash
# TUI 模式
./eterm

# 直连（不存在则自动创建主机）
./eterm root@192.168.1.1
./eterm deploy@prod.example.com -p 2222
```

## 快捷键

在非 SSH 标签页按 `?` 查看完整快捷键。常用：

| 按键 | 功能 |
|------|------|
| `Enter` | SSH 连接 |
| `n` / `e` / `d` | 新建 / 编辑 / 删除 |
| `s` | SFTP |
| `c` | 复制 SSH 命令 |
| `C` | 克隆主机 |
| `h` / `H` | 隐藏主机 / 切换隐藏可见 |
| `/` | 搜索 |
| `t` | 分组 ↔ 标签视图 |
| `q` | 快速连接 |
| `Ctrl+Tab` | 下一标签页 |
| `?` | 所有快捷键 |

## 数据

默认路径：`~/.config/eterm/eterm.db`（SQLite），可用 `-c path` 指定。

敏感字段（密码、私钥）由主密码派生密钥加密。首次运行完成加密初始化，支持无密码模式。

## 调试

| 变量 | 作用 |
|------|------|
| `ETERM_DEBUG_KEYS` | 向 stderr 输出按键事件 |
| `ETERM_DEBUG_APP` | 向 stderr 输出 SSH/SFTP 连接日志 |

## 许可

[MIT](LICENSE)
