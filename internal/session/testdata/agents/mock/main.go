// Command mock runs the stand-in model server (internal/agentmock) until
// stopped, printing its URL, for trying an agent by hand:
//
//	go run ./internal/session/testdata/agents/mock -log /tmp/mocklog
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/gecko-term/gecko/internal/agentmock"
)

func main() {
	logDir := flag.String("log", "", "save every request here")
	flag.Parse()
	m, err := agentmock.Start(*logDir)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println(m.URL)
	select {}
}
