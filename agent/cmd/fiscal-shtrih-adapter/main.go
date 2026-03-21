package main

import (
	"context"
	"os"

	"etalon-agent/internal/fiscalshtrih/cli"
	"etalon-agent/internal/fiscalshtrih/drvfr"
)

var AdapterVersion = "0.1.0-dev"

func main() {
	app := cli.New(AdapterVersion, drvfr.NewBridge())
	os.Exit(app.Execute(context.Background(), os.Args[1:]))
}
