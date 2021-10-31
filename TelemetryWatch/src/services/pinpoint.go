package services

import (
	"errors"
	"fmt"
	"os"
	"sync"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/pinpoint"
	"github.com/k3a/html2text"
)

type PinpointMessage struct {
	Subject   string
	EmailHTML string
	SMSText   string
	EmailTo   string
	NumberTo  string
}

const fromEmail = "your-email@email.com"

var oncePP sync.Once
var ppSvc *pinpoint.Pinpoint

const ppAppID = "YOUR-PINPOINT-APP-ID"

func initializePP() {
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
	ppSvc = pinpoint.New(sess)
}

func PPSvc() *pinpoint.Pinpoint {
	oncePP.Do(initializePP)
	return ppSvc
}

func SendPinpoint(msg *PinpointMessage) error {
	if msg.EmailTo == "" && msg.NumberTo == "" {
		return errors.New("No email and no phone number")
	}

	msgRequest := pinpoint.MessageRequest{
		Addresses: map[string]*pinpoint.AddressConfiguration{},
		MessageConfiguration: &pinpoint.DirectMessageConfiguration{
			DefaultMessage: &pinpoint.DefaultMessage{
				Body: aws.String(msg.EmailHTML),
			},
		},
	}

	if msg.EmailTo != "" {
		msgRequest.Addresses[msg.EmailTo] = &pinpoint.AddressConfiguration{
			ChannelType: aws.String(pinpoint.ChannelTypeEmail),
		}
		msgRequest.MessageConfiguration.EmailMessage = &pinpoint.EmailMessage{
			FromAddress: aws.String(fromEmail),
			SimpleEmail: &pinpoint.SimpleEmail{
				HtmlPart: &pinpoint.SimpleEmailPart{
					Charset: aws.String("UTF-8"),
					Data:    &msg.EmailHTML,
				},
				Subject: &pinpoint.SimpleEmailPart{
					Charset: aws.String("UTF-8"),
					Data:    aws.String(msg.Subject),
				},
				TextPart: &pinpoint.SimpleEmailPart{
					Charset: aws.String("UTF-8"),
					Data:    aws.String(html2text.HTML2Text(msg.EmailHTML)),
				},
			},
		}
	}

	if msg.NumberTo != "" {
		msgRequest.Addresses[msg.NumberTo] = &pinpoint.AddressConfiguration{
			ChannelType: aws.String(pinpoint.ChannelTypeSms),
		}
		msgRequest.MessageConfiguration.SMSMessage = &pinpoint.SMSMessage{
			Body: aws.String(msg.SMSText),
		}
	}

	sendMsg := pinpoint.SendMessagesInput{
		ApplicationId:  aws.String(ppAppID),
		MessageRequest: &msgRequest,
	}
	// fmt.Printf("%+v\n", sendMsg)
	output, err := PPSvc().SendMessages(&sendMsg)
	if err != nil {
		return err
	}

	results := output.MessageResponse.EndpointResult
	errs := ""
	for _, result := range results {
		r := *result.DeliveryStatus
		fmt.Printf("Pinpoint result: %v", result)
		if r != "SUCCESSFULL" && r != "OPT_OUT" && r != "DUPLICATE" {
			errs = errs + ", " + *result.DeliveryStatus
		}
	}
	if errs != "" {
		return errors.New(errs)
	}
	return nil
}
