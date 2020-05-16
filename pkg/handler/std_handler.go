package handler

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/0x13a/golang.cafe/pkg/database"
	"github.com/0x13a/golang.cafe/pkg/email"
	"github.com/0x13a/golang.cafe/pkg/middleware"
	"github.com/0x13a/golang.cafe/pkg/server"
	jwt "github.com/dgrijalva/jwt-go"
	"github.com/gorilla/mux"
	"github.com/segmentio/ksuid"
)

func IndexPageHandler(svr server.Server) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		location := r.URL.Query().Get("l")
		tag := r.URL.Query().Get("t")
		page := r.URL.Query().Get("p")

		var dst string
		if location != "" && tag != "" {
			dst = fmt.Sprintf("/Golang-%s-Jobs-In-%s", tag, location)
		} else if location != "" {
			dst = fmt.Sprintf("/Golang-Jobs-In-%s", location)
		} else if tag != "" {
			dst = fmt.Sprintf("/Golang-%s-Jobs", tag)
		}
		if dst != "" && page != "" {
			dst += fmt.Sprintf("?p=%s", page)
		}
		if dst != "" {
			svr.Redirect(w, r, http.StatusMovedPermanently, dst)
		}

		svr.RenderPageForLocationAndTag(w, "", "", page, "landing.html")
	}
}

func PermanentRedirectHandler(svr server.Server, dst string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		svr.Redirect(w, r, http.StatusMovedPermanently, fmt.Sprintf("https://golang.cafe/%s", dst))
	}
}

func PostAJobPageHandler(svr server.Server) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		svr.RenderPostAJobForLocation(w, r, "")
	}
}

func PostAJobWithoutPaymentPageHandler(svr server.Server) http.HandlerFunc {
	return middleware.AdminAuthenticatedMiddleware(
		svr.SessionStore,
		svr.GetJWTSigningKey(),
		func(w http.ResponseWriter, r *http.Request) {
			svr.Render(w, http.StatusOK, "post-a-job-without-payment.html", nil)
		},
	)
}

func RequestTokenSignOn(svr server.Server) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		req := &struct {
			Email string `json:"email"`
		}{}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			svr.JSON(w, http.StatusBadRequest, nil)
			return
		}
		emailRe := regexp.MustCompile("^[a-zA-Z0-9.!#$%&'*+/=?^_`{|}~-]+@[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?(?:\\.[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?)*$")
		if !emailRe.MatchString(req.Email) {
			svr.JSON(w, http.StatusBadRequest, nil)
			return
		}
		k, err := ksuid.NewRandom()
		if err != nil {
			svr.Log(err, "unable to generate token")
			svr.JSON(w, http.StatusBadRequest, nil)
			return
		}
		err = database.SaveTokenSignOn(svr.Conn, req.Email, k.String())
		if err != nil {
			svr.Log(err, "unable to save sign on token")
			svr.JSON(w, http.StatusBadRequest, nil)
			return
		}
		err = svr.GetEmail().SendEmail("Diego from Golang Cafe <team@golang.cafe>", req.Email, email.GolangCafeEmailAddress, "Sign On on Golang Cafe", fmt.Sprintf("Sign On on Golang Cafe https://golang.cafe/x/auth/%s", k.String()))
		if err != nil {
			svr.Log(err, "unable to send email while applying to job")
			svr.JSON(w, http.StatusBadRequest, nil)
			return
		}
		svr.JSON(w, http.StatusOK, nil)
	}
}

func VerifyTokenSignOn(svr server.Server, adminEmail string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		token := vars["token"]
		user, err := database.ValidateSignOnToken(svr.Conn, token)
		if err != nil {
			svr.Log(err, fmt.Sprintf("unable to validate signon token %s", token))
			svr.TEXT(w, http.StatusBadRequest, "Invalid or expired token")
			return
		}
		sess, err := svr.SessionStore.Get(r, "____gc")
		if err != nil {
			svr.TEXT(w, http.StatusInternalServerError, "Invalid or expired token")
			return
		}
		stdClaims := &jwt.StandardClaims{
			ExpiresAt: time.Now().Add(30 * 24 * time.Hour).UTC().Unix(),
			IssuedAt:  time.Now().UTC().Unix(),
			Issuer:    "https://golang.cafe",
		}
		claims := middleware.MyCustomClaims{
			Username:       user.Username,
			UserID:         user.ID,
			IsAdmin:        user.Email == adminEmail,
			StandardClaims: *stdClaims,
		}
		tkn := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
		ss, err := tkn.SignedString(svr.GetJWTSigningKey())
		sess.Values["jwt"] = ss
		err = sess.Save(r, w)
		if err != nil {
			svr.JSON(w, http.StatusInternalServerError, nil)
			return
		}
		if claims.IsAdmin {
			svr.Redirect(w, r, http.StatusMovedPermanently, "/manage/list")
			return
		}
		svr.Redirect(w, r, http.StatusMovedPermanently, "/news")
	}
}

