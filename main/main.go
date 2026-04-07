package main

import (
	log "github.com/sirupsen/logrus"

	"github.com/xmplusdev/xmray/main/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		log.Fatal(err)
	}
}
