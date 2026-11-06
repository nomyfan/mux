---
name: mux
description: Find where you run in Mux, the herdr-core (hc) GUI, and reach the other panes of your tab or workspace. Use when $HC_TERMINAL_ID is set and the user refers to Mux's workspaces, tabs or panes ("the pane beside me", "the server pane", "the other agent in this tab", "the dev tab"), or asks you to read, type into, wait for or stop the program of another pane.
---

# Panes in Mux

Mux shows herdr-core's terminals in workspaces of tabs, each tab split into
panes, one terminal each. The core itself has no workspaces or tabs: Mux
labels each pane's terminal with where it is, and keeps the labels current
as the user renames things or moves a tab to another workspace.

| Label | Value |
|---|---|
| `mux.workspace` | the workspace's id |
| `mux.workspace.name` | the name the user gave the workspace |
| `mux.tab` | the tab's id |
| `mux.tab.name` | the name the user gave the tab |
| `mux.pane.name` | the name the user gave the pane |

A `.name` label is there only when the user gave that name. The window shows
no ids: the user calls things by the names they gave or by what shows
without one: a workspace by its first pane's folder, a tab by its focused
pane's name, a pane by its agent or program (`claude`, `npm`, `shell`).

## Where you are

`$HC_TERMINAL_ID` is your terminal's address (`t7`, or `machine/t7` on
another machine). Pass it: `hc get` without an address does not pick yours.

```sh
hc get "$HC_TERMINAL_ID" --json | jq .labels
```

No `mux.*` labels means Mux does not show your terminal (or runs a version
without them): say so rather than guessing.

## The panes around you

`hc ls --label k=v` lists the live terminals carrying that label, exactly
(no wildcards); repeat `--label` for all of several. Its output has no
labels, so get them from each terminal:

```sh
me=$(hc get "$HC_TERMINAL_ID" --json)
tab=$(jq -r '.labels["mux.tab"]' <<<"$me")
ws=$(jq -r '.labels["mux.workspace"]' <<<"$me")

hc ls --label "mux.tab=$tab"          # this tab's panes, you among them
for t in $(hc ls --label "mux.workspace=$ws" --json | jq -r '.terminals[].address'); do
  hc get "$t" --json | jq -c '{address, argv, cwd: (.foreground.cwd // .cwd), labels, status: .status.state}'
done                                  # this workspace's panes, with their names and what they run
```

To find a tab or pane by its name, filter on it, with the workspace too, as
names repeat across workspaces: `hc ls --label "mux.workspace=$ws" --label
"mux.tab.name=dev"`. Leave yourself (`$HC_TERMINAL_ID`) out of what you
act on.

The labels say nothing of where panes lie in a tab (left, right, above):
for "the pane on the right", match it by its name or program, or ask the
user which one they mean. Ask too when more than one pane fits.

## Acting on a pane

The user may be watching or typing in that pane. Read it first, and ask
before sending anything that changes or ends work you were not asked to
touch.

```sh
hc read --lines 50 <t>        # its screen and recent history
hc send <t> "npm test"        # paste text and press Enter (waits ~3 s for no typing)
hc keys <t> ctrl+c            # press keys
hc interrupt <t>              # stop an agent's turn, or ctrl+c a program
hc wait <t>                   # until its agent stops working; instead of sleep
```

`hc --skill` describes these and the rest of `hc`.
