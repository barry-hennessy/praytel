# Read a real tool's graph: a Dagger pipeline. See docs/guides/graph.md.
Output "/out/graph.gif"
Output "/out/graph.mp4"

Source "/demo/common.tape"

# Load the module once, off camera, so the recorded run is the pipeline
# rather than Dagger generating the module's code. Under nesting, dagger
# only finds the module in the working directory once it has been loaded
# by path, so this run names it.
Hide
Type "cp -r /workload /src && cd /src && dagger -s -m /src call release > /dev/null 2>&1; echo warmed-up" Enter
Wait+Screen@600s /warmed-up/
Type "clear" Enter
Sleep 500ms
Show

# 1: run the pipeline
Ctrl+B
Type "1"
Sleep 1s
Type "praytel --tui -- dagger -s call release"
Sleep 800ms
Enter
Wait+Screen@120s /complete/
Sleep 3s

# 2: the graph
Ctrl+B
Type "2"
Sleep 1s
Ctrl+G
Sleep 3s
PageDown@1s 4
Sleep 1s
PageUp 4
Sleep 1s

# 3: search logs
Ctrl+B
Type "3"
Sleep 2s
Tab
Sleep 1s

# 4: the builds
Ctrl+B
Type "4"
Type "CGO_ENABLED=0"
Sleep 5s

# 5: the cycle
Ctrl+B
Type "5"
Sleep 1s
Right 5
Sleep 5s
Left 5
Sleep 1s

# 6: back to the tree
Ctrl+B
Type "6"
Sleep 1s
Ctrl+G
Sleep 4s
Ctrl+C
Sleep 500ms
