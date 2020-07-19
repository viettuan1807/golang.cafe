package server

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	stdtemplate "html/template"

	"github.com/0x13a/golang.cafe/pkg/config"
	"github.com/0x13a/golang.cafe/pkg/database"
	"github.com/0x13a/golang.cafe/pkg/email"
	"github.com/0x13a/golang.cafe/pkg/ipgeolocation"
	"github.com/0x13a/golang.cafe/pkg/middleware"
	"github.com/0x13a/golang.cafe/pkg/template"
	"github.com/aclements/go-moremath/stats"
	humanize "github.com/dustin/go-humanize"
	"github.com/gorilla/mux"
	"github.com/gorilla/sessions"

	raven "github.com/getsentry/raven-go"
)

type Server struct {
	cfg           config.Config
	Conn          *sql.DB
	router        *mux.Router
	tmpl          *template.Template
	emailClient   email.Client
	ipGeoLocation ipgeolocation.IPGeoLocation
	SessionStore  *sessions.CookieStore
}

func NewServer(
	cfg config.Config,
	conn *sql.DB,
	r *mux.Router,
	t *template.Template,
	emailClient email.Client,
	ipGeoLocation ipgeolocation.IPGeoLocation,
	sessionStore *sessions.CookieStore,
) Server {
	// todo: move somewhere else
	raven.SetDSN(cfg.SentryDSN)

	return Server{
		cfg:           cfg,
		Conn:          conn,
		router:        r,
		tmpl:          t,
		emailClient:   emailClient,
		ipGeoLocation: ipGeoLocation,
		SessionStore:  sessionStore,
	}
}

func (s Server) RegisterRoute(path string, handler func(w http.ResponseWriter, r *http.Request), methods []string) {
	s.router.HandleFunc(path, handler).Methods(methods...)
}

func (s Server) RegisterPathPrefix(path string, handler http.Handler, methods []string) {
	s.router.PathPrefix(path).Handler(handler).Methods(methods...)
}

func (s Server) StringToHTML(str string) stdtemplate.HTML {
	return s.tmpl.StringToHTML(str)
}

func (s Server) JSEscapeString(str string) string {
	return s.tmpl.JSEscapeString(str)
}

func (s Server) MarkdownToHTML(str string) stdtemplate.HTML {
	return s.tmpl.MarkdownToHTML(str)
}

func (s Server) GetConfig() config.Config {
	return s.cfg
}

