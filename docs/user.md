# 用户文档

eTerm 各功能的详细说明。安装与快速开始见 [README](../README.md)；命令行参数、源码构建与调试见 [开发者文档](dev.md)。

## 导入主机

支持导入 `~/.ssh/config` 与 Termius 主机数据。导入 `~/.ssh/config` 时若存在重名主机，可选择跳过或覆盖。

## 设置

`Esc` 菜单（`s`）或命令面板（Open Settings）打开 Settings 标签页；`Esc` 菜单（`c`）或命令面板（Open Shortcuts）打开 Shortcuts 标签页。语音与同步的设置也是全屏标签页，见各自章节。

各设置页的行类型与修改方式一致：

| 行类型 | 显示 | 修改 |
| ------ | ---- | ---- |
| 开关 | `on` / `off` | 空格或回车切换 |
| 选项 | 当前值（同步页为 `< 值 >`） | `←→` 循环切换（语音页还可用空格/回车同向切换） |
| 数值 / 文本 / 路径 | 当前值或占位提示 | 回车进入编辑态，`enter` 确认 / `esc` 取消 |
| 密钥 | `(set)` / `(not set)`，不回显原值 | 回车进入编辑态，密文输入；留空确认在同步/语音页表示清除，编辑 AI provider 时表示保持不变 |
| 绑定（Shortcuts 页） | 键名逗号分隔，未绑定显示 `(none)` | `enter` 重绑 / `+` 追加 / `backspace` 清空 |
| 动作 | 标签加 `enter` 提示 | 回车执行（如 Change master password、Microphone test） |

改动先暂存，底部显示 `* unsaved changes`，`C-s` 保存后才生效。`C-r` 的语义各页不同：语音设置放弃暂存改动、恢复已保存的值；Settings 与 Shortcuts 弹出确认框（`y` 确认 / `n` 取消），确认后恢复出厂默认值（仍是暂存，需 `C-s` 保存）。`↑↓`/`k`/`j` 移动光标（同步页用 `Tab`/`S-Tab`），鼠标点击可选中行、滚轮滚动（同步页不支持鼠标），`Esc` 关闭标签页。

### Settings 标签页

通用偏好，按组分节：

- Recording：Save session transcripts（保存会话文字记录，默认开）、Record session replay（录制会话回放，默认开）
- Interface：Grid status text（主机网格中显示状态文字，默认关）
- Terminal：Local terminal shell（本地 Shell 程序，留空自动探测）、tmux config file（tmux 配置文件，留空使用内置配置）
- Sharing：Share link max hours（分享链接有效期的默认值，1-168 小时，默认 4）
- Security：Change master password（动作行，回车打开修改主密码浮层）

`C-r` 确认后所有偏好恢复出厂默认值。

### Shortcuts 标签页

按键绑定按场景分组：Global、Home、SFTP、Keys、Forward、Snippet、SSH，组标题带编号。

- `↑↓`/`k`/`j` 移动；`[` `]` 跳到上一组/下一组；`1`-`7` 直接跳到对应组
- `enter` 重绑（替换该动作的全部现有绑定），`+` 追加一个绑定，`backspace`/`delete` 清空该行绑定；捕获态下按下要绑定的键即录入，`esc` 取消
- `C-s` 保存，保存后立即在全应用生效；`C-r` 确认后恢复出厂绑定

## AI 助手

`C-k` 打开全屏 AI 助手面板，再按一次（或 `Esc`）收起；收起后当前 run 在后台继续，状态栏显示 `ai running`。会话内容只在 `/new` 时清空。

Provider：首次启动自动导入 `~/.kimi-code/config.toml` 中 api_key 类型的 provider（OAuth 类型跳过），也可在面板中手动添加。`/model`（或面板内 `C-p`）打开 provider/模型列表：`enter` 切换当前模型，`a` 添加，`e` 编辑，`d` 删除（`y` 确认 / `n` 取消），`esc` 返回。列表中 `[active]` 标记当前项，`(set)` 表示已存 api_key，`[kimi]` 标记从 kimi-code 配置导入的 provider（只读，不可编辑/删除）。添加/编辑表单的字段为 name / type / base_url / api_key / model，`Tab`/`S-Tab` 在字段间移动，api_key 密文输入；编辑时 api_key 留空表示保持不变；保存出错时表单保持打开，可修正后重新提交。会话保存在 SQLite `ai_sessions` 表，`/resume` 恢复。

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
- 本机历史：`shell_history` 读本机 shell 命令历史，依次探测 zsh `~/.zsh_history`、bash `~/.bash_history`、fish `~/.local/share/fish/fish_history`，最新命令在前，默认 50 条、上限 500，只读
- 远程 daemon（仅在已注册 daemon 时挂载）：`list_daemons` / `list_daemon_sessions` / `enter_daemon` / `create_session` / `rename_session` / `kill_session`
- 打开会话：`open_local_terminal` / `open_ssh`（按 `list_hosts` 的主机名，重名报歧义）/ `open_tmux`（按 `list_tmux_sessions` 的会话名）
- 其他：`sleep`（最长 10 分钟）；`spawn_agent` / `wait_agent` / `list_agents` 后台子代理（最多 4 个并发）；`notify` 桌面通知（OSC 9）

