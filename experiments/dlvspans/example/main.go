package main

import (
	"fmt"
	"strings"
)

type Order struct{ Items []string }

func parse(s string) Order   { return Order{Items: strings.Split(s, ",")} }
func price(item string) int  { return len(item) * 3 }
func total(o Order) (t int) {
	for _, i := range o.Items {
		t += price(i)
	}
	return
}
func handle(s string) int { return total(parse(s)) }

func main() {
	fmt.Println(handle("apple,kiwi"))
}
