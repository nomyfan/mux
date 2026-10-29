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

## 迁移方案（已实现，分支 `hc-migration`）

1. `internal/herdr/endpoint`、`internal/herdr/server` 删除，换成 `internal/hc`：`hc connect` 子进程上的 JSON-RPC（framed mode）、分帧、channel 复用、attach 流、事件流。
2. 新增 `internal/layout`：Mux 自己的 workspace / tab / 分屏树，存在 core 的 `mux` 文档里。
3. `internal/term` 换成基于 `plugins/terminal` 的 pane 区：分屏树决定每个 pane 的矩形，每个 pane 一个 `terminal.View`，头部、分割线拖动、拖拽重排、Cmd+方向键、搜索保留。
4. `cmd/mux`：sidebar 的数据来自事件流的终端列表 + 布局文档；agent 列表是上报了 OSC 7501 状态的终端。

各模块的现状以 `AGENTS.md` 为准。

## 待确认问题的结论

- **多 viewer 尺寸**：没做 `ResizeByConn`。Mux 是唯一 viewer 时没问题；和 Herdr app 同时看一个终端时，尺寸以最后活跃的那个为准，另一边会看到错位。见下面"没做的"。
- **断线重连**：做了。连接断开时所有 pane 的插件终端显示 `[Disconnected]`，重连后 `View.Reset()` 全部重建，重新 attach 时带 `history: 5000`。`resumed` 没用上：重建的终端第一次 attach 就用当前视图尺寸。
- **字体平滑**：设置项改名为 Thicken Text，对应插件的 `Font.Thicken`（Ghostty 的 font-thicken），默认关。`settings.json` 里沿用 `font_smoothing` 这个键，开 = 更粗，方向和原来一致。原来"关"时更细的 `Antialiased` 画法插件没有，现在"关"是 AppKit 默认画法。

## 实现记录

### 布局文档格式

core 的 `mux` 文档（`doc.get` / `doc.set`，带版本号）：

```json
{
  "workspaces": [
    {"id": "w1a2b3c4", "name": "", "tabs": [
      {"id": "t5d6e7f8", "name": "", "focus": "t3", "zoomed": false,
       "root": {"vertical": false, "ratio": 0.5,
                "a": {"terminal": "t1"},
                "b": {"terminal": "t3", "name": "monitor"}}}
    ]}
  ],
  "active": "t5d6e7f8"
}
```

- 叶子是 pane（`terminal` 是 core 的终端地址），内部节点是分屏（`vertical` 表示上下，`ratio` 是 a 占的比例）。
- 没名字的 workspace 用第一个 pane 当前所在目录的目录名，没名字的 tab 用当前焦点 pane 的名字（agent 名、前台进程名或目录）。
- 读回来时 `Repair()` 修掉缺一边的节点、空 tab、越界的 ratio；`Prune()` 去掉终端已不在的 pane。
- 保存一次只发一个 `doc.set`；版本冲突时采用对方的版本（另一个 Mux 写的）。收到别人写的 `doc` 事件会重新读。手工改这个文档（比如用 `hc connect` 发 `doc.set`），正在运行的 Mux 会立刻跟着变，测试时验证过。

### 和 core 打交道时发现的事