func (s Server) RenderSalaryForLocation(w http.ResponseWriter, r *http.Request, location string) {
	loc, currency, country, err := database.GetLocation(s.Conn, location)
	complimentaryRemote := false
	if err != nil {
		complimentaryRemote = true
		loc = "Remote"
		currency = "$"
	}
	set, err := database.GetSalaryDataForLocationAndCurrency(s.Conn, loc, currency)
	if err != nil {
		s.Log(err, fmt.Sprintf("unable to retrieve salary stats for location %s and currency %s, err: %#v", location, currency, err))
		s.JSON(w, http.StatusInternalServerError, map[string]string{"status": "error"})
		return
	}
	trendSet, err := database.GetSalaryTrendsForLocationAndCurrency(s.Conn, loc, currency)
	if err != nil {
		s.Log(err, fmt.Sprintf("unable to retrieve salary trends for location %s and currency %s, err: %#v", location, currency, err))
		s.JSON(w, http.StatusInternalServerError, map[string]string{"status": "error"})
		return
	}
	if len(set) < 1 {
		complimentaryRemote = true
		set, err = database.GetSalaryDataForLocationAndCurrency(s.Conn, "Remote", "$")
		if err != nil {
			s.Log(err, fmt.Sprintf("unable to retrieve salary stats for location %s and currency %s, err: %#v", location, currency, err))
			s.JSON(w, http.StatusInternalServerError, map[string]string{"status": "error"})
			return
		}
		trendSet, err = database.GetSalaryTrendsForLocationAndCurrency(s.Conn, "Remote", "$")
		if err != nil {
			s.Log(err, fmt.Sprintf("unable to retrieve salary stats for location %s and currency %s, err: %#v", location, currency, err))
			s.JSON(w, http.StatusInternalServerError, map[string]string{"status": "error"})
			return
		}
	}
	jsonRes, err := json.Marshal(set)
	if err != nil {
		s.Log(err, fmt.Sprintf("unable to marshal data set %v, err: %#v", set, err))
		s.JSON(w, http.StatusInternalServerError, map[string]string{"status": "error"})
		return
	}
	jsonTrendRes, err := json.Marshal(trendSet)
	if err != nil {
		s.Log(err, fmt.Sprintf("unable to marshal data set trneds %v, err: %#v", trendSet, err))
		s.JSON(w, http.StatusInternalServerError, map[string]string{"status": "error"})
		return
	}
	var sampleMin, sampleMax stats.Sample
	for _, x := range set {
		sampleMin.Xs = append(sampleMin.Xs, float64(x.Min))
		sampleMax.Xs = append(sampleMax.Xs, float64(x.Max))
	}
	min, _ := sampleMin.Bounds()
	_, max := sampleMax.Bounds()
	min = min - 30000
	max = max + 30000
	if min < 0 {
		min = 0
	}
	s.Render(w, http.StatusOK, "salary-explorer.html", map[string]interface{}{
		"Location":            strings.ReplaceAll(location, "-", " "),
		"LocationURIEncoded":  url.QueryEscape(strings.ReplaceAll(location, "-", " ")),
		"Currency":            currency,
		"DataSet":             string(jsonRes),
		"DataSetTrends":       string(jsonTrendRes),
		"P10Max":              humanize.Comma(int64(math.Round(sampleMax.Quantile(0.1)))),
		"P10Min":              humanize.Comma(int64(math.Round(sampleMin.Quantile(0.1)))),
		"P50Max":              humanize.Comma(int64(math.Round(sampleMax.Quantile(0.5)))),
		"P50Min":              humanize.Comma(int64(math.Round(sampleMin.Quantile(0.5)))),
		"P90Max":              humanize.Comma(int64(math.Round(sampleMax.Quantile(0.9)))),
		"P90Min":              humanize.Comma(int64(math.Round(sampleMin.Quantile(0.9)))),
		"MeanMin":             humanize.Comma(int64(math.Round(sampleMin.Mean()))),
		"MeanMax":             humanize.Comma(int64(math.Round(sampleMax.Mean()))),
		"StdDevMin":           humanize.Comma(int64(math.Round(sampleMin.StdDev()))),
		"StdDevMax":           humanize.Comma(int64(math.Round(sampleMax.StdDev()))),
		"Count":               len(set),
		"Country":             country,
		"Min":                 int64(math.Round(min)),
		"Max":                 int64(math.Round(max)),
		"ComplimentaryRemote": complimentaryRemote,
		"MonthAndYear":        time.Now().UTC().Format("January 2006"),
	})
}

