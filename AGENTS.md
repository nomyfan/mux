# AGENTS.md

Mux is a native GUI client for [herdr-core](https://github.com/herdrdev/herdr-core) (`hc`), a terminal runtime for coding agents, written in Go with [MyGo](https://github.com/egoist/mygo)'s native `ui` package. It talks to the core through `hc connect` and shows its terminals in a window: a sidebar of workspaces, tabs and agent states on the left, and the chosen tab's panes on the right. The core runs the programs and relays their raw output; each pane feeds it to a terminal emulator of MyGo's terminal plugin (libghostty-vt). Workspaces, tabs and splits are Mux's own: the core has none, and keeps Mux's layout as a document.

## Commands

```sh
go tool mygo dev                          # run a development build with live reload
go tool mygo build                        # package the app (.app and .dmg on macOS, into build/)
go tool mygo vet .                        # go vet plus checks for stored UI build values
CGO_ENABLED=0 go test ./...               # all tests
MUX_HC_TEST=1 CGO_ENABLED=0 go test ./... # also the tests against a real herdr-core, in a session of their own
go test ./internal/layout -run TestBoxes  # a single test
scripts/make-icon.sh                      # render resources/icon.png and cmd/mux/icons/app.png from the artwork in design/ (needs ImageMagick)
scripts/make-screenshot.sh light.png dark.png  # split two window screenshots along the diagonal into docs/screenshot.png (needs ImageMagick)
scripts/make-appearance-previews.sh       # write the settings' appearance pictures, cmd/mux/icons/appearance-{light,dark,auto}.svg
```

`mygo` is pinned through the `tool` directive in `go.mod`; nothing to install separately. App settings (name, identifier, entry `./cmd/mux`, macOS 13 or later for libghostty-vt) live in `mygo.config.ts`, which MyGo evaluates with Bun or Node.js 22.6+; it sets `CFBundleVersion` to the short commit hash, shown in parentheses in the about panel. Running the app needs herdr-core installed (`curl -fsSL https://releases.herdr.dev/core/install.sh | sh`). The terminal plugin loads libghostty-vt at run time, without cgo: `mygo build` puts it in the app, and `go run` / `go test` download it once into the user's cache.

To run the app by hand without touching the user's own core, give it one of its own (see Notes):

```sh
R=$(mktemp -d /tmp/muxXXXX)   # under /tmp: socket paths are short
export HC_SESSION=muxdev XDG_RUNTIME_DIR=$R XDG_STATE_HOME=$R/state XDG_CONFIG_HOME=$R/config
go tool mygo dev               # or: open -n --env HC_SESSION=$HC_SESSION ... build/darwin-arm64/Mux.app
hc ls; hc send t1 'echo hi'   # with the same variables, hc reaches that core
hc server stop                 # stop it when done
```

In a pane, `printf '\e]7501;state=blocked:kind=permission:app=claude-code\e\\'` plays an agent asking for permission, as Claude Code does.

## Modules

Dependencies run `main` → `term` → `hc` and `layout`; `term` and `main` use MyGo's terminal plugin; `hc` and `layout` have no UI dependency.

### `cmd/mux/` (package `main`)

The app itself: top-level UI and app state.

- `main.go`:
  - Creates the window. On macOS the title bar is hidden and the traffic lights sit over the sidebar, centered on the pane headers.
  - The sidebar groups tabs by workspace and has a list of agents with breathing status dots. Workspaces, tabs and panes can be renamed in place, collapsed and created; closing asks for confirmation. A workspace without a name is called after the folder of its first pane, a tab without one after its focused pane. Cmd+T makes a tab in the shown workspace, Cmd+N a workspace, Cmd+W closes the shown tab (confirmed) and Cmd+Shift+W the focused pane; on macOS these four sit in the app's own File menu, as the default one's Close Window would take Cmd+W. Cmd+Shift+[ / ] go around the shown workspace's tabs, Cmd+1–8 to one of them and Cmd+9 to its last; Cmd+D / Cmd+Shift+D split the focused pane right / down; Cmd+Shift+Enter zooms it. New tabs, workspaces and split panes start where the focused pane's program is (its foreground process's folder, as the core reports it), or in the user's home without one, as the first one in an empty core (the core's own choice would be the folder it started in, `/` from the Finder).
  - Pane headers carry the split, zoom and close buttons, and show the pane's name, its agent's state, its folder and its Git branch (read from `.git` off the main thread).
  - Choosing an agent shows its pane; an agent without one, as one another agent started, gets a tab of its own.
- `core.go`: the app's side of the core. A goroutine keeps the connection (`hc connect`, which starts the core if none runs) and reconnects every second after a drop, unless the user stopped the core. It subscribes to the event stream before reading the layout document, so no change is missed, and the events reach the main thread through `post` (the window's `Update`, which tests replace), a batch at a time. The layout is saved to the core's `mux` document one write at a time, by version; a conflicting write adopts the other writer's layout, and another app's write is read again. Panes whose terminals finish or go are pruned, a tab with no pane closes, a workspace with no tab too; an empty core gets a workspace. Terminals are run with `keep`, so they get fresh shells after a core or machine restart, with the label `mux=1`, no parent, `TERM=xterm-256color`, `COLORTERM=truecolor`, and `LANG=en_US.UTF-8` when the app's environment has no locale (as one opened from the Finder). Opened from the Finder (parent pid 1), the app takes the login shell's environment for `hc connect`, so the core it starts gives its programs the user's.
- `model.go`: what things are called, from the layout and the terminals' records: agents (terminals whose programs report an OSC 7501 status) and their names and marks by the `app` they report, pane, tab and workspace names, the most urgent state of a tab's agents, and Git branches.
- `settings.go`: the settings dialog (Cmd+,): pages listed on the left (Server, Appearance, Terminal), the chosen one on the right, scrolling under its title. Server shows the core's status and the `hc` the app runs, and stops it (confirmed) or starts it again. Appearance is auto, light or dark, chosen among pictures of the window in each (`icons/appearance-*.svg`) and applied through `mygo.Theme.SetSource`. Terminal shows a preview of the terminals' text above their fonts: a family chosen among the system's monospaced ones (default: the system's own, `monospace`) and an optional fallback family, among all of them, for the glyphs the first lacks such as Nerd Font icons (default: none), their size (8–32, default 14) (`ui.FontFamilies`, listed in the background at startup), and, on macOS, Thicken Text (the plugin's `Font.Thicken`, as Ghostty's font-thicken; default off). They apply at once through `term.View.SetFont`. Settings are saved as `settings.json` in the app's user data directory and applied before the window opens.
- `server.go`: the core's state in the UI. Without herdr-core installed, the pane area shows the command installing it; stopped by the user, it offers Start Server. The core is shown with the in-app icon `cmd/mux/icons/app.png`.
- `theme.go`: the Vellum colors in light and dark. Controls take its Zed colors; terminals take its Ghostty colors, with a true blue and cyan, their own bright colors and grays that show on either ground, and the cursor in the controls' accent.

### `internal/hc`

A client of herdr-core's API (protocol generation 1; the core's `schema/README.md` and `hc schema` describe it). No UI dependency.

