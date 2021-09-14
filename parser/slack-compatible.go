package parser

import (
	"encoding/json"
	"errors"
	"io/ioutil"
	"net/http"
    "strings"
    "github.com/savsgio/go-logger"
)

func SlackParserFunc(r *http.Request) (string, error) {
	// get alert data from request
	body, err := ioutil.ReadAll(r.Body)
	if err != nil {
		return "", errors.New(readErr)
	}

	alert := struct {
		Text        string `json:"text"`
		Attachments []struct {
			Title     string `json:"title"`
			TitleLink string `json:"title_link"`
			Text      string `json:"text"`
		} `json:"attachments"`
	}{}

	// parse body into the alert struct
	err = json.Unmarshal(body, &alert)
	if err != nil {
		return "", errors.New(parseErr)
	}

	// construct alert message
	message := alert.Text
	for _, attachment := range alert.Attachments {
		if len(message) > 0 {
			message = message + "\n"
		}
		message += attachment.Title + "\n"
		message += attachment.TitleLink + "\n\n"
		message += attachment.Text
	}

	return message, nil
}

// Return pipeline, claim, message, error
// TODO: Why the hell is req.Body empty when calling /slack but populated when calling /slack/
func SlackParse(req *http.Request) (string, string, string, error) {
    logger.Infof("URL: %q", req.URL.Path)

    // Simple secret from URL
    path := strings.TrimPrefix(req.URL.Path, "/slack/")
    p := strings.Split(path, "/")
    pipeline := ""
    claim := ""
    if len(p) >= 1 {
        pipeline = p[0]
    }
    if len(p) >= 2 {
        claim = p[1]
    }

    logger.Infof("Pipeline: %q, Claim: %q", pipeline, claim)

	// get alert data from request
	body, err := ioutil.ReadAll(req.Body)
	if err != nil {
		return "", "", "", errors.New(readErr)
	}

	alert := struct {
		Text        string `json:"text"`
		Attachments []struct {
			Title     string `json:"title"`
			TitleLink string `json:"title_link"`
			Text      string `json:"text"`
		} `json:"attachments"`
	}{}

	// parse body into the alert struct
	err = json.Unmarshal(body, &alert)
	if err != nil {
        logger.Debugf("Could not parse body:\n%q", body)
		return "", "", "", errors.New(parseErr)
	}

	// construct alert message
	message := alert.Text
	for _, attachment := range alert.Attachments {
		if len(message) > 0 {
			message = message + "\n"
		}
		message += attachment.Title + "\n"
		message += attachment.TitleLink + "\n\n"
		message += attachment.Text
	}

	return pipeline, claim, message, nil

}

func SlackValidate(secret string, claim string, content string) bool {
    // On slack, we only check if the secret provided in URL is correct (no signatures)
    return secret == claim
}
