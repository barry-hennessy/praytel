def pane_of(view, name):
    return view() | tick | pane(name)
