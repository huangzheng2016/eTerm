# eTerm

A modern terminal SSH client built with [Bubble Tea](https://github.com/charmbracelet/bubbletea) and [Lip Gloss](https://github.com/charmbracelet/lipgloss).

[中文](README.md)

## Highlights

- **SSH / LocalShell** -- multi-tab terminals, scrollback, disconnect notice and reconnect
- **Host and key management** -- host groups, tags, search, built-in SSH key store
- **Import** -- import hosts from `~/.ssh/config` and Termius
- **SFTP and port forwarding** -- dual-pane file manager, local/remote/dynamic forwarding
- **Sync and remote shell** -- sync config across devices via syncd, open remote shells on devices under the same account
- **AI assistant** -- `C-k` opens a full-screen panel that drives terminal tabs and remote daemons, with background subagents and optional local tools
- **Voice input** -- `C-r` speech to text, local offline sherpa-onnx or Volcano Engine cloud recognition
- **Productivity** -- snippets, session recording, clipboard link paste, command palette, configurable keybindings
- **Secure storage** -- sensitive fields encrypted with a master password; sync data encrypted before upload, server stores ciphertext only

## Demo

| Host list | Host editor |
|:---:|:---:|
| ![Host list](doc/1.png) | ![Host editor](doc/2.png) |

| SFTP file manager | SSH terminal |
|:---:|:---:|
| ![SFTP](doc/3.png) | ![SSH](doc/4.png) |

## Installation

Download the CI-built archives from [Releases](https://github.com/huangzheng2016/eTerm/releases), or use the one-line installer (requires `curl`; on Windows use Git Bash or the PowerShell variant below):

**Linux / macOS / Windows (Git Bash)**

```bash
curl -fsSL https://raw.githubusercontent.com/huangzheng2016/eTerm/master/scripts/install.sh | sh
```

Installs to `~/.local/bin/eterm` by default (`~/bin/eterm.exe` on Windows). Set `INSTALL_DIR` to override.

**Windows (PowerShell)**

```powershell
iwr -useb https://raw.githubusercontent.com/huangzheng2016/eTerm/master/scripts/install.ps1 | iex
```

For building from source see the [developer docs](docs/dev.md) (Chinese).

## Quick start

```bash
# TUI mode
./eterm

# Direct connect (host is created if it does not exist)
./eterm root@192.168.1.1
./eterm deploy@prod.example.com -p 2222
```

## Keyboard shortcuts

Press `?` on any non-SSH tab for the full list (matches the remappable keys in Settings). Common ones:

| Key             | Action                          |
| --------------- | ------------------------------- |
| `Enter`         | SSH connect                     |
| `n` / `e` / `d` | New / edit / delete             |
| `s`             | SFTP                            |
| `c`             | Copy SSH command                |
| `C`             | Clone host                      |
| `h` / `H`       | Toggle hidden visible / hide host |
| `/`             | Search                          |
| `Esc`           | Open menu (quit / settings / sync) |
| `C-S-i`         | Upload clipboard file/image and paste link |
| `C-Tab`         | Next tab                        |
| `C-k`           | AI assistant                    |
| `C-p`           | Command palette                 |
| `C-r`           | Voice input                     |
| `?`             | All shortcuts                   |

Shortcuts are shown in short form: `C` = Ctrl, `S` = Shift, `A` = Alt, e.g. `C-S-i` means `Ctrl+Shift+I`.

Keybindings migrate automatically when upgrading from older versions: after the AI assistant took `C-k`, the command palette moved from `C-k` to `C-p`; if `C-p` was already taken, that feature falls back to its default key.

Windows uses `A-S-<letter>` instead of `C-S-<letter>` by default, because Windows terminal input can lose the Shift state in Ctrl+letter combinations.

## Detailed documentation

The detailed docs are currently available in Chinese only:

- **[AI assistant](docs/user.md#ai-助手)** -- full-screen panel driving terminals and remote daemons, slash commands, background subagents, local tools
- **[Voice input](docs/user.md#语音输入)** -- helper and model download, local/cloud engines, VAD and sentence-end settings
- **[Multi-device sync](docs/user.md#多设备同步)** -- syncd deployment (HTTP / SSH mode), remote shell daemon, temporary shell sharing
- **[Clipboard link paste](docs/user.md#剪贴板链接粘贴)** -- upload clipboard files/images to syncd and paste a short link
- **[Temporary shell sharing](docs/user.md#临时-shell-分享)** -- time-limited share links, browser access via xterm.js
- **[Terminal OSC support](docs/user.md#终端-osc-支持)** -- OSC 8/9/0/2/133 passthrough and shell integration
- **[Recommended tmux config](docs/user.md#推荐-tmux-配置)** -- enable OSC52 clipboard and extended keys
- **[Windows and tmux-less devices](docs/user.md#windows-与无-tmux-设备)** -- ConPTY local shell, daemon-hosted sessions as a tmux substitute
- **[Developer docs](docs/dev.md)** -- full CLI flags, build from source, data directory, debugging (pprof)

## License

[MIT](LICENSE)
