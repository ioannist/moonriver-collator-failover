package services

import (
	"fmt"
	"os"
	"sync"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/dynamodb"
	"github.com/aws/aws-sdk-go/service/dynamodb/dynamodbattribute"
	"github.com/chrisxue815/realworld-aws-lambda-dynamodb-go/util"
)

var onceDB sync.Once
var svcDB *dynamodb.DynamoDB
var databaseName string
var tableName = "YOUR-TABLE-NAME"

type AWSObject = map[string]*dynamodb.AttributeValue

type Session struct {
	NodeName     string `json:"nodeName"`     // key
	GroupName    string `json:"groupName"`    // backup group
	Session      string `json:"session"`      // encrypted session key
	Priority     int    `json:"priority"`     // higher priority gets activated first
	Transactions string `json:"transactions"` // encrypted presigned raw reassociation transactions
	// not stored in DB (local)
	Stopped   bool // true if removed association automatically
	NotSynced bool
}

func initializeDB() {
	var sess *session.Session
	var err error
	if ENVIR == "dev" {
		sess, err = session.NewSession(&aws.Config{
			Region:      aws.String("eu-central-1"),
			Credentials: credentials.NewSharedCredentials("", "movrfailover"),
		})
	} else {
		sess, err = session.NewSession(&aws.Config{
			Region: aws.String("eu-central-1"),
		})
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		panic(err)
	}
	svcDB = dynamodb.New(sess)
}

// DynamoDB initializes a dynamo DB
func DynamoDB() *dynamodb.DynamoDB {
	onceDB.Do(initializeDB)
	return svcDB
}

// ScanItems paginates over the results of a scan to make it possible to return all results with one client query
func ScanItems(scanInput *dynamodb.ScanInput, offset, cap int) ([]AWSObject, error) {
	items := make([]AWSObject, 0, cap)
	resultIndex := 0

	err := DynamoDB().ScanPages(scanInput, func(page *dynamodb.ScanOutput, lastPage bool) bool {

		pageCount := len(page.Items)
		if resultIndex+pageCount > offset {
			start := util.MaxInt(0, offset-resultIndex)
			for i := start; i < pageCount; i++ {
				items = append(items, page.Items[i])
			}
		}

		resultIndex += pageCount
		return true
	})
	if err != nil {
		return items, err
	}

	return items, nil
}

// UpdateItem executes an update comand to the database
func UpdateBoolValue(tableName string, key string, keyVal string, attribute string, value bool) error {
	input := &dynamodb.UpdateItemInput{
		ExpressionAttributeValues: map[string]*dynamodb.AttributeValue{
			":r": {
				BOOL: aws.Bool(value),
			},
		},
		TableName: aws.String(tableName),
		Key: map[string]*dynamodb.AttributeValue{
			key: {
				S: aws.String(keyVal),
			},
		},
		UpdateExpression: aws.String(fmt.Sprintf("set %s = :r", attribute)),
	}
	_, err := DynamoDB().UpdateItem(input)
	return err
}

func ScanSessions(out *[]*Session) error {
	params := dynamodb.ScanInput{
		TableName: aws.String(tableName),
	}
	items, err := ScanItems(&params, 0, 1000)
	if err != nil {
		return err
	}
	err = dynamodbattribute.UnmarshalListOfMaps(items, out)
	return err
}

func UpdateSessionActive(nodeName string, active bool) error {
	err := UpdateBoolValue(tableName, "nodeName", nodeName, "active", active)
	return err
}

func UpdateSessionStopped(nodeName string, stopped bool) error {
	err := UpdateBoolValue(tableName, "nodeName", nodeName, "stopped", stopped)
	return err
}
