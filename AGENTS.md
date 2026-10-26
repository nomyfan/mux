# AGENTS.md

Mux is a native GUI client for herdr, a terminal multiplexer for coding agents, written in Go with [MyGo](https://github.com/egoist/mygo)'s native `ui` package. It connects to herdr's client endpoint and replaces herdr's TUI with a window: a sidebar of workspaces, tabs and agent states on the left, and the chosen tab's terminal panes on the right. The herdr server still runs the terminals; Mux only paints them and forwards input, and never parses program output.

## Commands

```sh
go tool mygo dev                          # run a development build with live reload
go tool mygo build                        # package the app (.app and .dmg on macOS, into build/)
go tool mygo vet .                        # go vet plus checks for stored UI build values
CGO_ENABLED=0 go test ./...               # all tests
go test ./internal/term -run TestSearch   # a single test
scripts/make-icon.sh                      # render resources/icon.png and cmd/mux/icons/app.png from the artwork in design/ (needs ImageMagick)
scripts/make-screenshot.sh light.png dark.png  # split two window screenshots along the diagonal into docs/screenshot.png (needs ImageMagick)
scripts/make-appearance-previews.sh       # write the settings' appearance pictures, cmd/mux/icons/appearance-{light,dark,auto}.svg
```

`mygo` is pinned through the `tool` directive in `go.mod`; nothing to install separately. App settings (name, identifier, entry `./cmd/mux`) live in `mygo.config.ts`, which MyGo evaluates with Bun or Node.js 22.6+; it sets `CFBundleVersion` to the short commit hash, shown in parentheses in the about panel.

## Modules

Dependencies run `main` → `term` → `herdr/endpoint`; `herdr/server` and the icons are used only by `main`.

### `cmd/mux/` (package `main`)

The app itself: top-level UI and app state.

- `main.go`:
  - Creates the window. On macOS the title bar is hidden and the traffic lights sit over the sidebar, centered on the pane headers.
  - The sidebar groups tabs by workspace and has a list of agents with breathing status dots. Workspaces, tabs and panes can be renamed in place, collapsed and created; closing asks for confirmation. Cmd+T makes a tab in the focused workspace, Cmd+N a workspace, Cmd+W closes the focused tab (confirmed) and Cmd+Shift+W the focused pane; on macOS these four sit in the app's own File menu, as the default one's Close Window would take Cmd+W. Cmd+Shift+[ / ] go around the focused workspace's tabs, Cmd+1–8 to one of them and Cmd+9 to its last; Cmd+Shift+Enter zooms the focused pane. New tabs, workspaces and split panes pass no `cwd`, only where they come from, so herdr's `[terminal] new_cwd` decides where they start (by default, the source pane's directory).
  - Pane headers carry the split, zoom and close buttons.
  - A background goroutine connects to the endpoint and reconnects every second after a drop. Messages it reads go into an inbox and reach the main thread through `Window.Update`, a batch at a time.
  - Request queue: the endpoint handles one request at a time (otherwise it answers `endpoint_busy`), so requests wait here and the next is sent once the previous one is answered.
- `settings.go`: the settings dialog (Cmd+,): pages listed on the left (Server, Appearance, Terminal), the chosen one on the right, scrolling under its title. Appearance is auto, light or dark, chosen among pictures of the window in each (`icons/appearance-*.svg`) and applied through `mygo.Theme.SetSource`. Terminal shows a preview of the terminals' text, drawn as they draw it, above their fonts: a family chosen among the system's monospaced ones (default: the system's own, `monospace`) and an optional fallback family, among all of them, for the glyphs the first lacks such as Nerd Font icons (default: none), their size (8–32, default 14) (`ui.FontFamilies`, listed in the background at startup), and, on macOS, font smoothing (default: off, thinner, as Ghostty draws by default; on is `ui.Font`'s smoothing as AppKit draws text, off its `Antialiased`). They apply at once through `term.View.SetFont`. Settings are saved as `settings.json` in the app's user data directory and applied before the window opens.
- `server.go`: the herdr server's state in the UI. When no server listens on the socket, the sidebar is emptied and the pane area offers Start Server; the settings' Server page shows the status and starts or stops it (stopping asks for confirmation). The server is shown with the in-app icon `cmd/mux/icons/app.png`.
- `theme.go`: the Vellum colors in light and dark. Controls take its Zed colors; terminals take its Ghostty colors, with a true blue and cyan, their own bright colors and grays that show on either ground, and the cursor in the controls' accent.

### `internal/herdr/endpoint`

A client of herdr's client endpoint protocol (generation 1). No UI dependency.

- `conn.go`: resolves the socket path (`HERDR_SOCKET_PATH`, else `HERDR_SESSION`, else the default session), performs the hello / welcome handshake, reads framed messages, and sends requests and input.
- `bincode.go`: bincode 2 encoding and decoding: little endian, variable-length integers.
- `wire.go`: message types mirroring herdr's `src/protocol/wire.rs`, whose field order is the wire format. The two main payloads are the Snapshot (workspaces, tabs, panes and agents, sent as JSON) and the Surface (the laid-out grid of cells).
- `input.go`: input events sent to a pane: keys, text, paste and mouse.
- `surface.go`: applies incremental patches to the local surface. A patch for another revision returns `ErrStalePatch`, on which `main` drops the connection and reconnects to get a whole surface.

### `internal/herdr/server`

Starts and stops the herdr server. No UI dependency.

- `server.go`: the API socket beside the client socket (`herdr.sock` next to `herdr-client.sock`), whose JSON protocol takes one request per line; `Stop` sends `server.stop` there, as the client endpoint doesn't offer it. `NotRunning` tells a missing or refusing socket from other dial errors.
- `start_unix.go`: `Start` runs `herdr server` through the user's login shell (`$SHELL -l -c`), in its own session so it outlives the app, so that it finds herdr and its panes get the user's environment rather than the Finder's. `start_other.go` stubs it elsewhere.

### `internal/term`

Paints the terminals and handles their input. It never parses program output; it paints the cells the server already laid out.

- `view.go`: `View` holds state across frames (font, glyph cache, the size last sent to the server). `SetFont` changes the font at run time; the new cell size reaches the server on the next layout. `Build` is the entry point. Input and size go out through the `Endpoint` interface; header content, focus, requests and the like go back to `main` through `Hooks`. Cmd+arrow keys move the focus to the pane beside the focused one and Cmd+Option+arrow swap it with that pane, as the server finds it (`pane.focus_direction`, `pane.swap`).
- `paint.go` / `color.go`: paint cells, search highlights, the selection and the cursor, in the shape the program sets (DECSCUSR) and blinking when that shape does. Colors use a 256-color palette (the theme's 16 plus xterm's color cube and grays) and cell styles such as bold, italic and reverse.
- `block.go`: draws block elements (U+2580–259F) as rectangles of the cell, as Ghostty does, rather than the font's glyphs, whose antialiased edges show as seams between neighboring blocks.
- `layout.go`: computes each pane's card, header and content boxes. herdr leaves a border row above and below split panes, which the header takes.
- `keys.go`: maps `ui.Key` to endpoint keys. Keys that type text go as text input; Cmd chords are kept for the app.
- `select.go`: text selection and copy. The selection stays with the text as it scrolls.
- `search.go`: per-pane search (Cmd+F), performed by the server. Responses to earlier searches are dropped, and a search reruns when the pane's content changes.
- `rearrange.go`: dragging a pane by its title. Dropped within 25% of a target pane's edge, it goes beside the target; dropped in the middle, the two swap places.

### `cmd/mux/icons`

- `agents/`: agent marks as SVGs (currently Claude Code and Codex only), named by the agent kind herdr reports and embedded at build time. Agents without a mark get a generic icon.
- `app.png`: the app icon for use inside the app, embedded at build time: the Dock icon's tile at 256px, without its margin and drop shadow, which would show as gray corners in the UI. `scripts/make-icon.sh` renders it together with `resources/icon.png`.
- `appearance.svg`, `terminal.svg`: the Appearance and Terminal pages' icons in the settings sidebar, on the dark tile of `app.png`: System Settings' appearance mark (a ringed circle half light and half dark, around a smaller one the other way round) in cream as the app icon's art, and a prompt in Vellum's green.
- `appearance-light.svg`, `appearance-dark.svg`, `appearance-auto.svg`: the Appearance page's choices, the window drawn small in Vellum's light and dark colors, Auto's split along the diagonal as the README's screenshot is. `scripts/make-appearance-previews.sh` writes them; change them there.

### Other directories

`resources/` holds assets shipped inside the app bundle (the app icon); `design/` holds source files that are not shipped; `docs/` holds the README's screenshot; `scripts/` holds tools for maintaining the repository, such as `make-icon.sh`, which renders `resources/icon.png` and `cmd/mux/icons/app.png` from the artwork in `design/`; `make-screenshot.sh`, which makes `docs/screenshot.png` from a light and a dark screenshot of the window; and `make-appearance-previews.sh`, which writes the settings' appearance pictures.

## Notes

- Keep this file current. When a change adds, removes, renames or moves a module or file listed above, shifts a module's responsibility, or changes the commands, update this file in the same change.
- Before changing UI code, read `.agents/skills/mygo-maintenance/SKILL.md` for MyGo's lifetime, identity, threading and testing rules. UI tests use `ui.NewTester`.
- During development the agent usually runs inside a herdr pane, with `HERDR_SOCKET_PATH` and friends set. Any temporary test that actually dials a herdr socket (calls `endpoint.Dial` / `DefaultSocketPath`) must `t.Skip` unless `HERDR_SESSION == "guiprobe"`, be run with `env -u HERDR_ENV -u HERDR_SOCKET_PATH -u HERDR_PANE_ID -u HERDR_TAB_ID -u HERDR_WORKSPACE_ID HERDR_SESSION=guiprobe ...`, and be deleted before running `go test ./...`; otherwise it acts on the user's live default session.