func PostNewsItem(svr server.Server) http.HandlerFunc {
	return middleware.UserAuthenticatedMiddleware(
		svr.SessionStore,
		svr.GetJWTSigningKey(),
		func(w http.ResponseWriter, r *http.Request) {
			decoder := json.NewDecoder(r.Body)
			newsItem := database.NewsItem{}
			if err := decoder.Decode(&newsItem); err != nil {
				svr.JSON(w, http.StatusBadRequest, nil)
				return
			}
			sess, err := svr.SessionStore.Get(r, "____gc")
			if err != nil {
				svr.JSON(w, http.StatusInternalServerError, nil)
				return
			}
			tk, ok := sess.Values["jwt"].(string)
			if !ok {
				svr.JSON(w, http.StatusUnauthorized, nil)
				return
			}
			token, err := jwt.ParseWithClaims(tk, &middleware.MyCustomClaims{}, func(token *jwt.Token) (interface{}, error) {
				return svr.GetJWTSigningKey(), nil
			})
			if !token.Valid {
				svr.JSON(w, http.StatusUnauthorized, nil)
				return
			}
			claims, ok := token.Claims.(*middleware.MyCustomClaims)
			if !ok {
				svr.JSON(w, http.StatusUnauthorized, nil)
				return
			}
			newsItem.CreatedBy = database.User{ID: claims.UserID, Username: claims.Username, Email: claims.Email, CreatedAt: claims.CreatedAt}
			if err := database.CreateNewsItem(svr.Conn, newsItem); err != nil {
				svr.JSON(w, http.StatusBadRequest, nil)
				return
			}
			svr.JSON(w, http.StatusOK, "success")
		},
	)
}

func PostNewsComment(svr server.Server) http.HandlerFunc {
	return middleware.UserAuthenticatedMiddleware(
		svr.SessionStore,
		svr.GetJWTSigningKey(),
		func(w http.ResponseWriter, r *http.Request) {
			decoder := json.NewDecoder(r.Body)
			newsComment := database.NewsComment{}
			if err := decoder.Decode(&newsComment); err != nil {
				svr.Log(err, "unable to decode request")
				svr.JSON(w, http.StatusBadRequest, nil)
				return
			}
			sess, err := svr.SessionStore.Get(r, "____gc")
			if err != nil {
				svr.Log(err, "unable to retrieve session")
				svr.JSON(w, http.StatusInternalServerError, nil)
				return
			}
			tk, ok := sess.Values["jwt"].(string)
			if !ok {
				svr.Log(err, "unable to decode jwt from session")
				svr.JSON(w, http.StatusUnauthorized, nil)
				return
			}
			token, err := jwt.ParseWithClaims(tk, &middleware.MyCustomClaims{}, func(token *jwt.Token) (interface{}, error) {
				return svr.GetJWTSigningKey(), nil
			})
			if !token.Valid {
				svr.Log(err, "unable to validate jwt token")
				svr.JSON(w, http.StatusUnauthorized, nil)
				return
			}
			claims, ok := token.Claims.(*middleware.MyCustomClaims)
			if !ok {
				svr.Log(err, "unable to parse jwt claims")
				svr.JSON(w, http.StatusUnauthorized, nil)
				return
			}
			newsComment.CreatedBy = database.User{ID: claims.UserID, Username: claims.Username, Email: claims.Email, CreatedAt: claims.CreatedAt}
			if err := database.CreateNewsComment(svr.Conn, newsComment); err != nil {
				svr.Log(err, "unable to save news comment into db")
				svr.JSON(w, http.StatusBadRequest, nil)
				return
			}
			svr.JSON(w, http.StatusOK, "success")
		},
	)
}

func ListNewsItems(svr server.Server) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// list latest news items
		news, err := database.GetLatestNews(svr.Conn, 10)
		if err != nil {
			svr.Log(err, "unable to retrieve latest news")
			svr.JSON(w, http.StatusInternalServerError, nil)
			return
		}
		svr.Render(w, http.StatusOK, "news.html", map[string]interface{}{
			"News":         news,
			"IsSignedOn":   middleware.IsSignedOn(r, svr.SessionStore, svr.GetJWTSigningKey()),
			"MonthAndYear": time.Now().UTC().Format("January 2006"),
		})
	}
}

func ListNewsComments(svr server.Server) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		newsID := vars["id"]
		news, err := database.GetNewsByID(svr.Conn, newsID)
		if err != nil {
			svr.Log(err, "unable to retrieve latest news")
			svr.JSON(w, http.StatusInternalServerError, nil)
			return
		}
		comments, err := database.GetNewsComments(svr.Conn, newsID)
		if err != nil {
			svr.Log(err, "unable to retrieve latest news")
			svr.JSON(w, http.StatusInternalServerError, nil)
			return
		}
		svr.Render(w, http.StatusOK, "news_comments.html", map[string]interface{}{
			"ID":                 news.ID,
			"Title":              news.Title,
			"Text":               svr.MarkdownToHTML(news.Text),
			"CreatedAtHumanised": news.CreatedAtHumanised,
			"CreatedBy":          news.CreatedBy,
			"Comments":           comments,
			"TotalJobCount":      len(comments),
			"IsSignedOn":         middleware.IsSignedOn(r, svr.SessionStore, svr.GetJWTSigningKey()),
			"MonthAndYear":       time.Now().UTC().Format("January 2006"),
		})
	}
}

