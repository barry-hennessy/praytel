# Statements, a helper function and a loop: one file output per section.
def to_file(view, path):
    return view | final | file(path)

for section in ["tree", "graph"]:
    output(to_file(no_attrs() | snapshot(section), section + ".txt"))

output(stream() | each | stderr)
