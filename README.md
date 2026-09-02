# eTerm

基于 [Bubble Tea](https://github.com/charmbracelet/bubbletea) 与 [Lip Gloss](https://github.com/charmbracelet/lipgloss) 的现代终端 SSH 客户端。

## 功能

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

**从源码构建**

```bash
go build -o eterm .
./eterm
```

同步服务端（可选）：

```bash
go build -o etermsyncd ./cmd/etermsyncd
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o etermsyncd-linux ./cmd/etermsyncd
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

子命令：

| 命令              | 说明                             |
| ----------------- | ------------------------------ |
| `eterm version`   | 打印版本并退出（同 `-v`）            |
| `eterm upgrade`   | 启动 TUI 并强制检查更新，有新版本时提示升级   |
| `eterm daemon ...` | 管理远程 Shell daemon，见「多设备同步」一节 |


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


导入 `~/.ssh/config` 时若存在重名主机，可选择跳过或覆盖。

快捷键显示使用短写：`C` = Ctrl，`S` = Shift，`A` = Alt，例如 `C-S-i` 表示 `Ctrl+Shift+I`。

从旧版本升级时键位自动迁移：AI 助手占用 `C-k` 后，原来在 `C-k` 的命令面板移到 `C-p`；若 `C-p` 已被其他功能占用，该功能恢复默认键。

Windows 默认使用 `A-S-字母` 代替 `C-S-字母`，因为 Windows 的终端输入可能丢失 Ctrl 与字母组合中的 Shift 状态。

## AI 助手

`C-k` 打开全屏 AI 助手面板，再按一次（或 `Esc`）收起；收起后当前 run 在后台继续，状态栏显示 `ai running`。会话内容只在 `/new` 时清空。

Provider：首次启动自动导入 `~/.kimi-code/config.toml` 中 api_key 类型的 provider（OAuth 类型跳过），也可在面板中手动添加。`/model`（或面板内 `C-p`）选择模型。会话保存在 SQLite `ai_sessions` 表，`/resume` 恢复。

斜杠命令：

| 命令       | 功能                                  |
| -------- | ----------------------------------- |
| `/model`   | 选择 provider / 模型                    |
| `/new`     | 新会话                                 |
| `/resume`  | 恢复历史会话                              |
| `/fork`    | 分叉当前会话                              |
| `/undo`    | 撤销上一轮                               |
| `/tasks`   | 后台子代理列表（j/k 移动、enter 查看、x 取消）      |
| `/help`    | 帮助                                  |

面板按键：enter 发送；运行中继续输入会排队（Queued），在下一步边界注入当前 run；`C-c` 中断当前 run；`C-o` 展开/折叠工具输出；标题栏显示 context 用量（已用/上限）。清空会话用 `/new`。

终端控制工具：

- 标签页：`list_tabs` / `read_tab`（`skip_from_end` 向前翻历史）/ `send_keys`（解码 `\n` `\r` `\t` `\xHH` 转义，等 OSC 133;D 或超时后返回屏幕尾部）
- 远程 daemon（仅在已注册 daemon 时挂载）：`list_daemons` / `list_daemon_sessions` / `enter_daemon` / `create_session` / `rename_session` / `kill_session`
- 打开会话：`open_local_terminal` / `open_ssh`（按 `list_hosts` 的主机名，重名报歧义）/ `open_tmux`（按 `list_tmux_sessions` 的会话名）
- 其他：`sleep`（最长 10 分钟）；`spawn_agent` / `wait_agent` / `list_agents` 后台子代理（最多 4 个并发）；`notify` 桌面通知（OSC 9）

本地工具：`bash` 与 `str_replace_editor`（读/写/改/undo）。无沙箱，以当前用户完整权限执行。

## 语音输入

`C-r` 切换录音（终端无法感知按键抬起，因此是开关而非按住说话）。识别文本送入当前终端（等同粘贴）或 AI 面板输入框；句尾动作为 enter 时识别完一句直接提交。

helper 或模型未就绪时按 `C-r` 会打开设置面板引导下载。设置面板也可从命令面板或 `Esc` 菜单（`v`）进入：

- helper：一键下载 CI 构建的 voicehelper（release 产物 `voicehelper-<os>-<arch>.tar.gz`，darwin-arm64 / linux-amd64，约 45 MB，含 sherpa-onnx 动态库）
- 模型：SenseVoice 2024-07-17（约 1 GB，默认；同包含 fp32/int8 两套权重）/ Paraformer zh-small int8（约 74 MB）；模型含两套权重时主视图显示 Precision 开关（自定义目录需同时有 model.onnx 与 model.int8.onnx）
- Engine：enter 进入子菜单选择，顺序为 local（sherpa-onnx 离线识别）/ volcano（火山引擎云端识别，需 API key / App key / Access key，加密存储）/ 其他云端引擎
- speech sensitivity (0-1)：VAD 触发灵敏度
- end-of-sentence silence (ms)：句尾静音判停时长
- Sentence end：句尾动作 enter / space
- 测试录音：验证当前配置

## 多设备同步

`Esc` -> Sync 打开同步设置。默认使用 HTTP syncd。

最小启动：

```bash
etermsyncd -listen :8443 -db ./sync.db -api-key <token>
```

生产环境通常让 syncd 监听本机端口，由反向代理负责 HTTPS：

```bash
sudo install -m 0755 etermsyncd-linux /usr/local/bin/etermsyncd
sudo install -d -m 0755 /etc/etermsyncd /var/lib/etermsyncd
printf 'ETERMSYNCD_API_KEY=%s\n' '<token>' | sudo tee /etc/etermsyncd/etermsyncd.env
sudo chmod 600 /etc/etermsyncd/etermsyncd.env
```

`/etc/systemd/system/etermsyncd.service`：

```ini
[Unit]
Description=eTerm sync daemon
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
EnvironmentFile=/etc/etermsyncd/etermsyncd.env
ExecStart=/usr/local/bin/etermsyncd -listen 127.0.0.1:8080 -db /var/lib/etermsyncd/sync.db
Restart=always
RestartSec=3
User=root

[Install]
WantedBy=multi-user.target
```

```bash
sudo systemctl daemon-reload
sudo systemctl enable --now etermsyncd
```

需要远程 Shell 时，在可访问的 eTerm 主机上启动 daemon：

```bash
eterm daemon start    # 后台启动
eterm daemon status
eterm daemon stop
eterm daemon run      # 前台运行（start 实际以 daemon run 拉起子进程）
```

daemon 子命令的可选参数：`-c path`（数据库路径）、`-password <主密码>`（也可用环境变量 `ETERM_MASTER_PASSWORD`）、`-name <显示名>`（默认主机名）、`-pprof <地址>`。

daemon 与 syncd 的 relay 协议版本不匹配时，daemon 会报错并以退出码 1 退出，不再自动重连。升级 syncd 或 eterm 后，需要用与 syncd 匹配的新版 eterm 重新 `eterm daemon start`。

也可以选 SSH 模式：在某台可通过 SSH 登录的主机上常驻 etermsyncd（同上，监听 `127.0.0.1:18443`），同步设置里选 SSH 并填该主机和 Remote Port（默认 18443），API Key 与远端一致。客户端会用 SSH 本地端口映射访问远端的 HTTP API，records 同步、远程 Shell 和剪贴板托管与 HTTP 模式完全一致。注意 SSH 主机需要先交互连接一次以信任指纹。

同步数据在上传前加密，syncd 不保存明文。

## 剪贴板链接粘贴

在 `[L]` 本地 Shell、`[S]` SSH Shell、`[R]` 远程 Shell 中可使用：

- 普通粘贴：本地 Shell 和本地 tmux 中，剪贴板里的本地文件会粘贴为 `[filename](file:///path)`；SSH 和远程 Shell 会上传到 syncd 后粘贴链接
- `C-S-i`：强制读取系统剪贴板文件/图片，上传到 syncd，向当前 Shell 粘贴 `[filename](url)`
- `C-p` -> `Paste URL`：同样强制上传，适合作为兜底入口

短链格式为 `https://sync.example.com/b/<token>`（SSH 模式下为 `http://127.0.0.1:<remote port>/b/<token>`，在远端主机上访问），有效期 30 分钟。文件/图片最大 10 MiB。

普通文本粘贴不受影响。纯图片剪贴板通常不会触发终端文本粘贴事件，请使用 `C-S-i` 或命令面板入口上传。

## 临时 Shell 分享

在主机列表选中在线的远程设备按 Enter 打开远程菜单，选中设备或 tmux 会话后按 `s`，在弹窗中输入有效期（小时）和名称，即可生成一条 `https://<sync server>/x/<token>` 分享链接。访客用浏览器打开链接，通过 xterm.js 直接进入该 Shell，可读写。

- 单连接顶替：新访客打开链接会把当前已连接的访客踢下线
- 有效期在创建时固定，不随访问续期，到期后连接自动断开
- 设置项 `share_max_hours`（1-168，默认 4）仅作为弹窗中有效期的默认值，可按需修改

安全提示：链接中的 token 即访问凭证，任何拿到链接的人都能读写该 Shell，请像对待密码一样分发和保管。

## 终端 OSC 支持

- OSC 8：超链接透传到外层终端，可点击
- OSC 9：通知透传到外层终端
- OSC 0/2：动态标签页标题；手动改名后不再跟随远端设置
- OSC 133：shell 集成命令跟踪。本地 Shell 与本地 tmux 自动为 zsh/bash/fish 注入集成（设 `ETERM_NO_SHELL_INTEGRATION` 关闭）；AI 的 send_keys 依此判断命令执行结束

## 推荐 tmux 配置

本地 tmux 和远端 tmux 都推荐启用 OSC52 剪贴板。远端 tmux 需要这样配置，复制内容才能通过 eTerm 同步回本地系统剪贴板。

`~/.tmux.conf`：

```tmux
set -g mouse on
set -g mode-keys vi
set -g set-clipboard on
set -as terminal-features ',*:clipboard'
bind -T copy-mode-vi MouseDragEnd1Pane send -X copy-selection-and-cancel
bind -T copy-mode MouseDragEnd1Pane send -X copy-selection-and-cancel
bind -T copy-mode-vi y send -X copy-selection-and-cancel
bind -T copy-mode y send -X copy-selection-and-cancel
set -g extended-keys on
set -g extended-keys-format csi-u
```

设置中的 `tmux config file` 留空时使用 eTerm 管理的内置默认配置；填写路径后，本地和远程 daemon 的 tmux 命令都会使用该配置文件。

重载配置：

```bash
tmux source-file ~/.tmux.conf
```

## Windows 与无 tmux 设备

本地 Shell 在 Windows 上通过 ConPTY 启动（默认 `powershell.exe`，依次探测 `pwsh.exe` / `powershell.exe` / `cmd.exe`，可在设置项 `local terminal shell` 中指定）。

远程设备的 tmux 菜单在检测不到 tmux 的设备上（如 Windows）自动改用 daemon 托管会话：新建、附加、重命名、杀死的操作与 tmux 一致，关闭标签页只是 detach，会话在 daemon 上继续运行。区别是会话随 daemon 进程存活，daemon 退出后会话结束，不像 tmux server 那样独立常驻。

## 数据

默认路径：`~/.config/eterm/eterm.db`（SQLite），可用 `-c path` 指定。

敏感字段（密码、私钥）由主密码派生密钥加密。首次运行完成加密初始化，支持无密码模式。设置页可修改主密码。

偏好项在设置标签页中修改，`C-s` 保存。

tmux 恢复记录保存到 `~/.config/eterm/tmux_restore.json`，仅用于下次启动询问是否恢复本地/远端 tmux 标签页，不参与同步。

## 调试


| 变量                      | 作用                        |
| ----------------------- | ------------------------- |
| `ETERM_DEBUG_KEYS`      | 向 stderr 输出按键事件           |
| `ETERM_DEBUG_APP`       | 向 stderr 输出 SSH/SFTP 连接日志 |
| `ETERM_PPROF_ADDR`      | 开启主进程 pprof，例如 `127.0.0.1:6060` |
| `ETERM_DAEMON_PPROF_ADDR` | 开启 daemon pprof，例如 `127.0.0.1:6061` |
| `ETERMSYNCD_PPROF_ADDR` | 开启 syncd pprof，例如 `127.0.0.1:6062` |
| `ETERM_NO_UPDATE_CHECK` | 禁用 GitHub 版本检查            |

pprof 默认关闭，也可以用显式参数开启：

```bash
eterm -pprof 127.0.0.1:6060
eterm daemon start -pprof 127.0.0.1:6061
etermsyncd -pprof 127.0.0.1:6062 ...
```

卡住时抓现场：

```bash
curl -o goroutine.txt 'http://127.0.0.1:6060/debug/pprof/goroutine?debug=2'
go tool pprof 'http://127.0.0.1:6060/debug/pprof/profile?seconds=30'
curl -o trace.out 'http://127.0.0.1:6060/debug/pprof/trace?seconds=5'
```


## 许可

[MIT](LICENSE)