func (s Server) RenderPageForLocationAndTag(w http.ResponseWriter, location, tag, page, htmlView string) {
	showPage := true
	if page == "" {
		page = "1"
		showPage = false
	}
	tag = strings.TrimSpace(tag)
	location = strings.TrimSpace(location)
	reg, err := regexp.Compile("[^a-zA-Z0-9\\s]+")
	if err != nil {
		s.Log(err, "unable to compile regex (this should never happen)")
	}
	tag = reg.ReplaceAllString(tag, "")
	location = reg.ReplaceAllString(location, "")
	pageID, err := strconv.Atoi(page)
	if err != nil {
		pageID = 1
		showPage = false
	}
	var pinnedJobs []*database.JobPost
	pinnedJobs, err = database.GetPinnedJobs(s.Conn)
	if err != nil {
		s.Log(err, "unable to get pinned jobs")
	}
	jobsForPage, totalJobCount, err := database.JobsByQuery(s.Conn, location, tag, pageID, s.cfg.JobsPerPage)
	if err != nil {
		s.Log(err, "unable to get jobs by query")
		s.JSON(w, http.StatusInternalServerError, "Oops! An internal error has occurred")
		return
	}
	var complementaryRemote bool
	if len(jobsForPage) == 0 {
		complementaryRemote = true
		jobsForPage, totalJobCount, err = database.JobsByQuery(s.Conn, "Remote", tag, pageID, s.cfg.JobsPerPage)
		if len(jobsForPage) == 0 {
			jobsForPage, totalJobCount, err = database.JobsByQuery(s.Conn, "Remote", "", pageID, s.cfg.JobsPerPage)
		}
	}
	if err != nil {
		s.Log(err, "unable to retrieve jobs by query")
		s.JSON(w, http.StatusInternalServerError, "Oops! An internal error has occurred")
		return
	}
	pages := []int{}
	pageLinksPerPage := 8
	pageLinkShift := ((pageLinksPerPage / 2) + 1)
	firstPage := 1
	if pageID-pageLinkShift > 0 {
		firstPage = pageID - pageLinkShift
	}
	for i, j := firstPage, 1; i <= totalJobCount/s.cfg.JobsPerPage+1 && j <= pageLinksPerPage; i, j = i+1, j+1 {
		pages = append(pages, i)
	}
	for i, j := range jobsForPage {
		jobsForPage[i].JobDescription = string(s.tmpl.MarkdownToHTML(j.JobDescription))
		jobsForPage[i].Perks = string(s.tmpl.MarkdownToHTML(j.Perks))
		jobsForPage[i].InterviewProcess = string(s.tmpl.MarkdownToHTML(j.InterviewProcess))
		emailRe := regexp.MustCompile("^[a-zA-Z0-9.!#$%&'*+/=?^_`{|}~-]+@[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?(?:\\.[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?)*$")
		if emailRe.MatchString(j.HowToApply) {
			jobsForPage[i].IsQuickApply = true
		}
	}
	for i, j := range pinnedJobs {
		pinnedJobs[i].JobDescription = string(s.tmpl.MarkdownToHTML(j.JobDescription))
		pinnedJobs[i].Perks = string(s.tmpl.MarkdownToHTML(j.Perks))
		pinnedJobs[i].InterviewProcess = string(s.tmpl.MarkdownToHTML(j.InterviewProcess))
		emailRe := regexp.MustCompile("^[a-zA-Z0-9.!#$%&'*+/=?^_`{|}~-]+@[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?(?:\\.[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?)*$")
		if emailRe.MatchString(j.HowToApply) {
			pinnedJobs[i].IsQuickApply = true
		}
	}

	s.Render(w, http.StatusOK, htmlView, map[string]interface{}{
		"Jobs":                jobsForPage,
		"PinnedJobs":          pinnedJobs,
		"JobsMinusOne":        len(jobsForPage) - 1,
		"LocationFilter":      location,
		"TagFilter":           tag,
		"CurrentPage":         pageID,
		"ShowPage":            showPage,
		"PageSize":            s.cfg.JobsPerPage,
		"PageIndexes":         pages,
		"TotalJobCount":       totalJobCount,
		"ComplementaryRemote": complementaryRemote,
		"MonthAndYear":        time.Now().UTC().Format("January 2006"),
	})
}

