Output docs/demo/cli-overview.gif

Set Shell "bash"
Set Width 1200
Set Height 720
Set FontSize 16

Type "clear"
Enter
Sleep 500ms

Type "rm -rf tmp/vhs-cli"
Enter
Sleep 500ms

Type "./aegiskeys --config tmp/vhs-cli init"
Enter
Sleep 500ms
Type "demo-password"
Enter
Sleep 500ms
Type "demo-password"
Enter
Sleep 2s

Type "./aegiskeys --config tmp/vhs-cli provider list"
Enter
Sleep 3s

Type "./aegiskeys --config tmp/vhs-cli adapter verify --app aider"
Enter
Sleep 3s

Type "./aegiskeys --config tmp/vhs-cli doctor"
Enter
Sleep 3s