- `conn.go`: runs `hc connect`, says hello and switches to framed mode: frames of `[channel u32][kind u8][len u32][payload]`, JSON-RPC on channel 0, streams on their own channels. Calls run side by side, safe from any goroutine; frames of a channel that come before the reply opening it wait for it. `Hello.Check` tells a core the client can't use.
- `stream.go`: a channel's stream: `Read` takes the bytes and credits the core for them, `Write` sends what is typed, and `Resize`, `Focus` and `Facts` (the viewer's colors and cell size, for programs that ask) send their frames. The core's CLOSE ends it with `io.EOF` and a reason; a lost connection with `ErrLost`.
- `terminal.go`: the terminal record and the calls Mux makes: `terminal.run`, `terminal.attach`, `terminal.kill`, `terminal.rm`, `doc.get`, `doc.set`, `server.stop`.
- `events.go`: `events.subscribe` and its notifications, and `Terminals`, which applies them to the snapshot as the schema says (missed brings a new snapshot; events not above its `seq` are dropped).
- `find.go`: finds `herdr-core` or `hc` (one whose `--version` says `hc`) on PATH or where its installer and package managers put it, reads the login shell's environment, and drops a core terminal's variables (`HC_TERMINAL_ID`, `HERDR_*`) from the client's.

### `internal/layout`

