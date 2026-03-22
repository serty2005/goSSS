package main

import (
	"context"
	"os"

	"etalon-agent/internal/iikosyrverms/cli"
	"etalon-agent/internal/iikosyrverms/service"
)

var AdapterVersion = "0.1.0-dev"

func main() {
	app := cli.New(AdapterVersion, service.New())
	os.Exit(app.Execute(context.Background(), os.Args[1:]))
}
