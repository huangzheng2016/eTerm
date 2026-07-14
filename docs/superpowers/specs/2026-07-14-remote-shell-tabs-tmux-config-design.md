# Remote Shell, Tab Bar, and tmux Configuration Design

## Scope

Fix normal RemoteShell exit handling, make tab bar navigation use one persistent scroll state, and add a configurable tmux configuration file with an eTerm-managed default.

## RemoteShell Exit

Linux and macOS PTY masters can return `EIO` after the child process exits. The daemon currently forwards that read error in `FrameClose`, so a normal `exit` appears as an input/output error.

When PTY output ends, the daemon will prefer the child process result. If the child exited successfully, `EIO` from the PTY read is treated like EOF and the daemon sends an empty `FrameClose` payload. Non-zero process exits and unrelated read failures remain visible.

Tests will cover `EIO` paired with a successful child exit and preserve existing error propagation coverage.

## Tab Bar Navigation

`App.tabBar` will be the source of truth for tab items, active index, width, and scroll index. Rendering will use that model instead of constructing a new `TabsModel` on every view call.

Mouse wheel events and arrow clicks will update the same model that is rendered. Clicking a visible tab will activate it. Tab switching through configured next and previous shortcuts will update the active index and keep the selected tab visible.

The tab bar will also have configurable page-left and page-right actions. Their defaults are `alt+shift+left` and `alt+shift+right`. These actions scroll the visible tab range without changing the active tab. They will appear in Settings beside the existing tab navigation bindings.

Tests will verify rendered scroll changes, mouse wheel paging, arrow hit regions, tab hit regions, keyboard paging, and shortcut-driven active-tab visibility.

## tmux Configuration

Settings will add a `tmux config file` path. The value is stored as an application setting and is not synced through keybinding configuration.

When the setting is empty, eTerm will create or refresh an internal tmux configuration file under the eTerm configuration directory and invoke tmux with `-f` pointing to it. The internal file contains the recommended configuration without comments:

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

When the setting is non-empty, eTerm expands a leading home-directory marker and passes that file to tmux with `-f`. All local tmux commands and daemon-side remote tmux commands use the resolved path. Ordinary local and remote shells are unchanged.

The README recommendation will show the current local `~/.tmux.conf` contents as the recommended configuration, with no comments inside the configuration block. It will also document the Settings override and the built-in default behavior.

Tests will cover default file creation, custom path selection, tmux command arguments, and Settings load/save/reset behavior.

## Compatibility and Failure Behavior

The default does not overwrite `~/.tmux.conf`. Existing users with no setting receive the managed configuration automatically. A configured path is passed directly to tmux, so missing or invalid files produce tmux's existing command error instead of silent fallback.

No database migration is required because application settings are key-value records.
