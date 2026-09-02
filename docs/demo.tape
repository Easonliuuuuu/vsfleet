Output docs/assets/vsfleet.gif

Require go

Set Shell "bash"
Set FontSize 16
Set FontFamily "JetBrains Mono, DejaVu Sans Mono, monospace"
Set Width 1200
Set Height 700
Set Padding 20
Set Theme { "name": "vsfleet", "black": "#16161e", "red": "#ff7b72", "green": "#7ee787", "yellow": "#e3b341", "blue": "#7aa2f7", "magenta": "#bb9af7", "cyan": "#7dcfff", "white": "#e4e4e4", "brightBlack": "#565f89", "brightRed": "#ff7b72", "brightGreen": "#7ee787", "brightYellow": "#e3b341", "brightBlue": "#7aa2f7", "brightMagenta": "#bb9af7", "brightCyan": "#7dcfff", "brightWhite": "#ffffff", "background": "#16161e", "foreground": "#e4e4e4", "selectionBackground": "#3b4a6b", "cursorAccent": "#16161e" }
Set TypingSpeed 35ms
Set PlaybackSpeed 1

Env TERM "xterm-256color"
Env COLORTERM "truecolor"
Env NO_COLOR ""

Hide
Type "mkdir -p /tmp/vsfleet-readme-demo"
Enter
Type "go build -o /tmp/vsfleet-readme-demo/vsfleet ./cmd/vsfleet-demo && echo VSFLEET_''DEMO_READY"
Enter
Wait+Screen /VSFLEET_DEMO_READY/
Type "export PATH=/tmp/vsfleet-readme-demo:$PATH"
Enter
Type "clear"
Enter
Sleep 500ms
Show

Type "vsfleet"
Enter
Wait+Screen /CONTEXTS/
Sleep 1s

Type "a"
Sleep 2s
Screenshot docs/assets/vsfleet.png

Right@500ms 2
Sleep 2s

Type "/"
Type@120ms "postgres"
Sleep 2s
Enter
Sleep 1s
Escape
Sleep 1s

Tab
Down@400ms 2
Type "d"
Sleep 3s

Escape
Type "q"