行为准则：回答问题和排查时 AI 优先用只读工具（`read_tab` 分页翻历史、`shell_history`、`list_tabs` 等 `list_*` 发现工具），不会为了查看而打开标签页或发键；只有用户要求执行/修改，或信息确实读不到时，才用 `open_*` 与 `send_keys`。daemon 工具中 `list_daemons` / `list_daemon_sessions` 为只读发现，`enter_daemon` 会附着交互会话，仅在需要操作时使用。

本地工具：`bash` 与 `str_replace_editor`（读/写/改/undo）。无沙箱，以当前用户完整权限执行。

## 语音输入

`C-r` 切换录音（终端无法感知按键抬起，因此是开关而非按住说话）。识别文本送入当前终端（等同粘贴）或 AI 面板输入框；句尾动作为 enter 时识别完一句直接提交。

helper 或模型未就绪时按 `C-r` 会打开语音设置标签页并提示缺什么。设置标签页也可从命令面板（Voice Settings）或 `Esc` 菜单（`v`）进入。它与 Settings 同为全屏标签页，行类型与修改方式见「设置」一章；所有改动先暂存（底部显示 `* unsaved changes`），`C-s` 保存后才生效，`C-r` 放弃暂存改动、恢复已保存的值。文本类参数（API key、自定义模型路径）回车后在该行内编辑（enter 确认 / esc 取消，密钥密文回显，显示为 `(set)`/`(not set)`）。底部状态行显示当前配置是否就绪。

按用途分节，超出屏幕可滚动：

- Engine：引擎单选列表，`[active]` 标记当前引擎，enter/空格切换。顺序为 local (sherpa-onnx) 离线引擎、Volcano Engine 火山云端，之后是其他云端引擎（AssemblyAI / Deepgram / Whisper-compatible API）
- <引擎名> 设置（仅当前引擎带参数时显示）：火山引擎为 Volcano API key（密钥行，加密存储）与 Volcano model（`←→` 或回车在四个 resource id 间循环：volc.seedasr.sauc.duration（默认）/ volc.seedasr.sauc.concurrent / volc.bigasr.sauc.duration / volc.bigasr.sauc.concurrent）。旧版 app_key/access_key 双 key 配置已废弃，迁移时自动丢弃，需在设置里填 API key。其他云端引擎各带自己的参数（Deepgram：API key、Model（默认 nova-2-general）、Language（默认 zh）；AssemblyAI：API key；Whisper-compatible API：Base URL、API key、Model（默认 whisper-1））
- Model（仅本地引擎）：模型列表，enter 下载或设为当前（`[active]` 标记）：SenseVoice 2024-07-17（约 1 GB，默认；同包含 fp32/int8 两套权重）与 Paraformer zh-small int8（约 74 MB）；Custom model path 自定义模型目录，需同时有 tokens.txt 与 model.onnx 或 model.int8.onnx；两套权重齐全时显示 Precision 开关（fp32 / int8）
- Voice Helper（仅本地引擎）：helper 一键下载/更新/检查更新（CI 构建的 voicehelper，release 产物 `voicehelper-<os>-<arch>.tar.gz`，darwin-amd64 / darwin-arm64 / linux-amd64 / linux-arm64，约 45 MB，含 sherpa-onnx 动态库）
- Input：speech sensitivity (0-1)（VAD 触发灵敏度，步进 0.05，0 表示引擎默认值）、end-of-sentence silence（句尾静音判停时长 ms，步进 50，范围 50-5000，默认 1000；同时作为火山引擎的 end_window_size 传入，clamp 300-5000）、Sentence end（句尾动作 enter / space，默认 space）
- Microphone test：回车开始/停止录音测试，用当前配置识别一句话，显示识别到的文本，用于验证配置可用
- Volcano features（仅火山引擎显示）：
  - Context awareness：上下文感知开关（默认关，仅火山引擎生效）。开启后开始录音会把上下文作为 `corpus.context`（dialog_ctx，800 token 上限）传给火山引擎提高识别率：当前是 AI 面板时取最近 20 轮对话（user/assistant 交替）；当前是终端标签页时取屏幕尾部最近 20 行，先清洗（去表格线/边框等结构符号、压缩空白、丢弃无字母数字或汉字的行、相邻重复行去重）。上下文在每一句话（utterance）边界自动刷新，计算失败时复用上一轮成功的上下文
  - Semantic smoothing (DDC)：语义顺滑开关（默认开，仅火山引擎生效），对应火山 enable_ddc

