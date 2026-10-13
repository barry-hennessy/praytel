# View traces from a running app. See docs/guides/traces.md.
Output "/out/traces.gif"
Output "/out/traces.mp4"

Source "/demo/common.tape"

# 1: wrap the command
Ctrl+B
Type "1"
Sleep 1s
Type "praytel --tui -- shop"
Sleep 800ms
Enter

# 2: the live tree
Wait+Screen@30s /Orders\.Create/
Ctrl+B
Type "2"
Wait+Screen@30s /complete/
Sleep 4s

# 3: search
Ctrl+B
Type "3"
Sleep 1s
Type "gift"
Sleep 4s
Escape
Sleep 1s

# 4: stays open
Ctrl+B
Type "4"
Sleep 3s
Ctrl+C
Sleep 500ms
