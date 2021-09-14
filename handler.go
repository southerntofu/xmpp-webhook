package main

import (
	"github.com/savsgio/go-logger"
	"github.com/tmsmr/xmpp-webhook/config"
	"net/http"
)

// interface for parser functions
type parserFunc func(*http.Request) (string, error)

type messageHandler struct {
	messages   chan<- string // chan to xmpp client
	parserFunc parserFunc
	config     config.Config
}

// http request handler
func (h *messageHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// parse/generate message from http request
	m, err := h.parserFunc(r)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(err.Error()))
	} else {
		// send message to xmpp client
		h.messages <- m
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}
}

// returns new handler with a given parser function
func newMessageHandler(m chan<- string, f parserFunc, config config.Config) *messageHandler {
	return &messageHandler{
		messages:   m,
		parserFunc: f,
		config:     config,
	}
}

// Validates a secret against a claim. Usually done by comparing values,
// or performing a form of signature against the content.
// Arguments: secret, claim, content
type Validator func(string, string, string) bool

// Turns a HTTP request into a pipeline+claim+message, or error
type PlaintextParser func(*http.Request) (string, string, string, error)

// TODO: Make basic gitea support
// TODO: Investigate libraries to support more forge webhook providers:
//       https://github.com/go-playground/webhooks
// type ForgeParser func(*http.Request) (ForgeWebhook, string, string, error)

type PlaintextSecretHandler struct {
	messages   chan<- string // chan to xmpp client
	parserFunc PlaintextParser
	validator  Validator
	config     config.Config
}

// http request handler
func (h *PlaintextSecretHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	logger.Debugf("UseSecret: %t", h.config.WebhookUseSecret)
	// First try to parse the request
	pipeline, claim, m, err := h.parserFunc(r)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(err.Error()))
	} else {
		if !h.config.WebhookUseSecret {
			// No secrets and pipelines (for backwards-compatibility)
			// send message to xmpp client
			logger.Debugf("Skipping secret due to daemon config")
			h.messages <- m
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("ok"))
			return
		}
		// Request is parsed succesfully, check if it's valid
		// Check if the pipeline exists in config
		pipelineConfig, exists := h.config.Pipelines[pipeline]
		if exists {
			logger.Debugf("Found matching pipeline: %q", pipeline)
			// NOTE: We try the validation with every available secret, not very efficient when there's a lot
			// of secrets in a pipeline
			for secret := pipelineConfig.Secrets.Front(); secret != nil; secret = secret.Next() {
				if h.validator(secret.Value.(string), claim, m) {
					// send message to xmpp client
					h.messages <- m
					w.WriteHeader(http.StatusOK)
					_, _ = w.Write([]byte("ok"))
					// Don't try other secrets
					return
				}
			}
			// No matching secret found, 403
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte("WRONG SECRET"))
		} else {
			// The request pipeline does not exist
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte("PIPELINE NOT FOUND"))
		}
	}
}

// returns new handler with a given parser function
func newPlaintextSecretHandler(m chan<- string, f PlaintextParser, validator Validator, config config.Config) *PlaintextSecretHandler {
	return &PlaintextSecretHandler{
		messages:   m,
		parserFunc: f,
		validator:  validator,
		config:     config,
	}
}
