package main

import (
	"github.com/JimAlex927/tusd-backend/cmd/tusd/cli"
)

func main() {
	cli.ParseFlags()
	cli.PrepareGreeting()
	cli.LoadMappingFile()

	// Print version and other information and exit if the -version flag has been
	// passed else we will start the HTTP server
	if cli.Flags.ShowVersion {
		cli.ShowVersion()
	} else {
		cli.ShowVersion()
		cli.CreateComposer()
		cli.Serve()
	}
}
