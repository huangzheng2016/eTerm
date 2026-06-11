# Termius Import Design

Date: 2026-06-11

## Overview

Add an **Import** entry to the ESC menu. Initially supports Termius as the only source; architecture is extensible. The import flow is a linear overlay stack consistent with existing eTerm overlay patterns.

## Dependencies

- `github.com/huangzheng2016/termius_exporter` — provides `exporter.Export()` returning `([]parser.HostRecord, []parser.KeyRecord, error)`.
- `HostRecord`: `Aliases []string`, `Host`, `Port`, `Username`, `Password`, `KeyName` (already deduplicated by host+port+username).
- `KeyRecord`: `Aliases []string`, `PrivateKey`.

## Overlay Stack

```
escMenu
  └─ importSourceMenu          (source picker: Termius / ...)
       └─ importHostList       (step 1: select hosts)
            └─ importKeyList   (step 2: select keys)
                 └─ aliasPicker          (per-item, on-demand via enter)
                      └─ renameInput     (for conflict or "type new name")
                           └─ confirmDialog  (y/n before executing)
                                └─ importResult
```

Each overlay closes with `esc` returning to the previous layer.

## ESC Menu Changes

Add `escMenuImport` item between Settings and Sync:

```
  Quit        [q]
  Settings    [s]
  Import      [i]
  Sync        [y]
```

Triggers `types.OpenImportSourceMenuMsg{}`.

## Import Source Menu

A simple list overlay (same style as escMenu). Initially one item: **Termius**. Selecting it runs `exporter.Export("")` in a `tea.Cmd` and transitions to the host list on success, or shows an error message on failure.

## Step 1: Host List

Shows all `HostRecord` entries from the export result.

**Row format:**
```
[x] prod-server   root@1.2.3.4:22    (3 aliases)  [key: deploy]
[ ] db-primary    postgres@10.0.0.1:22
[=] staging       root@1.2.3.5:22                  [已存在]
```

- `[x]` selected, `[ ]` unselected, `[=]` exact duplicate — greyed out, not selectable.
- `(N aliases)` shown when `len(Aliases) > 1`.
- `[key: name]` shown when `KeyName != ""`.
- Exact duplicate: `HostRecord` fields match an existing DB row exactly → greyed, blocked.

**Keys:**
- `space` — toggle select (skip greyed rows)
- `enter` — open alias picker for highlighted row (skip if only 1 alias and no conflict)
- `y` — proceed to step 2 with confirmation dialog
- `esc` — back to source menu

## Step 2: Key List

Shows `KeyRecord` entries relevant to selected hosts plus any extras.

**Row format:**
```
[*] deploy-key    (1 alias)   [必须]
[x] staging-key  (2 aliases)
[ ] old-key       (1 alias)
[=] prod-key      (1 alias)   [已存在]
```

- `[*]` — locked selected: key is referenced by a selected host. Cannot be deselected. Shows `[必须]` label.
- `[x]` — voluntarily selected.
- `[ ]` — unselected.
- `[=]` — exact duplicate in DB, greyed.
- Exact duplicate check for keys: same `Fingerprint` already in DB.

**Keys:**
- `space` — toggle (locked rows ignored)
- `enter` — open alias picker for highlighted row
- `y` — open confirmation dialog
- `esc` — back to host list

## Alias Picker

Triggered by `enter` on any selectable row (host or key) that has `len(Aliases) > 1`, or when a name conflict is detected at import time.

Shows the list of aliases plus a final `[ 输入新名... ]` option.

```
选择别名: prod-server
  ▸ prod-server
    production
    prod-a
    [ 输入新名... ]
```

Selecting `[ 输入新名... ]` opens `renameInput` (single-line text input, pre-filled with first alias). `enter` confirms, `esc` cancels back to alias picker.

For single-alias items that have a name collision in the DB (same name, different content), the alias picker is skipped and `renameInput` opens directly with a conflict message.

## Conflict Rules

| Situation | Behaviour |
|-----------|-----------|
| Exact duplicate (same content already in DB) | Row greyed, not importable |
| Same alias name, different content (host) | Selectable; `renameInput` forced before import |
| Same key name, different fingerprint | Selectable; `renameInput` forced; imported host's `KeyName` updated to match new name |
| No conflict | Import as-is using chosen alias |

Conflict check runs once when the export result is loaded (for greying), and again per-item at import time (for rename forcing).

## Confirmation Dialog

Triggered by `y` at the end of step 2.

```
导入确认
  主机: 3 个
  密钥: 1 个（含 1 个必须）

  y 确认  n/esc 取消
```

`y` executes import; result shown as a status-bar toast (same style as existing import result).

## Messages (types package)

```go
OpenImportSourceMenuMsg  struct{}
ImportSourceSelectedMsg  struct{ Source string }   // "termius"
TermiusExportResultMsg   struct {
    Hosts []parser.HostRecord
    Keys  []parser.KeyRecord
    Err   error
}
RunTermiusImportMsg      struct {
    Hosts []importHostEntry   // resolved alias + optional rename
    Keys  []importKeyEntry
}
TermiusImportResultMsg   struct {
    Imported int
    Skipped  int
    Err      error
}
```

## New Files

| File | Purpose |
|------|---------|
| `internal/app/import_source_menu.go` | Source picker overlay |
| `internal/app/import_host_list.go` | Step 1 host list overlay |
| `internal/app/import_key_list.go` | Step 2 key list overlay |
| `internal/app/import_alias_picker.go` | Alias picker + rename input |
| `internal/app/import_termius.go` | `exporter.Export` call + DB write logic |

Existing files touched: `escmenu.go`, `overlay_mouse.go`, `app.go`, `internal/types/messages.go`.

## Out of Scope

- Import from SSH config via this flow (already exists separately).
- Editing host fields other than alias during import.
- Batch rename.
