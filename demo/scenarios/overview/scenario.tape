# Get an overview of a system. See docs/guides/overview.md.
Output "/out/overview.gif"
Output "/out/overview.mp4"

Source "/demo/common.tape"

# 1: the tree
Ctrl+B
Type "1"
Sleep 1s
Type "praytel --tui -- shop"
Sleep 500ms
Enter
Wait+Screen@30s /complete/
Sleep 2s

# 2: the graph
Ctrl+B
Type "2"
Sleep 1s
Ctrl+G
Sleep 5s

# 3: where boxes come from
Ctrl+B
Type "3"
Sleep 5s

# 4: scope it
Ctrl+B
Type "4"
Sleep 1s
Type "gift"
Sleep 6s
Ctrl+C
Sleep 500ms
