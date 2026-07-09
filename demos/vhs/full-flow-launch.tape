Output docs/demo/full-flow-launch.mp4

Require vhs
Require ttyd

Set Shell "bash"
Set Theme "Dracula"
Set Width 1440
Set Height 900
Set FontSize 15
Set Framerate 30
Set TypingSpeed 35ms
Set PlaybackSpeed 1.0
Env TERM "xterm-256color"
Env COLORTERM "truecolor"
Env FORCE_COLOR "1"
Env CLICOLOR_FORCE "1"

Hide
Type "rm -rf tmp/vhs-full"
Enter
Sleep 500ms

Type "./aegiskeys --config tmp/vhs-full init --password demo-password"
Enter
Sleep 1s

Type "./aegiskeys --config tmp/vhs-full settings set theme vault"
Enter
Sleep 500ms

Type "./aegiskeys --config tmp/vhs-full settings set auto_lock_minutes 0"
Enter
Sleep 500ms

Type "./aegiskeys --config tmp/vhs-full key add --provider openai --label demo-openai"
Enter
Sleep 500ms
Type "demo-password"
Enter
Sleep 500ms
Type "sk-demo-not-real-000000000000abcd"
Enter
Sleep 1s

Type "KEY_ID=$(printf 'demo-password\n' | ./aegiskeys --config tmp/vhs-full key list | awk 'NR==2 {print $1}')"
Enter
Sleep 500ms

Type './aegiskeys --config tmp/vhs-full profile create --name demo-launch --provider openai --key "$KEY_ID" --app generic --command bash --model-main gpt-5 --notes "VHS demo profile: safe fake key, real launch path"'
Enter
Sleep 500ms
Type "demo-password"
Enter
Sleep 1s

Show
Type "clear"
Enter
Sleep 500ms

Type "printf 'AegisKeys full-flow demo: vault, providers, masked keys, profiles, matrix logos, launch, doctor, audit, settings, scratch, help\n'"
Enter
Sleep 3s

Type "./aegiskeys --config tmp/vhs-full tui"
Enter
Sleep 1s
Type "demo-password"
Enter
Sleep 5s

Type "2"
Sleep 4s
Type "m"
Sleep 4s
Type "/"
Sleep 300ms
Type "gpt"
Sleep 3s
Escape
Sleep 1s
Escape
Sleep 2s

Type "3"
Sleep 4s
Type "4"
Sleep 4s
Type "5"
Sleep 5s

Right
Sleep 500ms
Copy 'bash -lc "echo AegisKeys child launch complete; echo OPENAI_API_KEY=<injected and redacted>; echo OPENAI_MODEL=$OPENAI_MODEL; echo OPENAI_BASE_URL=$OPENAI_BASE_URL; sleep 2"'
Paste
Sleep 2s
Enter
Sleep 7s

Type "6"
Sleep 5s
Type "7"
Sleep 5s
Type "8"
Sleep 4s
Type "9"
Sleep 2s
Type "n"
Sleep 1s
Ctrl+U
Type "Launch checklist"
Sleep 1s
Tab
Sleep 500ms
Copy "Provider: OpenAI demo\nKey: masked in views\nProfile: generic launch\nLaunch: child process only\nAudit: metadata only"
Paste
Sleep 2s
Ctrl+S
Sleep 3s
Type "v"
Sleep 1s
Type "j"
Sleep 700ms
Type "j"
Sleep 1s
Type "y"
Sleep 2s
Type "?"
Sleep 5s

Ctrl+C
Sleep 1s
