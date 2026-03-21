package main

import (
	"context"
	"os"

	"etalon-agent/internal/fiscalmitsu/cli"
	"etalon-agent/internal/fiscalmitsu/protocol"
)

var AdapterVersion = "0.1.0-dev"

func main() {
	app := cli.New(AdapterVersion, protocol.NewBridge())
	os.Exit(app.Execute(context.Background(), os.Args[1:]))
}