- **任何 `hc` 命令都会自动启动 core**，`hc connect` 也一样。所以 Mux 不再需要原来的"Start Server"流程：连上就有 core。用户在设置里 Stop Server 之后，Mux 必须停止自动重连（`gate`），否则下一秒又把 core 拉起来了。现在停止后显示"herdr-core is stopped"和 Start Server 按钮。
- **连接方式**：和 herdr-app 一样跑 `hc connect` 子进程，不直接连 socket。好处是不用复刻 socket 路径规则（`HC_SOCKET`、`$XDG_RUNTIME_DIR/herdr-core/<session>.sock`、`/run/user/<uid>/...`、state 目录），也自带自动启动。
- **找 hc**：PATH 里的 `herdr-core` / `hc`，再找 `~/.local/bin`、`/opt/homebrew/bin`、`/usr/local/bin`、`~/.cargo/bin`；`--version` 输出以 `hc ` 开头才算（有的窗口管理器自带一个无关的 `hc`，herdr-app 的 `core_bin.rs` 也这么判断）。
- **Finder 启动的环境**：core 由 Mux 的 `hc connect` 拉起，会继承 Mux 的环境，再传给它跑的程序。从 Finder 打开的 app 没有用户的 PATH 等，所以父进程是 1 时，Mux 先用登录 shell 取一遍环境（`$SHELL -l -c 'env -0'`，用标记隔开 profile 的输出）再启动 `hc connect`。**这一点只在 Linux 上用单元测试覆盖了逻辑，没在 macOS 上实际验证。** 另外：如果 core 已经被别人（比如终端里的 `hc` 命令）启动了，用的就是那个 core 的环境，Mux 管不了。
- **socket 路径长度**：Linux 上限 107 字节，macOS 104。core 会直接报错 "the socket path ... is too long"。测试里的 `XDG_RUNTIME_DIR` 必须放在 `/tmp` 下，不能用 `t.TempDir()`（macOS 的 `$TMPDIR` 很长）。
- **没有 viewer 时终端是 200×120**：`terminal.run` 返回的尺寸是 200×120；最后一个 viewer detach 后也回到 200×120（有 `size` 事件）。
- **终端身份**：core 默认告诉程序它跑在 Ghostty 里（记录里 `identity: "ghostty"`，XTVERSION 也这么答）。Mux 的模拟器正是 libghostty-vt，所以没传 `TERM_PROGRAM`，保持这个身份。herdr-app 传的是 `TERM_PROGRAM=herdr`。
- **LANG**：容器里 core 的环境没有 `LANG`，bash 在 C locale 下把 UTF-8 的多字节字符当成多个字符，回显出现 `��`。macOS 从 Finder 打开时也没有 `LANG`（Terminal.app、Ghostty 都是自己设）。现在 `terminal.run` 时如果 Mux 的环境里没有 `LANG` / `LC_ALL` / `LC_CTYPE`，就传 `LANG=en_US.UTF-8`，和 MyGo 插件自己启动程序时的做法一致。
- **keep 的终端**：Mux 用 `keep: true` 启动终端。core 停止再启动、或者机器重启后，这些终端过几秒得到新的 shell（在最后的目录，不会重跑原命令），期间 attach 返回 `unavailable`，pane 显示 `[Disconnected]`；收到 `restarted` 事件后 Mux 重新 attach。实测停止再启动 core 后三个 pane 都回来了，pane 名字保留。
- **OSC 7501 探测由 core 回答**：Claude Code 启动时的 `OSC 7501 ; ?` 由 core 回答，不会转给 viewer（herdr-core `tests/m3.rs`）。`app` 字段原样保留，Mux 按它选图标和名字（`claude-code` → Claude Code，`codex` → Codex）。用 `printf '\e]7501;state=blocked:kind=permission:app=claude-code\e\\'` 可以手工模拟一个 agent，实测 sidebar、tab 状态点、pane 头部都正确。
- **事件流的 credit**：events.subscribe 的 channel 也有流控，和 attach 流一样。Mux 每读完一帧就回 CREDIT（`Stream.Read`），订阅时额外给 1 MiB，和 herdr-app 的做法一致（它的 `hc.rs` 订阅时给 1 MiB，`window.rs` 每读一批补一次）。

### 和 MyGo 打交道时发现的事

- **插件的 `Conn` 尺寸回调在插件自己的 goroutine 上调用**，并且会等调用返回。所以 Mux 的 `conn` 在第一次 `ResizePixels` 时同步调用 `terminal.attach`，首屏就是视图的实际尺寸；attach 之前 `Read` 阻塞、`Write` 先缓存。插件先调 `UseTheme` 再调 `ResizePixels`，所以 attach 时 `display` 里已经有颜色和格子像素尺寸。
- **`Conn` 结束时插件写 `[Disconnected]`**（不是 `[Process exited]`）。
- **`Handle.Bounds(c)` 是上一帧提交的几何**，元素尺寸变化不会自动触发重建。pane 区按自己的尺寸排版子元素，第一次出现时宽度是 0、什么都不画，而之后如果没有别的事件就不会有下一帧，结果是 pane 区一直空白（重启 app 后从 core 读到布局时就是这样，旧代码靠服务端不停推 surface 掩盖了这个问题）。现在尺寸变化或第一次出现时 `c.Invalidate()` 再要一帧（`TestLaysOutUnasked`）。
- **指针事件只送给最内层有 `HandleInput` 的元素**：按在终端里的归插件，按在 pane 头部的归 pane 区。拖动 pane 标题、点头部切焦点都依赖这一点。
- **焦点**：`terminal.View` 返回的元素可以 `.Bind(&handle)`，用 `handle.Focused(c)` 读实际焦点、`handle.Focus(c)` 请求焦点。焦点在第一次请求后的下一帧才生效，所以端到端测试里要等 `tt.Focused("Terminal")` 再输入，否则第一帧的按键会丢。
- **Cmd 键**：插件不把 Cmd 组合键发给程序，pane 区的 `OnShortcut`（Cmd+F、Cmd+方向键）先于插件拿到按键。Linux 上 `ui.Cmd` 对应 Ctrl（在 Xvfb 里用 xdotool 发 Ctrl+Shift+D、Ctrl+方向键验证过）。

