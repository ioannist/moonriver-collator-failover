package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"time"

	"movrfailover/services"
)

type WhosActive struct {
	Active  bool   `json:"active"`  // is this session currently associated
	Account string `json:"account"` // public address
	Nonce   int    `json:"nonce"`   // current account nonce
}

func failover(sessAlert *services.Session, candidates []*services.Session, whosActive *map[string]WhosActive) error {
	for _, sessCandidate := range candidates {
		if !sessCandidate.NotSynced && !(*whosActive)[sessCandidate.Session].Active && sessCandidate.Priority > sessAlert.Priority && !sessAlert.Stopped {
			// Request updateAssociation
			fmt.Printf("Found reassociation candidate %s for %s\n", sessCandidate.NodeName, sessAlert.NodeName)
			fmt.Println("Extract tx from session json")
			tx, err := extractTransaction(sessAlert, sessCandidate.NodeName, (*whosActive)[sessAlert.Session].Nonce)
			if err != nil {
				fmt.Printf("%v\n", err)
				continue
			}
			if tx == "" {
				fmt.Println("Did not find a suitable transaction")
				continue
			}

			fmt.Printf("Request reassociation to %s\n", sessCandidate.NodeName)
			err = requestAssociation(tx)
			if err != nil {
				fmt.Printf("%v\n", err)
				// Ignore
			}

			message := fmt.Sprintf(`Requested reassociation from %s to %s`, sessAlert.NodeName, sessCandidate.NodeName)
			fmt.Printf("%s\n", message)
			notifyMe(message)

			isActive := false
			for i := 0; i < 6; i++ {
				time.Sleep(15 * time.Second)
				fmt.Println("Checking if reassociation was successfull...")
				whosActiveUpdated, err := getActiveSessions(candidates)
				if err != nil {
					fmt.Printf("%v\n", err)
					continue
				}
				isActive = whosActiveUpdated[sessCandidate.Session].Active
				if !isActive {
					continue
				} else {
					break
				}
			}
			if !isActive {
				fmt.Println("Session was not activated")
				continue
			}

			fmt.Println("Updating local")
			sessAlert.Stopped = true

			message = fmt.Sprintf(`Completed reassociation from %s to %s`, sessAlert.NodeName, sessCandidate.NodeName)
			fmt.Printf("%s\n", message)
			notifyMe(message)
			return nil
		}
	}
	return fmt.Errorf("Did not find reassociation candidate")
}

/**
Raw authormapping.updateAssociation transactions are signed, enrypted, and stored in the db
Every node (session) stores all possible transactions to reassociate to another node, for some nonces into the future
The job of this function is to extract the correct transaction given the old session (the one we want to switch from),
the new session (identified by the node name), and the current nonce
**/
func extractTransaction(sessionAlert *services.Session, nodeNameReassociate string, nonce int) (string, error) {
	type TX struct {
		TXs   []string `json:"txs"`
		Nonce int      `json:"nonce"`
	}
	nodenameToTransactions := map[string]TX{}
	err := json.Unmarshal([]byte(sessionAlert.Transactions), &nodenameToTransactions)
	if err != nil {
		return "", err
	}
	for nName, txNonce := range nodenameToTransactions {
		if nodeNameReassociate == nName {
			for i, tx := range txNonce.TXs {
				if nonce == txNonce.Nonce+i {
					return tx, nil
				}
			}
		}
	}
	return "", nil // did not find raw transaction; may need to run offline tx maker for new sessions
}

func requestAssociation(tx string) error {
	apiKey, keyCaller, err := services.GetKeys()
	if err != nil {
		return err
	}
	var jsonstr = []byte(fmt.Sprintf(`{"tx":"%s","keyCaller":"%s"}`, tx, keyCaller))
	err = httpPost(jsonstr, nil, apiKey, services.Config().REST_SESSION)
	if err != nil {
		return err
	}
	return nil
}

/**
This method calls a REST endpoint that accepts a list of session strings,
and returns which of these sessions are active, and the associated account
(public address) and their nonces for these active sessions
**/
func getActiveSessions(sessions []*services.Session) (map[string]WhosActive, error) {
	apiKey, _, err := services.GetKeys()
	if err != nil {
		return nil, err
	}
	type WhosActiveRequest struct {
		Sessions []string `json:"sessions"`
	}
	request := WhosActiveRequest{
		Sessions: []string{},
	}
	for _, sess := range sessions {
		request.Sessions = append(request.Sessions, sess.Session)
	}
	jsonstr, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}
	answer := map[string]WhosActive{}
	err = httpPost(jsonstr, &answer, apiKey, services.Config().REST_WHOS_SESSION)
	if err != nil {
		return nil, err
	}
	return answer, nil
}

func httpPost(jsonstr []byte, answer *map[string]WhosActive, apiKey string, endpoint string) error {
	req, err := http.NewRequest("POST", endpoint, bytes.NewBuffer(jsonstr))
	if err != nil {
		return err
	}
	req.Header.Set("X-Api-Key", apiKey)
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	fmt.Println("response Status:", resp.Status)
	// fmt.Println("response Headers:", resp.Header)
	body, _ := ioutil.ReadAll(resp.Body)
	fmt.Println("response Body:", string(body))
	if resp.Status == "200 OK" && answer != nil {
		err = json.Unmarshal(body, &answer)
	}
	return err
}
