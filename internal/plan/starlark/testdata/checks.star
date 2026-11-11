# The checks of design/PIPELINE.md, "Checks" and "Table tests".
checkout = span(service = "checkout")
drop = span(attrs = {"db.statement": match(".*DROP.*")})

never(checkout >> drop)
every(span(kind = "server"), has = span(name = "auth"))
every(span(kind = "server"), has = at_most(3, span(name = "retry")))
exactly(1, span(name = "migrate"), name = "one migration")
some(log(source = "stderr"))

def f(x):
    return x * 2

for x in [0, 1, 2]:
    every(span(attrs = {"test.name": "TestDouble", "input": x}),
          has = span(attrs = {"output": f(x)}))
