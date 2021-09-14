package config

import (
	"container/list"
	"fmt"
	"os"
	"strconv"
	"strings"

	//"github.com/pelletier/go-toml/v2"
	"github.com/savsgio/go-logger"
	"mellium.im/xmpp/jid"
	"mellium.im/xmpp/uri"
)

// TODO: figure out how to **not** duplicate it without making a mess of golang modules
func panicOnErr(err error) {
    if err != nil {
        panic(err)
    }
}

type Config struct {
	LogLevel         string `omitempty`
	JID              jid.JID
	Password         string
	Nick             string `omitempty`
	DirectTLS        bool   `omitempty`
	VerifyTLS        bool   `omitempty`
	WebhookListen    string
	WebhookUseSecret bool `omitempty`
	//WebhookSecret    string `omitempty`
	Recipients Recipients
	Pipelines  map[string]Pipeline
}

type Pipeline struct {
	ID         string
	Secrets    *list.List
	Recipients Recipients
	//theme Theme
}

type Recipients struct {
	Accounts  *list.List
	Chatrooms *list.List
}

// Take a list of comma separated entries, and produce Jids from it.
// Fail hard on an unparsable entry. If the entry starts with xmpp:
// it's parsed as XMPP URI, otherwise it's parsed directly as JID
func RecipientsFromCommaSeparatedString(flatList string) Recipients {
	accounts := list.New()
	chatrooms := list.New()

	for _, r := range strings.Split(flatList, ",") {
		logger.Debugf("Examining recipient %q", r)

		if strings.HasPrefix(r, "xmpp") {
			xmppURI, err := uri.Parse(r)
			panicOnErr(err)
			switch xmppURI.Action {
			case "join":
				chatrooms.PushBack(xmppURI.ToAddr)
			case "message":
				accounts.PushBack(xmppURI.ToAddr)
			default:
				panic(fmt.Sprintf("Could not parse XMPP URI: %q (unknown action %q)", r, xmppURI.Action))
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

// Parse an environment variable "key" as boolean,
// or return "fallback" otherwise. Cannot fail.
func boolFromEnv(key string, fallback bool) bool {
	value, exists := os.LookupEnv(key)
	if !exists {
		return fallback
	}
	res, err := strconv.ParseBool(value)
	if err != nil {
		logger.Errorf("Failed to parse environment variable %q (%q) as boolean, using default value (%q) instead", key, value, fallback)
		return fallback
	}
	return res
}



func LogLevelFromString(loglevel string) string {
	if loglevel != "" {
		// Default log level is INFO
		switch strings.ToLower(loglevel) {
		case "debug":
			logger.SetLevel(logger.DEBUG)
		case "info":
			// Do nothing, already the default level
		case "warning", "warn":
			logger.SetLevel(logger.WARNING)
		case "error":
			logger.SetLevel(logger.ERROR)
		case "fatal":
			logger.SetLevel(logger.FATAL)
		default:
			panic(fmt.Sprintf("Wrong $XMPP_LOGLEVEL %q. Possible values: debug, info, warn, error, fatal", loglevel))
		}
		return loglevel
	} else {
		return "info"
	}
}

func FromEnv() Config {
	LogLevel := LogLevelFromString(os.Getenv("XMPP_LOGLEVEL"))

	xi := os.Getenv("XMPP_ID")
	xp := os.Getenv("XMPP_PASS")
	xr := os.Getenv("XMPP_RECIPIENTS")

	// check if xmpp credentials and recipient list are supplied
	if xi == "" || xp == "" || xr == "" {
		logger.Fatal("XMPP_ID, XMPP_PASS or XMPP_RECIPIENTS not set")
	}

	JID, err := jid.Parse(xi)
	panicOnErr(err)

	Nick := os.Getenv("XMPP_NICK")
	if Nick == "" {
		Nick = "webhooks"
	}

	VerifyTLS := !boolFromEnv("XMPP_SKIP_VERIFY", false)
	DirectTLS := boolFromEnv("XMPP_OVER_TLS", true)

	// get listen address
	Listen := os.Getenv("XMPP_WEBHOOK_LISTEN_ADDRESS")
	if len(Listen) == 0 {
		Listen = ":4321"
	}

	// Recipients now contains a list of succesfully-parsed JIDs, split by type (Accounts/Chatrooms)
	Recipients := RecipientsFromCommaSeparatedString(xr)

	// If there is no secret, disable using secrets for backwards-compatibility
	Secret := os.Getenv("XMPP_WEBHOOK_SECRET")
	UseSecret := len(Secret) > 0

	if !UseSecret {
		logger.Warningf("Your webhooks endpoint is not protected by secrets. If it is publicly exposed on the Internet, you SHOULD set $XMPP_WEBHOOK_SECRET to the secret value used by your webhook client!")
	}

	Pipelines := make(map[string]Pipeline)
	mainPipelineSecrets := list.New()
	mainPipelineSecrets.PushBack(Secret)
	Pipelines["__main"] = Pipeline{
		ID:         "__main",
		Secrets:    mainPipelineSecrets,
		Recipients: Recipients,
	}

	return Config{
		LogLevel:         LogLevel,
		JID:              JID,
		Password:         xp,
		Nick:             Nick,
		DirectTLS:        DirectTLS,
		VerifyTLS:        VerifyTLS,
		WebhookListen:    Listen,
		WebhookUseSecret: UseSecret,
		//WebhookSecret:    Secret,
		// TODO: Move Recipients to Pipelines
		Recipients: Recipients,
		Pipelines:  Pipelines,
	}
}