注：火山引擎的 vad_segment_duration 不传。官方说明当 end_window_size 已配置时 vad_segment_duration 不生效，保持缺省即可。

## 多设备同步

`Esc` 菜单（`y`）或命令面板（Open Sync）打开同步设置。默认使用 HTTP syncd。

设置页按 Connection / Encryption / Behavior 分区：

- Connection：Enabled（同步开关）、Mode（HTTP / SSH）、API Key（与 syncd 的 `-api-key` 一致）；HTTP 模式另有 Server URL 与 Insecure TLS（跳过证书校验，自签证书用），SSH 模式另有 SSH Host（从已保存的主机中选择）与 Remote Port（默认 18443）
- Encryption：Passphrase（同步数据加密口令；同步数据在上传前加密，syncd 不保存明文）
- Behavior：Interval (sec)（自动同步间隔，默认 300）

交互与其他设置页略有不同：`Tab`/`S-Tab`（或 `↑↓`）在字段间移动，`←→` 循环选项，文本字段获得焦点后直接输入。API Key 与 Passphrase 是密钥字段：平时只显示 `(set)`/`(not set)`，回车进入编辑态后密文输入（enter 确认 / esc 取消），留空确认并保存表示清除。两个密钥用主密码加密存储，修改需要主密码已解锁。`C-s` 保存（开启同步时校验：HTTP 模式必填 Server URL，SSH 模式必选 SSH Host，Passphrase 必填，Remote Port 须为 1-65535），`F5` 测试连接，`C-y` 保存并立即同步，`Esc` 关闭。

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
eterm daemon rename 工作站   # 在线改显示名，运行中的 daemon 几秒内生效，重连后保持
```

daemon 子命令的可选参数：`-c path`（数据库路径）、`-password <主密码>`（也可用环境变量 `ETERM_MASTER_PASSWORD`）、`-name <显示名>`（默认主机名）、`-pprof <地址>`。

也可以在 TUI 里改显示名：主机列表选中在线设备，Enter 打开远程菜单后按 `n` 输入新名字。与 `daemon rename` 等效，立即生效并持久化，已打开的标签页标题会同步更新。

daemon 与 syncd 的 relay 协议版本不匹配时，daemon 会报错并以退出码 1 退出，不再自动重连。升级 syncd 或 eterm 后，需要用与 syncd 匹配的新版 eterm 重新 `eterm daemon start`。

也可以选 SSH 模式：在某台可通过 SSH 登录的主机上常驻 etermsyncd（同上，监听 `127.0.0.1:18443`），同步设置里选 SSH 并填该主机和 Remote Port（默认 18443），API Key 与远端一致。客户端会用 SSH 本地端口映射访问远端的 HTTP API，records 同步、远程 Shell 和剪贴板托管与 HTTP 模式完全一致。注意 SSH 主机需要先交互连接一次以信任指纹。

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
- Settings 标签页的 Share link max hours（1-168，默认 4）仅作为弹窗中有效期的默认值，可按需修改

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

Settings 标签页的 `tmux config file` 留空时使用 eTerm 管理的内置默认配置；填写路径后，本地和远程 daemon 的 tmux 命令都会使用该配置文件。

重载配置：

```bash
tmux source-file ~/.tmux.conf
```

## Windows 与无 tmux 设备

本地 Shell 在 Windows 上通过 ConPTY 启动（默认 `powershell.exe`，依次探测 `pwsh.exe` / `powershell.exe` / `cmd.exe`，可在 Settings 标签页的 Local terminal shell 中指定）。

远程设备的 tmux 菜单在检测不到 tmux 的设备上（如 Windows）自动改用 daemon 托管会话：新建、附加、重命名、杀死的操作与 tmux 一致，关闭标签页只是 detach，会话在 daemon 上继续运行。区别是会话随 daemon 进程存活，daemon 退出后会话结束，不像 tmux server 那样独立常驻。
