# Checks over the shop demo (demo/workloads/shop). From the repository root:
#
#   praytel -e demo/checks/shop.star -- sh -c 'cd demo/workloads/shop && go run .'
#
# praytel exits 4 if a check fails, 3 if one cannot be decided, and with the
# shop's own code if the shop fails. See docs/reference/praytel.1.md, "Checks".

orders = span(service = "web", name = "Orders.Create")

# Every order is placed, and each placed order is charged exactly once.
every(orders, has = span(name = "Orders.Place"))
every(span(name = "Orders.Place"), has = exactly(1, span(name = "Gateway.Charge")))

# Payments never reach into inventory.
never(span(service = "payments") >> span(service = "inventory"))

# The gift card is refused, and the refusal is logged.
some(span(name = "Gateway.Bank.Authorize", status = "error"))
some(log(body = contains("authorization refused")))
