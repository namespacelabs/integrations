package main

import (
	"flag"
	"fmt"
	"time"
)

var arg = flag.String("what", "", "What to say hi to.")

func main() {
	flag.Parse()

	for i := 0; i < 100; i++ {
		fmt.Printf("%d: Hello there %q.", i, *arg)
		time.Sleep(time.Second)
	}
}
