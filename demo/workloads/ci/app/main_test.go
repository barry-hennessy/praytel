package main

import "testing"

func TestGreeting(t *testing.T) {
	if got := Greeting("dagger"); got != "hello, dagger" {
		t.Fatalf("Greeting(dagger) = %q", got)
	}
}
