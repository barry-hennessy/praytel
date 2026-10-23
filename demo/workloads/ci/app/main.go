// hello is the app the ci workload's pipeline builds. It is deliberately
// tiny, so the recording shows the pipeline rather than a compile.
package main

import (
	"fmt"
	"os"
)

func main() {
	name := "world"
	if len(os.Args) > 1 {
		name = os.Args[1]
	}
	fmt.Println(Greeting(name))
}

// Greeting is what hello prints for name.
func Greeting(name string) string {
	return "hello, " + name
}
