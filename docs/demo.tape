Output docs/assets/vsfleet.gif

Require go

Set Shell "bash"
Set FontSize 16
Set FontFamily "JetBrains Mono, DejaVu Sans Mono, monospace"
Set Width 1200
Set Height 560
Set Padding 20
Set Theme { "name": "vsfleet", "black": "#16161e", "red": "#ff7b72", "green": "#7ee787", "yellow": "#e3b341", "blue": "#7aa2f7", "magenta": "#bb9af7", "cyan": "#7dcfff", "white": "#e4e4e4", "brightBlack": "#565f89", "brightRed": "#ff7b72", "brightGreen": "#7ee787", "brightYellow": "#e3b341", "brightBlue": "#7aa2f7", "brightMagenta": "#bb9af7", "brightCyan": "#7dcfff", "brightWhite": "#ffffff", "background": "#16161e", "foreground": "#e4e4e4", "selectionBackground": "#3b4a6b", "cursorAccent": "#16161e" }
Set TypingSpeed 35ms
Set Framerate 24
Set PlaybackSpeed 1

Env TERM "xterm-256color"
Env COLORTERM "truecolor"
Env NO_COLOR ""

Hide
Type "rm -rf /tmp/vsfleet-readme-demo && mkdir -p /tmp/vsfleet-readme-demo"
Enter
Type "go build -o /tmp/vsfleet-readme-demo/vsfleet ./cmd/vsfleet-demo && go build -o /tmp/vsfleet-readme-demo/vsfleet-history ./cmd/vsfleet-testbed && echo VSFLEET_''DEMO_READY"
Enter
# A cold Go build cache takes minutes; the default five second wait does not
# survive that, and a recording that fails on a clean checkout is not
# reproducible.
Wait+Screen@5m /VSFLEET_DEMO_READY/
Type "export PATH=/tmp/vsfleet-readme-demo:$PATH"
Enter

# Start with the connected synthetic lab. Keeping its startup before the first
# Show prevents simulator boot time from becoming a long frozen GIF frame.
Type "export VSFLEET_TESTBED_ROOT=/tmp/vsfleet-readme-demo/history"
Enter
Type "vsfleet-history --port-base 38443"
Enter
Wait+Screen@30s /credentials required/
Type "H"
Wait+Screen@15s /RUNS/
Sleep 900ms
Show

# The hub opens on the newest pair. The change stream is ranked by what a
# change means for a migration, not by object name, and the inspector follows
# the cursor. "1" keeps only the blockers; "0" restores the full stream.
Sleep 2400ms
Down@450ms 3
Sleep 1400ms
Type "1"
Sleep 2200ms
Type "0"
Sleep 1000ms

# edge-vc missed run #3, so the span is not estate-wide until "c" clips the
# baseline back to the newest run with matching coverage.
Type "c"
Wait+Screen /clipped the span/
Sleep 2600ms

# The stored runs and the read-only health verdict share the hub.
Tab
Sleep 100ms
Tab
Wait+Screen /newest first/
Sleep 2400ms
Tab
Wait+Screen /read-only findings/
Sleep 2800ms

# Move to the friendly offline estate while capture is hidden.
Hide
Type "q"
Sleep 500ms
Type "vsfleet"
Enter
Wait+Screen /api-01/
Show
Sleep 1500ms

# Inspect a VM, then jump straight to the new vApp inventory and its hierarchy.
Enter
Sleep 2200ms
Escape
Sleep 300ms
Type "7"
Sleep 1800ms
Enter
Sleep 2600ms
Escape
Sleep 300ms

# Visit the routed edge vCenter so the later search and all-context view can
# aggregate both healthy sites while preserving the failed DR context.
Type "c"
Sleep 1200ms
Down
Enter
Wait+Screen /edge-vc  ·  2 connected/
Sleep 700ms
Type "1"
Wait+Screen /10.42.0.11/
Sleep 800ms

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

# Stop capturing before quitting: leaving the alt screen records a blank frame,
# which flashes on every loop of the GIF.
Escape
Sleep 1200ms
Hide
Type "q"
Sleep 700ms
Type "rm -rf /tmp/vsfleet-readme-demo"
Enter
