package services

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"sync"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/secretsmanager"
)

var onceSM sync.Once
var svcSM *secretsmanager.SecretsManager
var secretName = "YOUR-SECRET-NAME"

type SecretKey struct {
	ApiKey    string `json:"apikey_movrfailover1"`
	KeyCaller string `json:"secretkey_movrfailover1"`
}

func initializeSM() {
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
	svcSM = secretsmanager.New(sess)
}

func SM() *secretsmanager.SecretsManager {
	onceSM.Do(initializeSM)
	return svcSM
}

func GetKeys() (string, string, error) {
	input := &secretsmanager.GetSecretValueInput{
		SecretId:     aws.String(secretName),
		VersionStage: aws.String("AWSCURRENT"), // VersionStage defaults to AWSCURRENT if unspecified
	}
	result, err := SM().GetSecretValue(input)
	if err != nil {
		return "", "", err
	}
	// Decrypts secret using the associated KMS CMK.
	// Depending on whether the secret is a string or binary, one of these fields will be populated.
	var secretString string
	if result.SecretString != nil {
		secretString = *result.SecretString
	} else {
		decodedBinarySecretBytes := make([]byte, base64.StdEncoding.DecodedLen(len(result.SecretBinary)))
		len, err := base64.StdEncoding.Decode(decodedBinarySecretBytes, result.SecretBinary)
		if err != nil {
			fmt.Println("Base64 Decode Error:", err)
			return "", "", err
		}
		secretString = string(decodedBinarySecretBytes[:len])
	}
	var key SecretKey
	err = json.Unmarshal([]byte(secretString), &key)
	if err != nil {
		return "", "", err
	}
	return key.ApiKey, key.KeyCaller, nil
}
