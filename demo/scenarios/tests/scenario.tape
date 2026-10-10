# View traces of tests. See docs/guides/tests.md.
Output "/out/tests.gif"
Output "/out/tests.mp4"

Source "/demo/common.tape"

# 1: the bare test run
Ctrl+B
Type "1"
Sleep 1s
Type "shop.test 2>&1 | grep -v '^{'"
Sleep 500ms
Enter
Sleep 5s

# 2: the same suite under praytel
Ctrl+B
Type "2"
Type "clear && praytel --tui -- shop.test"
Sleep 500ms
Enter
Wait+Screen@30s /complete/
Sleep 3s

# 3: find the failing test
Ctrl+B
Type "3"
Sleep 1s
Type "TestGift"
Sleep 3s

# 4: follow the error down
Ctrl+B
Type "4"
Sleep 7s
Ctrl+C
Sleep 500ms
