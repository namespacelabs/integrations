package main

import (
	"bufio"
	"bytes"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
)

var tokenFile = flag.String("f", "", "The path of the file where the token value lives.")

func main() {
	flag.Parse()

	if *tokenFile == "" {
		log.Fatal("-f is required")
	}

	tokenData, err := os.ReadFile(*tokenFile)
	if err != nil {
		log.Fatal(err)
	}

	switch flag.Arg(0) {
	case "get":
		scanner := bufio.NewScanner(os.Stdin)
		attributes := map[string]string{}
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" {
				break
			}

			parts := strings.SplitN(line, "=", 2)
			if len(parts) == 2 {
				attributes[parts[0]] = parts[1]
			}
		}

		if attributes["protocol"] == "https" && attributes["host"] == "github.com" {
			fmt.Fprintf(os.Stdout, "username=token\n")
			fmt.Fprintf(os.Stdout, "password=%s\n", bytes.TrimSpace(tokenData))
			// (don’t forget the blank line at the end; it tells git credential that the application finished feeding all the information it has)
			fmt.Fprintln(os.Stdout)
		}
	}

	os.Exit(0)
}
