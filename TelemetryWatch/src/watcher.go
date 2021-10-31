package main

import (
	"encoding/json"
	"fmt"
	"log"
	"movrfailover/services"
	"sync"
	"time"

	retry "github.com/avast/retry-go"
	websocket "github.com/gorilla/websocket"
)

var chanMessages = make(chan []byte, 10000) // Buffered channel to account for bursts or spikes in data

var nodeCount = 0
var nodeCountMX sync.RWMutex
var nodeFinalized = map[string]int{} // nodeName -> last finalized block
var nodeImported = map[string]int{}  // nodeName -> last imported block
var nfMX sync.RWMutex                // mx for nodeFinalized
var niMX sync.RWMutex                // mx for nodeImported

func readTelemetry() {
	ws, err := wsConnect(services.Config().HOST, services.Config().TELEMETRY_ID)
	if err != nil {
		panic(err)
	}
	for {
		var msg []byte
		// err := c.ReadJSON(&msg)
		// Ideally use c.ReadMessage instead of ReadJSON so you can parse the JSON data in a
		// separate go routine. Any processing done in this loop increases the chances of disconnects
		// due to not consuming the data fast enough.
		_, msg, err := ws.ReadMessage()
		if err != nil {
			fmt.Println("Error Reading Message from socket")
			fmt.Printf("%v", err)

			nodeCountMX.Lock()
			nodeCount = 0
			nodeCountMX.Unlock()

			ws, err = wsConnect(services.Config().HOST, services.Config().TELEMETRY_ID)
			if err != nil {
				fmt.Printf("%v\n", err)
				continue
			}
			continue
		}
		chanMessages <- msg
	}
}

func watch() {
	fmt.Println("Starting aggregation thread")
	nodeIDs := map[int]string{} // ID to hash
	for msgBytes := range chanMessages {
		var messages TelemetryMessages
		if err := json.Unmarshal(msgBytes, &messages); err != nil {
			fmt.Printf("%v\n", err)
			continue
		}

		// fmt.Printf("%+v\n\n", messages)

		for i := 0; i < len(messages)-1; i = i + 2 {
			message := messages[i : i+2]

			command, ok := message[0].(float64)
			if !ok {
				continue
			}

			// AddedNode: 0x03 as 0x03,
			if command == 3 {
				data, ok := message[1].([]interface{})
				if !ok {
					continue
				}
				nodeID, ok := data[0].(float64)
				if !ok {
					continue
				}
				nodeDetails, ok := data[1].([]interface{})
				if !ok {
					continue
				}
				nodeName, ok := nodeDetails[0].(string)
				if !ok {
					continue
				}
				nodeIDs[int(nodeID)] = nodeName
				nodeCountMX.Lock()
				nodeCount++
				nodeCountMX.Unlock()
				continue
			}

			// RemovedNode: 0x04 as 0x04,
			if command == 4 {
				nodeID, ok := message[1].(float64)
				if !ok {
					continue
				}
				nodeIDs[int(nodeID)] = ""
				nodeCountMX.Lock()
				nodeCount--
				nodeCountMX.Unlock()
				continue
			}

			// ImportedBlock: 0x06 as 0x06,
			if command == 6 {
				data, ok := message[1].([]interface{})
				if !ok {
					continue
				}
				nodeID, ok := data[0].(float64)
				if !ok {
					continue
				}
				nodeDetails, ok := data[1].([]interface{})
				if !ok {
					continue
				}
				importedBlock, ok := nodeDetails[0].(float64)
				if !ok {
					continue
				}
				// kmsg[3] = nodeIDs[int(nodeID)]
				// kmsg[4] = strconv.Itoa(int(importedBlock))
				nodeName := nodeIDs[int(nodeID)]
				niMX.Lock()
				if nodeImported[nodeName] < int(importedBlock) {
					nodeImported[nodeName] = int(importedBlock)
				}
				niMX.Unlock()
				//fmt.Printf("ImportedBlock: %v for %v\n", importedBlock, nodeName)
			}

			// FinalizedBlock: 0x07 as 0x07,
			if command == 7 {
				data, ok := message[1].([]interface{})
				if !ok {
					continue
				}
				nodeID, ok := data[0].(float64)
				if !ok {
					continue
				}
				finalizedBlock, ok := data[1].(float64)
				if !ok {
					continue
				}
				// kmsg[3] = nodeIDs[int(nodeID)]
				// kmsg[5] = strconv.Itoa(int(finalizedBlock))
				nodeName := nodeIDs[int(nodeID)]
				nfMX.Lock()
				if nodeFinalized[nodeName] < int(finalizedBlock) {
					nodeFinalized[nodeName] = int(finalizedBlock)
				}
				nfMX.Unlock()
				//fmt.Printf("FinalizedBlock: %v for %s\n", finalizedBlock, nodeName)
			}

		}
	}
}

func wsConnect(host string, telemetryID string) (*websocket.Conn, error) {
	var ws *websocket.Conn
	err := retry.Do(
		func() error {
			var err error

			if ws != nil {
				fmt.Println("Closing socket to reconnect")
				ws.Close()
			}
			fmt.Println("Open socket")
			endPoint := fmt.Sprintf("ws://%s/feed", host)
			ws, _, err = websocket.DefaultDialer.Dial(endPoint, nil)
			if err != nil {
				fmt.Printf("%v\n", err)
				return err
			}
			// defer ws.Close() // cannot use inside retry.Do func
			err = ws.WriteMessage(websocket.TextMessage, []byte(fmt.Sprintf("subscribe:%s", telemetryID)))
			if err != nil {
				fmt.Printf("%v\n", err)
				return err
			}
			fmt.Println("Socket connected")
			return nil
		},
		retry.OnRetry(func(n uint, err error) {
			log.Printf("#%d: %s\n", n, err)
		}),
		retry.Attempts(1000000),
		retry.Delay(3*time.Second),
	)
	return ws, err
}