func (s Server) RenderPageForLocationAndTagAdmin(w http.ResponseWriter, location, tag, page, htmlView string) {
	showPage := true
	if page == "" {
		page = "1"
		showPage = false
	}
	tag = strings.TrimSpace(tag)
	location = strings.TrimSpace(location)
	reg, err := regexp.Compile("[^a-zA-Z0-9\\s]+")
	if err != nil {
		s.Log(err, "unable to compile regex (this should never happen)")
	}
	tag = reg.ReplaceAllString(tag, "")
	location = reg.ReplaceAllString(location, "")
	pageID, err := strconv.Atoi(page)
	if err != nil {
		pageID = 1
		showPage = false
	}
	var pinnedJobs []*database.JobPost
	pinnedJobs, err = database.GetPinnedJobs(s.Conn)
	if err != nil {
		s.Log(err, "unable to get pinned jobs")
	}
	var pendingJobs []*database.JobPost
	pendingJobs, err = database.GetPendingJobs(s.Conn)
	if err != nil {
		s.Log(err, "unable to get pending jobs")
	}
	jobsForPage, totalJobCount, err := database.JobsByQuery(s.Conn, location, tag, pageID, s.cfg.JobsPerPage)
	if err != nil {
		s.Log(err, "unable to get jobs by query")
		s.JSON(w, http.StatusInternalServerError, "Oops! An internal error has occurred")
		return
	}
	var complementaryRemote bool
	if len(jobsForPage) == 0 {
		complementaryRemote = true
		jobsForPage, totalJobCount, err = database.JobsByQuery(s.Conn, "Remote", tag, pageID, s.cfg.JobsPerPage)
		if len(jobsForPage) == 0 {
			jobsForPage, totalJobCount, err = database.JobsByQuery(s.Conn, "Remote", "", pageID, s.cfg.JobsPerPage)
		}
	}
	if err != nil {
		s.Log(err, "unable to retrieve jobs by query")
		s.JSON(w, http.StatusInternalServerError, "Oops! An internal error has occurred")
		return
	}
	pages := []int{}
	pageLinksPerPage := 8
	pageLinkShift := ((pageLinksPerPage / 2) + 1)
	firstPage := 1
	if pageID-pageLinkShift > 0 {
		firstPage = pageID - pageLinkShift
	}
	for i, j := firstPage, 1; i <= totalJobCount/s.cfg.JobsPerPage+1 && j <= pageLinksPerPage; i, j = i+1, j+1 {
		pages = append(pages, i)
	}
	for i, j := range jobsForPage {
		jobsForPage[i].JobDescription = string(s.tmpl.MarkdownToHTML(j.JobDescription))
		jobsForPage[i].Perks = string(s.tmpl.MarkdownToHTML(j.Perks))
		jobsForPage[i].InterviewProcess = string(s.tmpl.MarkdownToHTML(j.InterviewProcess))
		emailRe := regexp.MustCompile("^[a-zA-Z0-9.!#$%&'*+/=?^_`{|}~-]+@[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?(?:\\.[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?)*$")
		if emailRe.MatchString(j.HowToApply) {
			jobsForPage[i].IsQuickApply = true
		}
	}
	for i, j := range pinnedJobs {
		pinnedJobs[i].JobDescription = string(s.tmpl.MarkdownToHTML(j.JobDescription))
		pinnedJobs[i].Perks = string(s.tmpl.MarkdownToHTML(j.Perks))
		pinnedJobs[i].InterviewProcess = string(s.tmpl.MarkdownToHTML(j.InterviewProcess))
		emailRe := regexp.MustCompile("^[a-zA-Z0-9.!#$%&'*+/=?^_`{|}~-]+@[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?(?:\\.[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?)*$")
		if emailRe.MatchString(j.HowToApply) {
			pinnedJobs[i].IsQuickApply = true
		}
	}

	s.Render(w, http.StatusOK, htmlView, map[string]interface{}{
		"Jobs":                jobsForPage,
		"PinnedJobs":          pinnedJobs,
		"PendingJobs":         pendingJobs,
		"JobsMinusOne":        len(jobsForPage) - 1,
		"LocationFilter":      location,
		"TagFilter":           tag,
		"CurrentPage":         pageID,
		"ShowPage":            showPage,
		"PageSize":            s.cfg.JobsPerPage,
		"PageIndexes":         pages,
		"TotalJobCount":       totalJobCount,
		"ComplementaryRemote": complementaryRemote,
		"MonthAndYear":        time.Now().UTC().Format("January 2006"),
	})
}

func (s Server) RenderPostAJobForLocation(w http.ResponseWriter, r *http.Request, location string) {
	ipAddrs := strings.Split(r.Header.Get("x-forwarded-for"), ", ")
	currency := ipgeolocation.Currency{ipgeolocation.CurrencyUSD, "$"}
	var err error
	if len(ipAddrs) > 0 {
		currency, err = s.ipGeoLocation.GetCurrencyForIP(ipAddrs[0])
		if err != nil {
			s.Log(err, fmt.Sprintf("unable to retrieve currency for ip addr %+v", ipAddrs[0]))
		}
	} else {
		s.Log(errors.New("coud not find ip address in x-forwarded-for"), "could not find ip address in x-forwarded-for, defaulting currency to USD")
	}
	s.Render(w, http.StatusOK, "post-a-job.html", map[string]interface{}{
		"Location":             location,
		"Currency":             currency,
		"StripePublishableKey": s.GetConfig().StripePublishableKey,
	})
}

