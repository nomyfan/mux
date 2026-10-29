# 迁移到 herdr-core（hc）

本文档记录 Mux 从 herdr 1 的 client endpoint 迁移到 herdr-core（`hc`）的决策过程、调研结论和实现过程中的发现，方便在另一台机器上接着做。

调研时看的上游版本：

- herdr-core `88fe4e7`（2026-10-29），API 版本 0.1.13，protocol generation 1
- herdr-app `b41b5cb`（2026-10-29）
- MyGo v0.6.3（`go.mod` 里锁定的版本）

## 背景

herdr 拆成了两个仓库：

- [herdr-core](https://github.com/herdrdev/herdr-core)（命令 `hc`）：终端运行时。跑程序、保活、把原始字节转发给 viewer，从 OSC 7501 读状态。**不画任何东西，也没有 workspace / tab / 分屏**（它的 AGENTS.md："Core is a runtime, not a multiplexer"）。
- [herdr-app](https://github.com/herdrdev/herdr-app)：基于 GPUI 的桌面应用，是 core 的一个客户端。space、tab、分屏都在 app 这一层。

Mux 原来连的是 herdr 1 的 client endpoint：bincode 编码的二进制协议，服务端排好版、按格子发 Surface 和增量 patch，Mux 只负责画格子。

## 调研结论

### 1. hc 有正式的协议 API，不用再解 bincode

- JSON-RPC 2.0，走 Unix socket，一行一条消息（plain mode）。
- `schema/hc.openrpc.json` 是完整的 OpenRPC + JSON Schema，从 Rust 类型生成；`hc schema` 或 `rpc.discover` 也能从运行中的 core 取到。
- 兼容承诺：`released.openrpc.json` 存上次发布的 API，任何会破坏旧客户端的改动都会让测试失败（"The API only grows"）。但目前标注为 preview（0.x）。
- 错误带固定的 `data.kind` 和 `hint`。
- `hello {framed: true}` 之后切到 framed mode：每帧 `[channel u32 LE][kind u8][len u32 LE][payload]`。channel 0 走 JSON-RPC（多个调用可以并行，不再有 herdr 1 的 `endpoint_busy`），其他 channel 是 attach 的字节流。帧类型：DATA / CREDIT / CLOSE / RESIZE / FOCUS / FACTS。

### 2. 终端内容是原始 PTY 字节，客户端要自己跑 VT

`terminal.attach` 返回一个 channel，core 在上面发：首屏（当前屏幕、可选的历史、最近一次 status 报告和标题、`CSI ?9001h/l`），然后是程序的原始输出（去掉 core 已经回答过的终端查询）。core 不编码按键，客户端自己编码后作为 DATA 发回去。

这和 Mux 原来"不解析程序输出，只画服务端排好的格子"的前提正好相反。

### 3. MyGo 的 terminal 插件正好能接上

之前尝试过用 MyGo 的 `plugins/terminal`（libghostty-vt，运行时加载，不需要 cgo），当时行不通：herdr 1 发的是排好版的格子，不是字节流；插件给"服务端的终端"准备的 `Mirror` 方案又要求服务端也跑这个插件。

hc 发原始字节，正好是插件文档里 "Without a program" 一节的 `Conn` 用法：

```go
term, _ := terminal.New(terminal.Options{Conn: stream})
```

对应关系：

| 插件 `Conn` | hc framed mode |
|---|---|
| `Read` | 该 channel 的 DATA 帧；交给终端之后回 CREDIT（初始额度 256 KiB） |
| `Write`（按键、粘贴、鼠标，已按程序要求的协议编码） | 同一 channel 的 DATA 帧 |
| `Resize(cols, rows)` | RESIZE 帧 |
| `ResizePixels` + `UseTheme` | FACTS 帧：`Display{fg, bg, cursor, cell:[w,h]}`，core 用它回答程序的颜色和像素尺寸查询 |
| `Close` | CLOSE 帧 |

这样 `endpoint` 里的 bincode / wire / surface，以及 `term` 里自己画格子、选择、按键映射、服务端搜索的代码都可以不要了；插件自带选择复制、IME、Kitty 键盘协议、链接、搜索、box drawing。

### 4. workspace / tab / 分屏由 Mux 自己管

core 没有布局。herdr-app 把 space、tab、分屏树存成一份不透明 JSON 文档（名字叫 `herdr-app`），用 `doc.get` / `doc.set` 做带版本号的乐观写入，格式见 herdr-app 的 `src/mirror.rs`。这是 app 的内部格式，没有稳定性承诺。

决定：Mux 用自己的文档（名字 `mux`），格式由 Mux 定义，不和 herdr-app 共享布局。以后想和 Herdr app 共享，再考虑跟它的格式。

### 5. Agent 状态：OSC 7501

Ghostty 作者 Mitchell Hashimoto 在做 Rex 时提出了 OSC 7501（Program Status Protocol）：

- 格式 `ESC ] 7501 ; key=value:key=value ESC \`
- `state`：`idle / working / done / blocked / error`；`blocked` 带 `kind`（`permission / question / auth`）和 base64 的 `msg`
- 可选 `app`（例如 `claude-code`、`codex`）、`progress`、层级 `id`

支持情况（2026-10-29 查证）：

- **Claude Code**：v2.1.295 起支持。启动时先发 `OSC 7501 ; ?` 探测，终端约 2 秒内没回就不上报。`CLAUDE_CODE_DISABLE_TERMINAL_TITLE` 会关掉它。
- **Codex**：openai/codex#52725 已合并，官方 changelog 列在 CLI 0.162.0（2026-10-08）。不探测，stdout 是终端就发；只发 `idle / working / blocked`，带 `app=codex`。

对 Mux 的意义：

- core 自己回答 `OSC 7501 ; ?`，不转给 viewer（herdr-core `tests/m3.rs` 的 `a_new_viewer_sees_the_status_and_7501_questions_are_answered_once`），所以 Mux 不用处理探测。
- core 原样保留 `app` 字段（`status.rs`），agent 图标按 `status.app` 选，不用再靠进程名猜。
- core 不做任何屏幕识别：不上报 7501 的程序就没有状态。

参考：

- [A Terminal Protocol for Program Status (OSC 7501)](https://mitchellh.com/writing/program-status-osc7501)
- [Mitchell Hashimoto: Claude Code v2.1.295 supports OSC 7501](https://x.com/mitchellh/status/2108296550405619967)
- [openai/codex#52725](https://github.com/openai/codex/pull/52725)
- [contember/okena#220（Claude Code 的探测行为）](https://github.com/contember/okena/issues/220)

## 迁移方案

1. `internal/herdr/endpoint` 换成 hc 客户端：JSON-RPC（plain / framed）、分帧、channel 复用、每个 attach channel 一个 `Conn`。
2. `internal/term` 换成基于 `plugins/terminal` 的 pane 区：Mux 自己的分屏树决定每个 pane 的矩形，每个 pane 一个 `terminal.View`，头部、分割线拖动、拖拽重排保留。
3. `cmd/mux`：sidebar 的数据从 `events.subscribe` 的终端列表 + Mux 自己的布局文档来；agent 列表来自有 status 的终端。
4. `internal/herdr/server`：启动 / 停止换成 hc 的方式。

## 待确认的问题

- 多个 viewer 同时看一个终端时，core 让"最后活跃的 viewer"决定尺寸。如果 Herdr app 也连着，应该开 `ResizeByConn`，收到 `size` 事件时调 `term.Resize`。
- 断线重连：Conn 一断，插件认为程序已结束。重连要新建 Terminal 重新 attach，用 `history` 带滚动历史，用 `resumed: true` 避免重新活跃前改尺寸。
- 字体平滑：插件只有 `Thicken`（最粗）和 AppKit 默认两档，Mux 设置里"关"对应的更细的 `Antialiased` 可能没有直接对应。

## 实现记录

（实现过程中值得记下的东西写在这里。）
