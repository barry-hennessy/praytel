# Shared preamble for every scenario tape: canvas settings, then a hidden
# tmux session whose status line is the caption bar (see demo/tmux.conf).
# Include with: Source "/demo/common.tape"

Require tmux
Require praytel

Set Shell bash
Set FontSize 16
Set Width 1500
Set Height 900
Set Padding 16
Set TypingSpeed 45ms
# Frame capture in a container sustains roughly 12 fps at this size. A higher
# nominal rate is encoded as if it had been met, which speeds playback up.
Set Framerate 10

Hide
# The pipeline's own telemetry variables are present in the container. Drop
# them so the recorded shell starts clean, as a normal terminal would.
Type "for v in $(env | grep -oE '^(OTEL_[A-Z_]+|TRACEPARENT)'); do unset $v; done" Enter
Type "tmux -u -f /demo/tmux.conf new -s demo" Enter
Sleep 1s
Type "PS1='$ ' && clear" Enter
Show
