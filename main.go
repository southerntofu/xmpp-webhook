package main

import (
	"container/list"
	"context"
	"crypto/tls"
	"encoding/xml"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/tmsmr/xmpp-webhook/parser"
	"mellium.im/sasl"
	"mellium.im/xmlstream"
	"mellium.im/xmpp"
	"mellium.im/xmpp/dial"
	"mellium.im/xmpp/jid"
	"mellium.im/xmpp/muc"
	"mellium.im/xmpp/stanza"
	"mellium.im/xmpp/uri"
)

func panicOnErr(err error) {
	if err != nil {
		panic(err)
	}
}

type MessageBody struct {
	stanza.Message
	Body string `xml:"body",omitempty`
}

type Recipients struct {
	Accounts  *list.List
	Chatrooms *list.List
}

// Take a list of comma separated entries, and produce Jids from it.
// Fail hard on an unparsable entry. If the entry starts with xmpp:
// it's parsed as XMPP URI, otherwise it's parsed directly as JID
func parseRecipients(flatList string) Recipients {
	accounts := list.New()
	chatrooms := list.New()

	for _, r := range strings.Split(flatList, ",") {
		// TODO: debug
		//log.Println("Examining recipient ", r)

		if strings.HasPrefix(r, "xmpp") {
			xmppURI, err := uri.Parse(r)
			panicOnErr(err)
			switch xmppURI.Action {
			case "join":
				chatrooms.PushBack(xmppURI.ToAddr)
			case "message":
				accounts.PushBack(xmppURI.ToAddr)
			default:
				log.Fatalln("Could not parse XMPP URI: ", r, " (unknown action ", xmppURI.Action, ")")
			}
		} else {
			recipient, err := jid.Parse(r)
			panicOnErr(err)
			accounts.PushBack(recipient)
		}
	}

	return Recipients{
		Accounts:  accounts,
		Chatrooms: chatrooms,
	}
}

func initXMPP(address jid.JID, pass string, skipTLSVerify bool, useXMPPS bool) (*xmpp.Session, error) {
	tlsConfig := tls.Config{InsecureSkipVerify: skipTLSVerify}
	var dialer dial.Dialer
	// only use the tls config for the dialer if necessary
	if skipTLSVerify {
		dialer = dial.Dialer{NoTLS: !useXMPPS, TLSConfig: &tlsConfig}
	} else {
		dialer = dial.Dialer{NoTLS: !useXMPPS}
	}
	conn, err := dialer.Dial(context.TODO(), "tcp", address)
	if err != nil {
		log.Println("Dial error to ", address)
		return nil, err
	}
	// we need the domain in the tls config if we want to verify the cert
	if !skipTLSVerify {
		tlsConfig.ServerName = address.Domainpart()
	}
	return xmpp.NewSession(
		context.TODO(),
		address.Domain(),
		address,
		conn,
		0,
		xmpp.NewNegotiator(xmpp.StreamConfig{Features: func(_ *xmpp.Session, f ...xmpp.StreamFeature) []xmpp.StreamFeature {
			if f != nil {
				return f
			}
			return []xmpp.StreamFeature{
				xmpp.BindResource(),
				xmpp.StartTLS(&tlsConfig),
				xmpp.SASL("", pass, sasl.ScramSha256Plus, sasl.ScramSha256, sasl.ScramSha1Plus, sasl.ScramSha1, sasl.Plain),
			}
		}}),
	)
}

func closeXMPP(session *xmpp.Session) {
	_ = session.Close()
	_ = session.Conn().Close()
}

// Parse an environment variable "key" as boolean,
// or return "fallback" otherwise. Cannot fail.
func boolFromEnv(key string, fallback bool) bool {
	value, exists := os.LookupEnv(key)
	if !exists {
		return fallback
	}
	res, err := strconv.ParseBool(value)
	if err != nil {
		log.Println("ERROR: Failed to parse environment variable ", key, " (", value, ") as boolean, using default value (", fallback, ") instead")
		return fallback
	}
	return res
}

