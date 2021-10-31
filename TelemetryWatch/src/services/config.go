package services

import (
	"fmt"
	"sync"

	"github.com/tkanos/gonfig"
)

type Configuration struct {
	HOST                            string
	TELEMETRY_ID                    string
	IMPORTED_NOTIFY_THRESHOLD       int
	FINALIZED_NOTIFY_THRESHOLD      int
	IMPORTED_REASSOCIATE_THRESHOLD  int
	FINALIZED_REASSOCIATE_THRESHOLD int
	ALERT_CHILL_PERIOD_IN_MINUTES   int
	BLOCK_CHECK_PERIOD_IN_SECONDS   int
	REST_SESSION                    string
	REST_WHOS_SESSION               string
}

var onceConf sync.Once
var ENVIR string = "prod"
var configuration Configuration

func Config() Configuration {
	onceConf.Do(loadConfig)
	return configuration
}

func loadConfig() {
	configuration = Configuration{}
	var fileName string
	if ENVIR == "dev" {
		fileName = fmt.Sprintf("./%s_config.json", ENVIR)
	} else {
		fileName = fmt.Sprintf("/home/ubuntu/%s_config.json", ENVIR)
	}
	err := gonfig.GetConf(fileName, &configuration)
	if err != nil {
		panic(err)
	}
}
