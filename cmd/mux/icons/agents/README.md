# Agent icons

Each file is the mark of a coding agent, which the sidebar shows beside the
agent. Agents name themselves in their status reports (OSC 7501's `app`, as
`claude-code`, `codex` or `pi`), and `agentMarks` in `cmd/mux/model.go` maps
those names to these files. Only Claude Code's, Codex's and Pi's are here for
now; other agents show a generic mark.

The SVGs come from [LobeHub Icons](https://github.com/lobehub/lobe-icons)
(`@lobehub/icons-static-svg` 1.98.0, MIT): the `-color` version where there
is one, else the monochrome one, drawn in `currentColor`, which follows the
text. The marks are trademarks of their owners.

`pi.svg` is pi's own colored logo (<https://pi.dev/logo.svg>), as LobeHub
has only a monochrome one. Its view box leaves the mark about 17/24 of the
icon, a little under Codex's round one: a solid square filling as much as
the others' marks looked bigger beside them.

`codex.svg` has lost the white rounded square under the mark, which stood
out on the sidebar, dark ones most.
