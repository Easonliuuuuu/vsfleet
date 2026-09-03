Output docs/assets/vsfleet.gif

Require go

Set Shell "bash"
Set FontSize 16
Set FontFamily "JetBrains Mono, DejaVu Sans Mono, monospace"
Set Width 1200
Set Height 500
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
# A cold Go build cache takes minutes; the default five second wait does not
# survive that, and a recording that fails on a clean checkout is not
# reproducible.
Wait+Screen@5m /VSFLEET_DEMO_READY/
Type "export PATH=/tmp/vsfleet-readme-demo:$PATH"
Enter
Type "clear"
Enter
Sleep 500ms
Show

Type "vsfleet"
Enter
Wait+Screen /api-01/
Sleep 1500ms

# The number row jumps straight to a resource kind.
Type "3"
Sleep 1200ms
Type "5"
Sleep 1200ms
Type "1"
Sleep 1s

# "/" narrows the table; the query line reports what the rest of the estate
# holds, and "tab" widens the same query to every vCenter and every kind.
Type "/"
Type@120ms "esxi-0"
Sleep 2500ms
Tab
Sleep 3500ms
Escape
Sleep 300ms
Escape
Sleep 800ms

# Every vCenter at once — the unreachable site costs one line, not the screen.
Type "a"
Sleep 2500ms
Screenshot docs/assets/vsfleet.png
Sleep 500ms

# The contexts screen carries the routes, the latencies, and the reason a
# vCenter is not answering.
Type "c"
Sleep 2500ms
Down@400ms 2
Sleep 800ms

# And "d" walks the connection stage by stage.
Type "d"
Sleep 3500ms

# Back to the table, and stop capturing before quitting: leaving the alt
# screen records a blank frame, which flashes on every loop of the GIF.
Escape
Sleep 2s
Hide
Type "q"
Sleep 500ms
