package handler

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/0x13a/golang.cafe/pkg/email"
	"github.com/0x13a/golang.cafe/pkg/server"
)

type SubscribeRqMailerlite struct {
	Email  string                 `json:"email"`
	Fields map[string]interface{} `json:"fields"`
}

func ViewNewsletterPageHandler(svr server.Server) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		svr.RenderPageForLocationAndTag(w, "", "", "", "newsletter.html")
	}
}

func ViewShopPageHandler(svr server.Server) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		svr.RenderPageForLocationAndTag(w, "", "", "", "shop.html")
	}
}

func ViewCommunityNewsletterPageHandler(svr server.Server) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		svr.RenderPageForLocationAndTag(w, "", "", "", "news.html")
	}
}

func ViewSlackPageHandler(svr server.Server) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		svr.RenderPageForLocationAndTag(w, "", "", "", "slack.html")
	}
}

func ViewSupportPageHandler(svr server.Server) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		svr.RenderPageForLocationAndTag(w, "", "", "", "support.html")
	}
}

func SaveMemberToCommunityNewsletterPageHandler(svr server.Server) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		email := strings.ToLower(r.URL.Query().Get("email"))
		emailRe := regexp.MustCompile("^[a-zA-Z0-9.!#$%&'*+/=?^_`{|}~-]+@[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?(?:\\.[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?)*$")
		if !emailRe.MatchString(email) {
			svr.JSON(w, http.StatusBadRequest, nil)
			return
		}
		mailerliteRq := &SubscribeRqMailerlite{}
		mailerliteRq.Fields = make(map[string]interface{})
		mailerliteRq.Email = email
		mailerliteRq.Fields["community_type"] = "slack"
		jsonMailerliteRq, err := json.Marshal(mailerliteRq)
		if err != nil {
			svr.Log(err, fmt.Sprintf("unable to marshal mailerliteRq %v: %v", mailerliteRq, err))
			svr.JSON(w, http.StatusBadRequest, nil)
			return
		}
		// send http request to mailerlite
		client := &http.Client{}
		req, err := http.NewRequest("POST", "https://api.mailerlite.com/api/v2/subscribers", bytes.NewBuffer(jsonMailerliteRq))
		if err != nil {
			svr.Log(err, fmt.Sprintf("unable to create req for mailerlite %v: %v", jsonMailerliteRq, err))
			svr.JSON(w, http.StatusInternalServerError, nil)
			return
		}
		req.Header.Add("X-MailerLite-ApiKey", svr.GetConfig().MailerLiteAPIKey)
		req.Header.Add("content-type", "application/json")
		res, err := client.Do(req)
		if err != nil {
			svr.Log(err, fmt.Sprintf("unable to save subscriber to mailerlite %v: %v", jsonMailerliteRq, err))
			svr.JSON(w, http.StatusInternalServerError, nil)
			return
		}
		defer res.Body.Close()
		if res.StatusCode != http.StatusOK {
			svr.Log(errors.New("got non 200 status code from mailerlite"), fmt.Sprintf("got non 200 status code: %v req: %v", res.StatusCode, jsonMailerliteRq))
			svr.JSON(w, http.StatusInternalServerError, nil)
			return
		}
		svr.JSON(w, http.StatusOK, nil)
	}
}

func SendSlackInviteLink(svr server.Server) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		emailAddr := strings.ToLower(r.URL.Query().Get("email"))
		emailRe := regexp.MustCompile("^[a-zA-Z0-9.!#$%&'*+/=?^_`{|}~-]+@[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?(?:\\.[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?)*$")
		if !emailRe.MatchString(emailAddr) {
			svr.JSON(w, http.StatusBadRequest, nil)
			return
		}

		if err := svr.GetEmail().SendEmail("Diego from Golang Cafe <team@golang.cafe>", emailAddr, email.GolangCafeEmailAddress, "Your invite to Golang Cafe Slack", fmt.Sprintf("Thanks for your interest. Here's your invite for Golang Cafe Slack, please follow or copy on your URL bar the link below. %s", svr.GetConfig().SlackInviteURL)); err != nil {
			svr.JSON(w, http.StatusInternalServerError, nil)
			return
		}
		svr.JSON(w, http.StatusOK, nil)
	}
}
func SaveMemberToNewsletterPageHandler(svr server.Server) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		email := strings.ToLower(r.URL.Query().Get("email"))
		if err := svr.SaveSubscriber(email); err != nil {
			svr.JSON(w, http.StatusBadRequest, nil)
			return
		}
		svr.JSON(w, http.StatusOK, nil)
	}
}