func main() {
	// get xmpp credentials, message recipients
	xi := os.Getenv("XMPP_ID")
	xp := os.Getenv("XMPP_PASS")
	xr := os.Getenv("XMPP_RECIPIENTS")

	skipTLSVerify := boolFromEnv("XMPP_SKIP_VERIFY", false)
	useXMPPS := boolFromEnv("XMPP_OVER_TLS", true)
	log.Println("XMPPS: ", useXMPPS, ", Skip Verification: ", skipTLSVerify)

	// get listen address
	listenAddress := os.Getenv("XMPP_WEBHOOK_LISTEN_ADDRESS")
	if len(listenAddress) == 0 {
		listenAddress = ":4321"
	}

	// check if xmpp credentials and recipient list are supplied
	if xi == "" || xp == "" || xr == "" {
		log.Fatal("XMPP_ID, XMPP_PASS or XMPP_RECIPIENTS not set")
	}

	// Recipients now contains a list of succesfully-parsed JIDs, split by type (Accounts/Chatrooms)
	recipients := parseRecipients(xr)

	myjid, err := jid.Parse(xi)
	panicOnErr(err)

	// connect to xmpp server
	log.Printf("Starting XMPP session")
	xmppSession, err := initXMPP(myjid, xp, skipTLSVerify, useXMPPS)
	panicOnErr(err)
	defer closeXMPP(xmppSession)
	log.Printf("Established session")

	// send initial presence
	panicOnErr(xmppSession.Send(context.TODO(), stanza.Presence{Type: stanza.AvailablePresence}.Wrap(nil)))
	log.Printf("Sent initial presence")

	// listen for messages and echo them
	go func() {
		err = xmppSession.Serve(xmpp.HandlerFunc(func(t xmlstream.TokenReadEncoder, start *xml.StartElement) error {
			d := xml.NewTokenDecoder(xmlstream.MultiReader(xmlstream.Token(*start), t))
			d.Token()

			// TODO: How to display raw XML from the received request?
			//raw, _ := xml.Marshal(start)
			//log.Println("  IN: ", string(raw))
			//log.Printf("  IN: %+v", start)
			log.Printf("  IN: %q", start)

			// ignore elements that aren't messages
			if start.Name.Local != "message" {
				return nil
			}

			msg := MessageBody{}
			err = d.DecodeElement(&msg, start)
			if err != nil && err != io.EOF {
				log.Printf("Error decoding message: %q", err)
				return nil
			}

			switch msg.Type {
			case stanza.ErrorMessage:
				errStanza, err := stanza.UnmarshalError(t)
				if err != nil && err != io.EOF {
					log.Printf("Error decoding message: %q", err)
				} else {
					log.Printf("Received error from XMPP: %q", errStanza)
				}
				return nil
			case stanza.ChatMessage:
				if msg.Body == "" {
					return nil
				}
				// create reply with identical contents
				reply := MessageBody{
					Message: stanza.Message{
						To:   msg.From.Bare(),
						From: myjid,
						Type: stanza.ChatMessage,
					},
					Body: msg.Body,
				}

				// try to send reply, ignore errors
				_ = t.Encode(reply)
				return nil

			default:
				return nil
			}

		}))
		panicOnErr(err)
	}()

	// create chan for messages (webhooks -> xmpp)
	messages := make(chan string)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// wait for messages from the webhooks and send them to all recipients
	go func() {
		for m := range messages {
			for recipient := recipients.Accounts.Front(); recipient != nil; recipient = recipient.Next() {
				err = xmppSession.Encode(ctx, MessageBody{
					Message: stanza.Message{
						To:   recipient.Value.(jid.JID),
						From: myjid,
						Type: stanza.ChatMessage,
					},
					Body: m,
				})
				if err != nil {
					log.Println("Received error when sending notification to account ", recipient, ": \n", err)
				}
			}
			for recipient := recipients.Chatrooms.Front(); recipient != nil; recipient = recipient.Next() {
				err = xmppSession.Encode(ctx, MessageBody{
					Message: stanza.Message{
						To:   recipient.Value.(jid.JID),
						From: myjid,
						Type: stanza.ChatMessage,
					},
					Body: m,
				})
				if err != nil {
					log.Println("Received error when sending notification to chatroom ", recipient.Value, ": \n", err)
				}
			}
		}
	}()

	go func() {
		log.Printf("Now joining XMPP chatrooms...")
		mucClient := &muc.Client{}
		//for recipient := range recipients.Chatrooms {
		for recipient := recipients.Chatrooms.Front(); recipient != nil; recipient = recipient.Next() {
			roomJID, _ := recipient.Value.(jid.JID).WithResource("webhooks")
			// TODO: debug log
			log.Println("  Joining chatroom ", recipient.Value.(jid.JID))
			opts := []muc.Option{muc.MaxBytes(0)}
			_, err = mucClient.Join(context.TODO(), roomJID, xmppSession, opts...)
			if err != nil {
				log.Fatalf("  Error joining MUC %s: %v", roomJID, err)
			}
		}
	}()

	log.Printf("Starting HTTP server")

	// initialize handlers with associated parser functions
	http.Handle("/grafana", newMessageHandler(messages, parser.GrafanaParserFunc))
	http.Handle("/slack", newMessageHandler(messages, parser.SlackParserFunc))
	http.Handle("/alertmanager", newMessageHandler(messages, parser.AlertmanagerParserFunc))

	// listen for requests
	_ = http.ListenAndServe(listenAddress, nil)
}
