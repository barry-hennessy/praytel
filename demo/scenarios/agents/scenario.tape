# Using praytel with agents. See docs/guides/agents.md.
#
# This shows the CLI form of what an agent does through the MCP tools: the
# same queries, evaluated by the same engine.
Output "/out/agents.gif"
Output "/out/agents.mp4"

Source "/demo/common.tape"

# 1: the playbook
Ctrl+B
Type "1"
Sleep 1s
Type "praytel agent instructions"
Sleep 500ms
Enter
Sleep 6s

# 2: focus on the area of change
Set TypingSpeed 20ms
Ctrl+B
Type "2"
Type `clear && praytel -q 'span{ resource.service.name = "payments" && span:status = error } | matches' -- shop.test | fold -w 150`
Sleep 800ms
Enter
Sleep 7s

# 3: check the work is real
Ctrl+B
Type "3"
Type `clear && praytel -q 'span{ span:name = "Orders.Place" } > span{ span:name = "Orders.Repo.Save" && span:kind = client } | matches' -- shop.test | fold -w 150`
Sleep 800ms
Enter
Sleep 7s

# 4: MCP
Ctrl+B
Type "4"
Sleep 4s
