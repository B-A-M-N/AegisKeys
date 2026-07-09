Output docs/demo/tui-matrix-logo.gif

Set Shell "bash"
Set Theme "Dracula"
Set Width 1280
Set Height 760
Set FontSize 15
Env TERM "xterm-256color"
Env COLORTERM "truecolor"
Env FORCE_COLOR "1"
Env CLICOLOR_FORCE "1"

Type "clear"
Enter
Sleep 500ms

Type "rm -rf tmp/vhs-tui"
Enter
Sleep 500ms

Type "./aegiskeys --config tmp/vhs-tui init"
Enter
Sleep 500ms
Type "demo-password"
Enter
Sleep 500ms
Type "demo-password"
Enter
Sleep 2s

Type "./aegiskeys --config tmp/vhs-tui settings set theme vault"
Enter
Sleep 1s

Type "./aegiskeys --config tmp/vhs-tui tui"
Enter
Sleep 1s
Type "demo-password"
Enter
Sleep 4s

Type "2"
Sleep 2s
Type "3"
Sleep 2s
Type "4"
Sleep 2s
Type "5"
Sleep 3s

Ctrl+C
Sleep 1s
