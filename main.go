package main

import (
	"github.com/tmsmr/xmpp-webhook/config"
	"github.com/tmsmr/xmpp-webhook/parser"

	"context"
	"crypto/tls"
	"encoding/xml"
	"io"
	"net/http"

	//"github.com/pelletier/go-toml/v2"
	"github.com/savsgio/go-logger"
	"mellium.im/sasl"
	"mellium.im/xmlstream"
	"mellium.im/xmpp"
	"mellium.im/xmpp/dial"
	"mellium.im/xmpp/jid"
	"mellium.im/xmpp/muc"
	"mellium.im/xmpp/stanza"
)

func panicOnErr(err error) {
	if err != nil {
		panic(err)
	}
}

// Custom debug io.Writer for use within xmpp.StreamConfig
type debugWriter struct{}

func (e debugWriter) Write(p []byte) (int, error) {
	line := string(p)
	logger.Debug(line)
	return len(p), nil
}

type MessageBody struct {
	stanza.Message
	Body string `xml:"body",omitempty`
}

func initXMPP(conf config.Config) (*xmpp.Session, error) {
	skipTLSVerify := !conf.VerifyTLS
	useXMPPS := conf.DirectTLS
	address := conf.JID
	pass := conf.Password
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
		logger.Errorf("Dial error to %q", address)
		return nil, err
	}
	// we need the domain in the tls config if we want to verify the cert
	if !skipTLSVerify {
		tlsConfig.ServerName = address.Domainpart()
	}

	debugOutput := debugWriter{}
	return xmpp.NewSession(
		context.TODO(),
		address.Domain(),
		address,
		conn,
		0,
		xmpp.NewNegotiator(xmpp.StreamConfig{TeeIn: debugOutput, TeeOut: debugOutput, Features: func(_ *xmpp.Session, f ...xmpp.StreamFeature) []xmpp.StreamFeature {
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

func main() {
	found, conf, err := config.FromTOMLFile("config.toml")
	// If the conf file exists, but cannot be parsed, panic!
	// If the conf file does not exist, parse from config
	if !found {
		conf = config.FromEnv()
	} else {
		if err != nil {
			panicOnErr(err)
		}
	}
	logger.Infof("Starting xmpp-webhook as %q (DirectTLS: %t, VerifyTLS: %t) to serve on %q", conf.JID, conf.DirectTLS, conf.VerifyTLS, conf.WebhookListen)

	// connect to xmpp server
	logger.Debugf("Starting XMPP session")
	xmppSession, err := initXMPP(conf)
	panicOnErr(err)
	defer closeXMPP(xmppSession)
	logger.Infof("Established XMPP session")

	// send initial presence
	panicOnErr(xmppSession.Send(context.TODO(), stanza.Presence{Type: stanza.AvailablePresence}.Wrap(nil)))
	logger.Debugf("Sent initial presence")

	// listen for messages and echo them
	go func() {
		err = xmppSession.Serve(xmpp.HandlerFunc(func(t xmlstream.TokenReadEncoder, start *xml.StartElement) error {
			d := xml.NewTokenDecoder(xmlstream.MultiReader(xmlstream.Token(*start), t))
			d.Token()

			// ignore elements that aren't messages
			if start.Name.Local != "message" {
				return nil
			}

			msg := MessageBody{}
			err = d.DecodeElement(&msg, start)
			if err != nil && err != io.EOF {
				logger.Errorf("Error decoding ChatMessage: %q", err)
				return nil
			}

			switch msg.Type {
			case stanza.ErrorMessage:
				errStanza, err := stanza.UnmarshalError(t)
				if err != nil && err != io.EOF {
					logger.Errorf("Error decoding ErrorMessage: %q", err)
				} else {
					logger.Errorf("Received error from XMPP: %q", errStanza)
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
						From: conf.JID,
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
			for recipient := conf.Recipients.Accounts.Front(); recipient != nil; recipient = recipient.Next() {
				err = xmppSession.Encode(ctx, MessageBody{
					Message: stanza.Message{
						To:   recipient.Value.(jid.JID),
						From: conf.JID,
						Type: stanza.ChatMessage,
					},
					Body: m,
				})
				if err != nil {
					logger.Errorf("Could not send notification to %q: \n%q", recipient, err)
				}
			}
			for recipient := conf.Recipients.Chatrooms.Front(); recipient != nil; recipient = recipient.Next() {
				err = xmppSession.Encode(ctx, MessageBody{
					Message: stanza.Message{
						To:   recipient.Value.(jid.JID),
						From: conf.JID,
						Type: stanza.GroupChatMessage,
					},
					Body: m,
				})
				if err != nil {
					logger.Errorf("Could not send chatroom notification to %q:\n%q", recipient.Value, err)
				}
			}
		}
	}()

	go func() {
		success := true
		logger.Infof("Joining XMPP chatrooms...")
		mucClient := &muc.Client{}
		for recipient := conf.Recipients.Chatrooms.Front(); recipient != nil; recipient = recipient.Next() {
			roomJID, _ := recipient.Value.(jid.JID).WithResource(conf.Nick)
			logger.Debugf("Joining chatroom %q", recipient.Value.(jid.JID))
			opts := []muc.Option{muc.MaxBytes(0)}
			_, err = mucClient.Join(context.TODO(), roomJID, xmppSession, opts...)
			if err != nil {
				success = false
				logger.Errorf("Could not join MUC %s: %v", roomJID, err)
			}
		}
		// TODO: Why are we never reaching this part? What produces the blocking?
		// Apparently, muc.Client.Join blocks until the server's answer is received,
		// However since we're in a difference coroutine (reusing the same XMPP session)
		// We're never receiving it. Here we should either have a dedicated session or a muxer,
		// Or we should run MUC joins from the same coroutine that established the session.
		if success {
			logger.Infof("Joined chatrooms successfully.")
		} else {
			// TODO: How to keep track of failed rooms and try them again regularly?
			logger.Warningf("Failed to join one or more chatrooms (see logs above).")
		}
	}()

	logger.Infof("Starting HTTP server")

	// initialize handlers with associated parser functions
	http.Handle("/grafana", newMessageHandler(messages, parser.GrafanaParserFunc, conf))
	//http.Handle("/slack", newMessageHandler(messages, parser.SlackParserFunc, config))
	http.Handle("/alertmanager", newMessageHandler(messages, parser.AlertmanagerParserFunc, conf))

	// So apparently in golang to handle dynamic functions you need to call http.HandleFunc
	//http.Handle("/slack/", newPlaintextSecretHandler(messages, parser.SlackParse, parser.SlackValidate, config))
	//handler := newPlaintextSecretHandler(messages, parser.SlackParse, parser.SlackValidate, config)
	// NOPE: http.HandleFunc("/slack/", handler.parserFunc.(func(http.ResponseWriter, *http.Request)))

	// Cannot use lambda function because we need config context... Or can we?
	handler := newPlaintextSecretHandler(messages, parser.SlackParse, parser.SlackValidate, conf)
	// NOTE: Don't append a trailing slash, because then the route will match but req.Body will be empty! (WTF)
	http.HandleFunc("/slack", func(w http.ResponseWriter, r *http.Request) { handler.ServeHTTP(w, r) })

	// listen for requests
	_ = http.ListenAndServe(conf.WebhookListen, nil)
}