Mux's layout, kept in the core's `mux` document. No UI dependency.

- `layout.go`: workspaces of tabs, the tab shown, adding and removing them, pruning panes whose terminals are gone, and repairing a document read back.
- `tree.go`: a tab's binary tree of splits whose leaves are panes (a terminal, and a name the user gave): splitting, swapping, moving a pane beside another, laying the panes out in a box with lines between them, and finding the pane beside one toward a direction.

### `internal/term`

Shows a tab's panes, each a terminal of MyGo's terminal plugin, which parses the program's output and encodes keys, mouse and paste as the program asked (Kitty keyboard protocol, bracketed paste and so on), selects and copies, opens links and searches.

- `view.go`: `View` holds the panes' terminals across frames, made as a pane first shows and closed as it goes (`Retain`, `Drop`, `Reset`). A hidden tab's panes stay attached, so switching is instant, though each holds a stream and a scrollback. `Build` lays out the tab with headers along each pane's top (`HeaderHeight`), the dividers between panes, which drag, and asks for another frame as the area's size changes, as it lays out in the last committed box. Focus follows the keyboard both ways: a click in a terminal focuses its pane in the tab, and the tab's focus, as the app moves it, takes the keyboard. Cmd+arrow keys move the focus to the pane beside the focused one and Cmd+Option+arrow swap it with that pane. While a transition around it settles (`Settling`, as the sidebar slides), the panes take the width they settle at, so their programs don't redraw at every step.
- `conn.go`: a pane terminal's `Conn`: it attaches as the plugin first tells it the view's size, so the first paint comes at that size; until then reads wait and what is typed waits too. It tells the core the size, the cell size and the colors as they change.
- `search.go`: per-pane search (Cmd+F) with the plugin's search: Enter goes to older matches, Shift+Enter to newer.
- `rearrange.go`: dragging a pane by its title. Dropped within 25% of a target pane's edge, it goes beside the target; dropped in the middle, the two swap places.

### `cmd/mux/icons`

- `agents/`: agent marks as SVGs (currently Claude Code and Codex only), embedded at build time; `agentMarks` maps the `app` agents report to them. Agents without a mark get a generic icon.
- `app.png`: the app icon for use inside the app, embedded at build time: the Dock icon's tile at 256px, without its margin and drop shadow, which would show as gray corners in the UI. `scripts/make-icon.sh` renders it together with `resources/icon.png`.
- `appearance.svg`, `terminal.svg`: the Appearance and Terminal pages' icons in the settings sidebar, on the dark tile of `app.png`: System Settings' appearance mark (a ringed circle half light and half dark, around a smaller one the other way round) in cream as the app icon's art, and a prompt in Vellum's green.
- `appearance-light.svg`, `appearance-dark.svg`, `appearance-auto.svg`: the Appearance page's choices, the window drawn small in Vellum's light and dark colors, Auto's split along the diagonal as the README's screenshot is. `scripts/make-appearance-previews.sh` writes them; change them there.

### Other directories

