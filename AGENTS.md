# AGENTS.md

Mux is a native GUI client for herdr, a terminal multiplexer for coding agents, written in Go with [MyGo](https://github.com/egoist/mygo)'s native `ui` package. It connects to herdr's client endpoint and replaces herdr's TUI with a window: a sidebar of workspaces, tabs and agent states on the left, and the chosen tab's terminal panes on the right. The herdr server still runs the terminals; Mux only paints them and forwards input, and never parses program output.

## Commands

```sh
go tool mygo dev                   # run a development build with live reload
go tool mygo build                 # package the app (.app and .dmg on macOS, into build/)
go tool mygo vet .                 # go vet plus checks for stored UI build values
CGO_ENABLED=0 go test ./...        # all tests
go test ./src/term -run TestSearch # a single test
```

`mygo` is pinned through the `tool` directive in `go.mod`; nothing to install separately. App settings (name, identifier, entry `./src`) live in `mygo.json`.

## Modules

Dependencies run `main` → `term` → `herdr/endpoint`; the agent icons are used only by `main`.

### `src/` (package `main`)

The app itself: top-level UI and app state.

- `main.go`:
  - Creates the window. On macOS the title bar is hidden and the traffic lights sit over the sidebar, centered on the pane headers.
  - The sidebar groups tabs by workspace and has a list of agents with breathing status dots. Workspaces, tabs and panes can be renamed in place, collapsed and created; closing asks for confirmation.
  - Pane headers carry the split, zoom and close buttons.
  - A background goroutine connects to the endpoint and reconnects every second after a drop. Messages it reads go into an inbox and reach the main thread through `Window.Update`, a batch at a time.
  - Request queue: the endpoint handles one request at a time (otherwise it answers `endpoint_busy`), so requests wait here and the next is sent once the previous one is answered.
- `theme.go`: the Vellum colors in light and dark. Controls take its Zed colors, terminals its Ghostty colors.

### `src/herdr/endpoint`

A client of herdr's client endpoint protocol (generation 1). No UI dependency.

- `conn.go`: resolves the socket path (`HERDR_SOCKET_PATH`, else `HERDR_SESSION`, else the default session), performs the hello / welcome handshake, reads framed messages, and sends requests and input.
- `bincode.go`: bincode 2 encoding and decoding: little endian, variable-length integers.
- `wire.go`: message types mirroring herdr's `src/protocol/wire.rs`, whose field order is the wire format. The two main payloads are the Snapshot (workspaces, tabs, panes and agents, sent as JSON) and the Surface (the laid-out grid of cells).
- `input.go`: input events sent to a pane: keys, text, paste and mouse.
- `surface.go`: applies incremental patches to the local surface. A patch for another revision returns `ErrStalePatch`, on which `main` drops the connection and reconnects to get a whole surface.

### `src/term`

Paints the terminals and handles their input. It never parses program output; it paints the cells the server already laid out.

- `view.go`: `View` holds state across frames (font, glyph cache, the size last sent to the server). `Build` is the entry point. Input and size go out through the `Endpoint` interface; header content, focus, requests and the like go back to `main` through `Hooks`.
- `paint.go` / `color.go`: paint cells, search highlights and the selection. Colors use a 256-color palette (the theme's 16 plus xterm's color cube and grays) and cell styles such as bold, italic and reverse.
- `layout.go`: computes each pane's card, header and content boxes. herdr leaves a border row above and below split panes, which the header takes.
- `keys.go`: maps `ui.Key` to endpoint keys. Keys that type text go as text input; Cmd chords are kept for the app.
- `select.go`: text selection and copy. The selection stays with the text as it scrolls.
- `search.go`: per-pane search (Cmd+F), performed by the server. Responses to earlier searches are dropped, and a search reruns when the pane's content changes.
- `rearrange.go`: dragging a pane by its title. Dropped within 25% of a target pane's edge, it goes beside the target; dropped in the middle, the two swap places.

### `src/icons/agents`

Agent marks as SVGs (currently Claude Code and Codex only), named by the agent kind herdr reports and embedded at build time. Agents without a mark get a generic icon.

### Other directories

`resources/` holds assets shipped inside the app bundle (the app icon); `design/` holds source files that are not shipped.

## Notes

- Keep this file current. When a change adds, removes, renames or moves a module or file listed above, shifts a module's responsibility, or changes the commands, update this file in the same change.
- Before changing UI code, read `.agents/skills/mygo-maintenance/SKILL.md` for MyGo's lifetime, identity, threading and testing rules. UI tests use `ui.NewTester`.
- During development the agent usually runs inside a herdr pane, with `HERDR_SOCKET_PATH` and friends set. Any temporary test that actually dials a herdr socket (calls `endpoint.Dial` / `DefaultSocketPath`) must `t.Skip` unless `HERDR_SESSION == "guiprobe"`, be run with `env -u HERDR_ENV -u HERDR_SOCKET_PATH -u HERDR_PANE_ID -u HERDR_TAB_ID -u HERDR_WORKSPACE_ID HERDR_SESSION=guiprobe ...`, and be deleted before running `go test ./...`; otherwise it acts on the user's live default session.