type SubscribeRqMailerlite struct {
	Email  string                 `json:"email"`
	Fields map[string]interface{} `json:"fields"`
}

func (s Server) SaveSubscriber(email string) error {
	emailRe := regexp.MustCompile("^[a-zA-Z0-9.!#$%&'*+/=?^_`{|}~-]+@[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?(?:\\.[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?)*$")
	if !emailRe.MatchString(email) {
		return fmt.Errorf("invalid email format for %s", email)
	}
	mailerliteRq := &SubscribeRqMailerlite{}
	mailerliteRq.Fields = make(map[string]interface{})
	mailerliteRq.Email = email
	mailerliteRq.Fields["frequency"] = "weekly"
	jsonMailerliteRq, err := json.Marshal(mailerliteRq)
	if err != nil {
		s.Log(err, fmt.Sprintf("unable to marshal mailerliteRq %v: %v", mailerliteRq, err))
		return err
	}
	// send http request to mailerlite
	client := &http.Client{}
	req, err := http.NewRequest("POST", "https://api.mailerlite.com/api/v2/subscribers", bytes.NewBuffer(jsonMailerliteRq))
	if err != nil {
		s.Log(err, fmt.Sprintf("unable to create req for mailerlite %v: %v", jsonMailerliteRq, err))
		return err
	}
	req.Header.Add("X-MailerLite-ApiKey", s.GetConfig().MailerLiteAPIKey)
	req.Header.Add("content-type", "application/json")
	res, err := client.Do(req)
	if err != nil {
		s.Log(err, fmt.Sprintf("unable to save subscriber to mailerlite %v: %v", jsonMailerliteRq, err))
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		s.Log(errors.New("got non 200 status code from mailerlite"), fmt.Sprintf("got non 200 status code: %v req: %v", res.StatusCode, jsonMailerliteRq))
		return errors.New("got non 200 status code from mailerlite")
	}

	return nil
}

func (s Server) GetCurrencyForIP(ip string) (ipgeolocation.Currency, error) {
	return s.ipGeoLocation.GetCurrencyForIP(ip)
}

func (s Server) Render(w http.ResponseWriter, status int, htmlView string, data interface{}) error {
	return s.tmpl.Render(w, status, htmlView, data)
}

func (s Server) XML(w http.ResponseWriter, status int, data []byte) {
	w.Header().Set("Content-Type", "text/xml")
	w.WriteHeader(status)
	w.Write(data)
}

func (s Server) JSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if data != nil {
		json.NewEncoder(w).Encode(data)
	}
}

func (s Server) TEXT(w http.ResponseWriter, status int, text string) {
	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(status)
	w.Write([]byte(text))
}

func (s Server) MEDIA(w http.ResponseWriter, status int, media []byte, mediaType string) {
	w.Header().Set("Content-Type", mediaType)
	w.Header().Set("Cache-Control", "max-age=31536000")
	w.WriteHeader(status)
	w.Write(media)
}

func (s Server) Log(err error, msg string) {
	raven.CaptureErrorAndWait(err, map[string]string{"ctx": msg})
	log.Printf("%s: %+v", msg, err)
}

func (s Server) GetEmail() email.Client {
	return s.emailClient
}

func (s Server) Redirect(w http.ResponseWriter, r *http.Request, status int, dst string) {
	http.Redirect(w, r, dst, status)
}

func (s Server) Run() error {
	addr := fmt.Sprintf("0.0.0.0:%s", s.cfg.Port)
	if s.cfg.Env != "dev" {
		addr = fmt.Sprintf(":%s", s.cfg.Port)
	}
	return http.ListenAndServe(
		addr,
		middleware.HTTPSMiddleware(
			middleware.GzipMiddleware(
				middleware.HeadersMiddleware(s.router, s.cfg.Env),
			),
			s.cfg.Env,
		),
	)
}

func (s Server) GetJWTSigningKey() []byte {
	return s.cfg.JwtSigningKey
}
