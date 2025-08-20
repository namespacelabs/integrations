package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"

	"github.com/google/shlex"
)

var x = flag.String("cmd", "", "The command to start.")

func main() {
	flag.Parse()

	fmt.Fprintf(os.Stderr, "Hello from the sidecar, starting instance...")

	if err := (func() error {
		parsed, err := shlex.Split(*x)
		if err != nil {
			return err
		}

		cmd := exec.Command(parsed[0], parsed[1:]...)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		return cmd.Run()
	})(); err != nil {
		log.Fatal(err)
	}
}
