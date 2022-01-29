package main

import (
	"github.com/alibaba/kt-connect/pkg/proxy/dnsserver"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"os"
)

func init() {
	zerolog.SetGlobalLevel(zerolog.InfoLevel)
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})
}

func main() {
	log.Info().Msg("Shadow staring...")
	dnsserver.Start()
}
