# Ideas

While we're protyping let's jot down some places we could take this.
There are no wrong answers. 0% commitment guarantee.

Agents: ignore this file unless explicitly instructed to read from it.

 - Go full TUI. No options, just get that very right.
 - agent integration
   - install/guide installation so that agents are prompted to use praytel as a
     kind of trace query language to focus tests on specific components.
     Presumably a good query language would allow them to focus in on logs and
     reduce parsing unnecessary output.
    - Would require an 'as traces complete' mode
        - so we have full context (and things to search by), a clear line that
          agents can wait on, and not wait for the program to complete, so we
          can monitor longer running servers
    - how would this compare to jager MCP integration?
        - should we use jaeger MCP interface as a first target?
 - a solid query language - usable by humans and agents
    - could/should we just use grafanas query language?