func ListJobsAsAdminPageHandler(svr server.Server) http.HandlerFunc {
	return middleware.AdminAuthenticatedMiddleware(
		svr.SessionStore,
		svr.GetJWTSigningKey(),
		func(w http.ResponseWriter, r *http.Request) {
			loc := r.URL.Query().Get("l")
			skill := r.URL.Query().Get("s")
			page := r.URL.Query().Get("p")
			svr.RenderPageForLocationAndTagAdmin(w, loc, skill, page, "list-jobs-admin.html")
		},
	)
}

func PostAJobForLocationPageHandler(svr server.Server, location string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		svr.RenderPostAJobForLocation(w, r, location)
	}
}

func PostAJobForLocationFromURLPageHandler(svr server.Server) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		location := vars["location"]
		location = strings.ReplaceAll(location, "-", " ")
		reg, err := regexp.Compile("[^a-zA-Z0-9\\s]+")
		if err != nil {
			log.Fatal(err)
		}
		location = reg.ReplaceAllString(location, "")
		svr.RenderPostAJobForLocation(w, r, location)
	}
}

func JobBySlugPageHandler(svr server.Server) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		slug := vars["slug"]
		location := vars["l"]
		job, err := database.JobPostBySlug(svr.Conn, slug)
		if err != nil || job == nil {
			svr.JSON(w, http.StatusNotFound, fmt.Sprintf("Job golang.cafe/job/%s not found", slug))
			return
		}
		if err := database.TrackJobView(svr.Conn, job); err != nil {
			svr.Log(err, fmt.Sprintf("unable to track job view for %s: %v", slug, err))
		}
		jobLocations := strings.Split(job.Location, "/")
		var isQuickApply bool
		emailRe := regexp.MustCompile("^[a-zA-Z0-9.!#$%&'*+/=?^_`{|}~-]+@[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?(?:\\.[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?)*$")
		if emailRe.MatchString(job.HowToApply) {
			isQuickApply = true
		}
		svr.Render(w, http.StatusOK, "job.html", map[string]interface{}{
			"Job":                     job,
			"JobURIEncoded":           url.QueryEscape(job.Slug),
			"IsQuickApply":            isQuickApply,
			"HTMLJobDescription":      svr.MarkdownToHTML(job.JobDescription),
			"HTMLJobPerks":            svr.MarkdownToHTML(job.Perks),
			"HTMLJobInterviewProcess": svr.MarkdownToHTML(job.InterviewProcess),
			"LocationFilter":          location,
			"ExternalJobId":           job.ExternalID,
			"GoogleJobCreatedAt":      time.Unix(job.CreatedAt, 0).Format(time.RFC3339),
			"GoogleJobValidThrough":   time.Unix(job.CreatedAt, 0).AddDate(0, 5, 0),
			"GoogleJobLocation":       jobLocations[0],
			"GoogleJobDescription":    strconv.Quote(strings.ReplaceAll(string(svr.MarkdownToHTML(job.JobDescription)), "\n", "")),
		})
	}
}

func LandingPageForLocationHandler(svr server.Server, location string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		page := r.URL.Query().Get("p")
		svr.RenderPageForLocationAndTag(w, location, "", page, "landing.html")
	}
}

func LandingPageForLocationAndSkillPlaceholderHandler(svr server.Server, location string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		skill := strings.ReplaceAll(vars["skill"], "-", " ")
		page := r.URL.Query().Get("p")
		svr.RenderPageForLocationAndTag(w, location, skill, page, "landing.html")
	}
}

func LandingPageForLocationPlaceholderHandler(svr server.Server) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		loc := strings.ReplaceAll(vars["location"], "-", " ")
		page := r.URL.Query().Get("p")
		svr.RenderPageForLocationAndTag(w, loc, "", page, "landing.html")
	}
}

func LandingPageForSkillPlaceholderHandler(svr server.Server) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		skill := strings.ReplaceAll(vars["skill"], "-", " ")
		page := r.URL.Query().Get("p")
		svr.RenderPageForLocationAndTag(w, "", skill, page, "landing.html")
	}
}

func LandingPageForSkillAndLocationPlaceholderHandler(svr server.Server) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		loc := strings.ReplaceAll(vars["location"], "-", " ")
		skill := strings.ReplaceAll(vars["skill"], "-", " ")
		page := r.URL.Query().Get("p")
		svr.RenderPageForLocationAndTag(w, loc, skill, page, "landing.html")
	}
}
