# 开发者文档

面向从源码构建、排查问题的开发者。功能使用说明见 [用户文档](user.md)。

## 命令行参数

### eterm

```
eterm [flags] [user@]host[:port]
```

不带主机参数时进入 TUI；带主机参数时直连，主机不存在则自动创建（默认用户 `root`、端口 `22`，可被 `-p` 覆盖）。

| 参数                  | 说明                             |
| ------------------- | ------------------------------ |
| `-c path`           | SQLite 数据库路径                   |
| `-p port`           | 与直连主机一起使用时指定端口                 |
| `-v`                | 打印版本并退出                        |
| `--version-json`    | 以 JSON 打印 version / commit 并退出 |
| `--no-update-check` | 禁用解锁后的 GitHub 版本检查             |
| `-pprof addr`       | 开启主进程 pprof HTTP 服务（也可用环境变量 `ETERM_PPROF_ADDR`） |

子命令：

| 命令              | 说明                             |
| ----------------- | ------------------------------ |
| `eterm version`   | 打印版本并退出（同 `-v`）            |
| `eterm upgrade`   | 启动 TUI 并强制检查更新，有新版本时提示升级   |
| `eterm daemon ...` | 管理远程 Shell daemon，见 [用户文档](user.md#多设备同步) |

### eterm daemon

子命令：`start`（默认，后台启动）/ `stop` / `status` / `run`（前台运行，`start` 实际以 `daemon run` 拉起子进程）。

| 参数              | 说明                             |
| ----------------- | ------------------------------ |
| `-c path`         | SQLite 数据库路径                   |
| `-password <pwd>` | 主密码（也可用环境变量 `ETERM_MASTER_PASSWORD`） |
| `-name <名称>`      | 设备显示名（默认主机名）             |
| `-pprof addr`     | 开启 daemon pprof HTTP 服务（也可用环境变量 `ETERM_DAEMON_PPROF_ADDR`） |

### etermsyncd

| 参数            | 说明                             |
| --------------- | ------------------------------ |
| `-listen addr`  | HTTP 监听地址（默认 `:8443`）          |
| `-db path`      | SQLite 数据库路径（默认 `sync.db`）     |
| `-api-key key`  | 认证的 Bearer token（也可用环境变量 `ETERMSYNCD_API_KEY`） |
| `-cert file`    | TLS 证书文件                       |
| `-key file`     | TLS 私钥文件                       |
| `-pprof addr`   | 开启 syncd pprof HTTP 服务（也可用环境变量 `ETERMSYNCD_PPROF_ADDR`） |

## 从源码构建

```bash
go build -o eterm .
./eterm
```

同步服务端（可选）：

```bash
go build -o etermsyncd ./cmd/etermsyncd
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o etermsyncd-linux ./cmd/etermsyncd
```

voicehelper 是独立的 CGO module（main module 保持纯 Go），构建与发布产物说明见 [cmd/voicehelper/README.md](../cmd/voicehelper/README.md)。

## 数据目录

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
