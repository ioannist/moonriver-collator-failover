package main

import (
	"fmt"
	"os"

	"movrfailover/services"
)

type TelemetryMessages []interface{}

func main() {

	if len(os.Args) > 1 {
		services.ENVIR = os.Args[1]
	}

	// Get sessions, group them and store them in local map
	fmt.Println("Get sessions from db")
	sessions := []*services.Session{}
	err := services.ScanSessions(&sessions)
	if err != nil {
		panic(err)
	}

	sessionGroups := map[string][]*services.Session{}
	fmt.Println("Loaded sessions:")
	for _, session := range sessions {
		if _, ok := sessionGroups[session.GroupName]; !ok {
			sessionGroups[session.GroupName] = []*services.Session{}
		}
		sessionGroups[session.GroupName] = append(sessionGroups[session.GroupName], session)
		fmt.Printf("Loaded session: %+v\n", session.NodeName)
	}

	// Send email and SMS alerts as they are submitted to the alert queue
	go processAlerts()

	// Report status on screen every X seconds
	go reportStatus()

	// Launch a delegator for every group (network)
	// If a new group is added to the DB, then program must restart;
	// however, it's ok to add/remove sesison entries to existing groups in the DB
	for groupName, sessions := range sessionGroups {
		go delegate(groupName, sessions)
	}

	// Read messages off queue and watch for lagging nodes
	go watch()

	// Read stats from the telemetry server (little logic to keep fast)
	go readTelemetry()

	<-(chan int)(nil) // wait forever
}