### 怎么验证的

- `CGO_ENABLED=0 go test ./...`：hc（假 core 走管道）、layout、term（假 attach 流 + `ui.NewTester`）、app 的 UI 测试。
- `MUX_HC_TEST=1 go test ./...`：另外跑两个连真实 herdr-core 的测试，在临时的 session / XDG 目录里，结束时 `server.stop`：
  - `internal/hc` 的 `TestCore`：run、attach、输入输出、resize 事件、kill、文档版本冲突。
  - `cmd/mux` 的 `TestWithCore`：app 的真实连接循环 + Tester。空 core 自动建 workspace、shell 提示符出现、输入、Cmd+D 分屏、第二个 pane 的输入不串到第一个、布局写进 core、Cmd+Shift+W 关 pane 后终端结束、`exit` 后 pane / tab / workspace 一起消失。
- 加 `-race` 全部通过。
- `go tool mygo build` 在 Linux 上打包成功，产物（tar.gz / deb）里带着 `libghostty-vt.so`。
- 在 Linux 的 Xvfb 里跑了真实 app，用 xdotool 发键盘输入、截图看：颜色、粗体、下划线、中文、emoji、`ls --color`、`top` 和 vim 全屏、Ctrl+Shift+D 分屏、Ctrl+Shift+Enter 放大、Ctrl+→ 切焦点、Ctrl+, 设置、Stop Server / Start Server、重启 app 后布局和滚动历史恢复、外部改布局文档后 app 跟着变。

手工跑的方法（隔离的 core，不碰自己的）：

```sh
R=$(mktemp -d /tmp/muxXXXX)   # socket 路径要短
export HC_SESSION=muxdev XDG_RUNTIME_DIR=$R XDG_STATE_HOME=$R/state XDG_CONFIG_HOME=$R/config
go tool mygo dev               # 或 go run ./cmd/mux
hc ls; hc send t1 'echo hi'   # 同一组环境变量下可以从外面操作这个 core
hc server stop                 # 用完停掉
```

### 没做的、需要在 Mac 上确认的

- **没在 macOS 上跑过**，只在 Linux（Xvfb）上验证了。需要在 Mac 上确认：`go tool mygo build` 打的 .app 里有 libghostty-vt（`mygo.config.ts` 已设 `minimumSystemVersion: "13.0"`）；红绿灯位置；Cmd 快捷键和 File 菜单；从 Finder 打开时取登录 shell 环境；Thicken Text 的效果；中文输入法在终端里的合成位置。
- **和 Herdr app 共用布局**：没做。Mux 只用自己的 `mux` 文档；不在它布局里的终端（比如 Herdr app 开的）只在上报状态时出现在 Agents 列表里，点它会给它开一个 tab。herdr-app 的格式在它的 `src/mirror.rs`，没有稳定性承诺。
- **多 viewer 尺寸**（`ResizeByConn` + `size` 事件）：没做。
- **其他机器的终端**（`machine/t7`）：事件流里会有，Mux 没特别处理；`cwdOf` / Git 分支只看本机路径。
- **agent 的 done → idle**：herdr-app 在用户看过之后把 `done` 显示成 `idle`，Mux 原样显示程序上报的状态。
- **隐藏 tab 的终端一直保持 attach**：切回来是瞬间的，但每个 pane 都占一条流和一份 scrollback。pane 多了以后可以考虑 detach 不可见的。
- **拖动分割线时程序每帧都收到 resize**：原来只在跨过一个格子时请求服务端。插件会按格子数变化才调 `ResizePixels`，问题不大，但拖得快时 shell 会重绘多次。
- **设置里的字体预览**还是用 `ui.Font` 自己画，不是插件的终端；可以换成一个只 `Feed` 预览文本的插件终端，做到真正"和终端画法一致"。
- **herdr 1 的数据不迁移**：原来 herdr 1 里的 workspace 不会带过来，Mux 第一次连 hc 时会新建一个 workspace。
- **core 版本检查**只看 protocol generation 和用到的方法是否都有，没有最低版本号。

### 其他

- 迁移前 `main` 上 `internal/term` 的 `TestPaneMoves` 就是失败的（"made 3 requests, want 2"），和这次改动无关，那段代码已经随旧实现删除。
- 调试时踩过的坑：在 Bash 里用 `pkill -f mux-bin` 会把执行这条命令的 shell 自己也杀掉（命令行里含这个字符串），要用 `pgrep -f "^<完整路径>$"`。
