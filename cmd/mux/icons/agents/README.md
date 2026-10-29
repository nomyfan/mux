# Agent icons

Each file is the mark of a coding agent, which the sidebar shows beside the
agent. Agents name themselves in their status reports (OSC 7501's `app`, as
`claude-code` or `codex`), and `agentMarks` in `cmd/mux/model.go` maps those
names to these files. Only Claude Code's and Codex's are here for now; other
agents show a generic mark.

The SVGs come from [LobeHub Icons](https://github.com/lobehub/lobe-icons)
(`@lobehub/icons-static-svg` 1.98.0, MIT): the `-color` version where there
is one, else the monochrome one, drawn in `currentColor`, which follows the
text. The marks are trademarks of their owners.

`codex.svg` has lost the white rounded square under the mark, which stood
out on the sidebar, dark ones most.
