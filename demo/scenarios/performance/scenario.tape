# Focus on performance. See docs/guides/performance.md.
Output "/out/performance.gif"
Output "/out/performance.mp4"

Source "/demo/common.tape"

# 1: the query
Ctrl+B
Type "1"
Sleep 1s
Type "praytel --tui --query 'span{ span:duration > 300ms }' -- shop"
Sleep 800ms
Enter

# 2: the slow path
Wait+Screen@30s /complete/
Ctrl+B
Type "2"
Sleep 7s
Ctrl+C
Sleep 500ms

# 3: matches only
Ctrl+B
Type "3"
Type "clear && praytel --tui --query 'span{ span:duration > 300ms } | matches' -- shop"
Sleep 800ms
Enter
Wait+Screen@30s /complete/
Sleep 6s
Ctrl+C
Sleep 500ms
