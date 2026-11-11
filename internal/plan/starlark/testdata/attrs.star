output(attrs("http.* -http.port") | snapshot("tree", logs = True) | final | stdout | exit)