`resources/` holds assets shipped inside the app bundle (the app icon); `design/` holds source files that are not shipped; `docs/` holds the README's screenshot; `scripts/` holds tools for maintaining the repository, such as `make-icon.sh`, which renders `resources/icon.png` and `cmd/mux/icons/app.png` from the artwork in `design/`; `make-screenshot.sh`, which makes `docs/screenshot.png` from a light and a dark screenshot of the window; and `make-appearance-previews.sh`, which writes the settings' appearance pictures.

## Notes

- Keep this file current. When a change adds, removes, renames or moves a module or file listed above, shifts a module's responsibility, or changes the commands, update this file in the same change.
- Before changing UI code, read `.agents/skills/mygo-maintenance/SKILL.md` for MyGo's lifetime, identity, threading and testing rules. UI tests use `ui.NewTester`.
- Never let a test or a probe act on the user's own core. The tests against a real herdr-core (`MUX_HC_TEST=1`) run one of their own: `HC_SESSION`, `XDG_RUNTIME_DIR`, `XDG_STATE_HOME` and `XDG_CONFIG_HOME` all point at temporary places, and they stop it as they end. Do the same for any probe by hand. Socket paths are short (104 bytes on macOS), so the runtime directory goes under `/tmp`, not `$TMPDIR`.
- Any `hc` command, `hc connect` and `hc schema` among them, starts the core if none runs: stopping it from the app holds the app's reconnection back until the user starts it again. Run even a lookup with the variables above.

### herdr-core

- The API is the core's to describe: `hc schema` (OpenRPC). It only grows, but is a preview (0.x); `Hello.Check` tells a core Mux can't use, by protocol generation and the methods it calls, not by version.
- The core draws nothing and has no layout: it relays raw output, answers terminal queries itself (from the `Facts` a viewer sends) and drops them from what it relays.
- It reads no screens: a program that reports no OSC 7501 status has none. It answers OSC 7501's probe itself (Claude Code waits about 2 seconds for it), keeps the `app` a program reports, and shows the last status to a new viewer.
- A terminal without a viewer is 200×120, as it starts and after its last viewer goes. A new shell prints its first prompt at that width and again at the pane's as Mux attaches, so the first line can show the two mixed.
- With several viewers, a terminal takes the size of the one that attached or typed last, and that of another as it goes.
- `keep` terminals come back after a core or machine restart with a fresh shell in their last folder (not the program they ran); until then, attaching says `unavailable`, and a `restarted` event follows.
- A core someone else started (a terminal's `hc`) keeps that one's environment: Mux's login-shell environment only reaches a core Mux starts.

### MyGo

- An element's `Draw` paints under its children, and each pane's terminal is a child of the pane area: what must show over the terminals (the drop zone, the drag chip) is a child built after them.
- `Handle.Bounds` is the last committed frame's: a view that lays out by its own size asks for another frame as that changes, or nothing shows until the next event.
- A press goes to the innermost element with `HandleInput` (the terminal in a pane, the pane area over a header), which then takes every move and the release, wherever the pointer goes.
- The terminal plugin calls a `Conn`'s size and theme methods on its own goroutine and waits for them; it writes `[Disconnected]` as the `Conn` ends. It sends no Cmd chords to the program: the pane area's shortcuts take them.
- A focus request takes effect in a later frame: tests wait for `Focused` before typing. `Tester.Focused` and `Find` go by label, and the settings' Terminal page shares one with the terminals.

### Not supported

- Herdr app's layout (its `herdr-app` document) and terminals of other machines (`machine/t7`). Such terminals still show as agents, and choosing one gives it a tab; for another machine's, `cwdOf` and Git branches read local paths.

### Not done yet

- Size with several viewers (`size` events): another viewer of a terminal (a second Mux on the same core, `hc attach`) takes its size as it attaches or types, and Mux keeps drawing at its own.
- `done` stays `done` after the user looks (Herdr app shows `idle`).
- The app menu has no Settings… item, and the View menu is MyGo's default, with Reload and Toggle Developer Tools.
