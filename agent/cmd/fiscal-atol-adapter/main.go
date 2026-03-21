package main

import (
	"context"
	"os"

	"etalon-agent/internal/fiscalatol/cli"
	"etalon-agent/internal/fiscalatol/libfptr"
)

var AdapterVersion = "0.1.0-dev"

func main() {
	app := cli.New(AdapterVersion, libfptr.NewBridge())
	os.Exit(app.Execute(context.Background(), os.Args[1:]))
}
