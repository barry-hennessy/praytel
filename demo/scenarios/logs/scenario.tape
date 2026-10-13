# View logs correlated to traces. See docs/guides/logs.md.
Output "/out/logs.gif"
Output "/out/logs.mp4"

Source "/demo/common.tape"

# 1: what the app prints on its own
Ctrl+B
Type "1"
Sleep 1s
Type "shop -pace 50ms 2>/dev/null | head -4 | cut -c1-140"
Sleep 500ms
Enter
Sleep 4s

# 2: the same logs, joined
Type "clear && praytel --tui -- shop"
Sleep 500ms
Enter
Wait+Screen@30s /Orders\.Create/
Ctrl+B
Type "2"
Wait+Screen@30s /complete/
Sleep 4s

# 3: orphans
Ctrl+B
Type "3"
Sleep 4s

# 4: search log bodies
Ctrl+B
Type "4"
Sleep 1s
Tab
Sleep 1s
Type "refused"
Sleep 5s
Ctrl+C
Sleep 500ms
