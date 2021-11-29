package main

import (
	"fmt"
	"movrfailover/services"
	"sort"
	"sync"
	"time"
)

var alerts = make(chan services.PinpointMessage, 100)
var notifiedAt = map[string]int{} // time we sent an alert for groupname
var naMX sync.RWMutex             // mx for notifiedAt

func delegate(gpName string, ses []*services.Session) {
	time.Sleep(30 * time.Second) // wait to get first blocks
	for {
		time.Sleep(time.Duration(services.Config().BLOCK_CHECK_PERIOD_IN_SECONDS) * time.Second)

		sort.Slice(ses, func(i, j int) bool {
			return ses[i].Priority < ses[j].Priority
		}) // low priority to high

		maxImported := 0
		maxFinalized := 0
		onAlert := []*services.Session{}
		reassociateDo := false
		notifyDo := false
		whosActive := map[string]*WhosActive{}
		var err error

		niMX.RLock()
		nfMX.RLock()
		for _, session := range ses {
			maxImported = services.Max(nodeImported[session.NodeName], maxImported)
			maxFinalized = services.Max(nodeFinalized[session.NodeName], maxFinalized)
		}
		for _, session := range ses {
			importedLag := maxImported-nodeImported[session.NodeName] > services.Config().IMPORTED_REASSOCIATE_THRESHOLD
			finalizedLag := maxFinalized-nodeFinalized[session.NodeName] > services.Config().FINALIZED_REASSOCIATE_THRESHOLD

			if importedLag || finalizedLag {
				// if any node is lagging, we notify
				notifyDo = true
				session.NotSynced = true
				if len(whosActive) == 0 { // request active sessions only if there is an issue
					fmt.Println("Getting which sessions are active/associated")
					whosActive, err = getActiveSessions(ses)
					if err != nil {
						fmt.Printf("%v\n", err)
					}
				}
				if act, ok := whosActive[session.Session]; ok && act.Active {
					// if this is the currently associated node, we must reassociate
					onAlert = append(onAlert, session)
					reassociateDo = true
				}
			}
		}
		nfMX.RUnlock()
		niMX.RUnlock()

		if reassociateDo {
			fmt.Println("Reassociation is required (associated node found to be lagging)")
			for _, sessAlert := range onAlert { // these are active nodes that are not syncing
				// we need the nonce of the proxy account, since we are using a proxy
				// we can skip this step if we are not using proxy
				fmt.Println("Getting nonce of proxy acount")
				nonces, err := getAccountNonces([]string{sessAlert.Proxy})
				if err != nil {
					fmt.Printf("%v\n", err)
					continue
				}
				if _, ok := nonces[sessAlert.Proxy]; !ok {
					fmt.Println("Failed to find nonce!")
					continue
				}
				whosActive[sessAlert.Session].Nonce = nonces[sessAlert.Proxy]

				// find next available backup replacement
				err = failover(sessAlert, ses, &whosActive)
				if err != nil {
					fmt.Printf("%v\n", err)
					continue
				}
			}
		}

		naMX.RLock()
		notRecentlyNotified := notifiedAt[gpName] == 0 || notifiedAt[gpName] < int(time.Now().Unix())-services.Config().ALERT_CHILL_PERIOD_IN_MINUTES*60
		naMX.RUnlock()
		if notifyDo && notRecentlyNotified {
			// NOTIFY
			fmt.Println("Notifying")
			notifyMe(fmt.Sprintf(`Check %s`, gpName))
			naMX.Lock()
			notifiedAt[gpName] = int(time.Now().Unix())
			naMX.Unlock()
		}
	}
}

func notifyMe(message string) {
	pm := services.PinpointMessage{
		Subject:   "DIVNET ALERT",
		EmailHTML: "<p>" + message + "</p>",
		EmailTo:   "ioannis.tsiokos@gmail.com",
	}
	alerts <- pm
	pms := services.PinpointMessage{
		Subject:  "DIVNET ALERT",
		SMSText:  message,
		NumberTo: "+306936576875",
	}
	alerts <- pms
}

func processAlerts() {
	for alert := range alerts {
		err := services.SendPinpoint(&alert)
		if err != nil {
			fmt.Printf("%v\n", err)
		}
		fmt.Println("Alert sent")
		time.Sleep(2 * time.Second)
	}
}

func reportStatus() {
	for {
		nodeCountMX.RLock()
		fmt.Printf("Nodes: %v\n", nodeCount)
		nodeCountMX.RUnlock()

		time.Sleep(60 * time.Second)
	}
}
