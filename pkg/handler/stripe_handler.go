package handler

import (
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"

	"github.com/0x13a/golang.cafe/pkg/database"
	"github.com/0x13a/golang.cafe/pkg/email"
	"github.com/0x13a/golang.cafe/pkg/payment"
	"github.com/0x13a/golang.cafe/pkg/server"
)

func StripePaymentConfirmationWebookHandler(svr server.Server) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		const MaxBodyBytes = int64(65536)
		req.Body = http.MaxBytesReader(w, req.Body, MaxBodyBytes)
		body, err := ioutil.ReadAll(req.Body)
		if err != nil {
			svr.Log(err, "error reading request body from stripe")
			svr.JSON(w, http.StatusServiceUnavailable, nil)
			return
		}

		stripeSig := req.Header.Get("Stripe-Signature")
		sess, err := payment.HandleCheckoutSessionComplete(body, svr.GetConfig().StripeEndpointSecret, stripeSig)
		if err != nil {
			svr.Log(err, "error while handling checkout session complete")
			svr.JSON(w, http.StatusBadRequest, nil)
			return
		}
		if sess != nil {
			affectedRows, err := database.SaveSuccessfulPayment(svr.Conn, sess.ID)
			if err != nil {
				svr.Log(err, "error while saving successful payment")
				svr.JSON(w, http.StatusBadRequest, nil)
				return
			}
			if affectedRows != 1 {
				svr.Log(errors.New("invalid number of rows affected when saving payment"), fmt.Sprintf("got %d expected 1", affectedRows))
				svr.JSON(w, http.StatusBadRequest, nil)
				return
			}
			job, err := database.GetJobByStripeSessionID(svr.Conn, sess.ID)
			if err != nil {
				svr.Log(errors.New("unable to find job by stripe session id"), fmt.Sprintf("session id %s", sess.ID))
				svr.JSON(w, http.StatusBadRequest, nil)
				return
			}
			purchaseEvent, err := database.GetPurchaseEventBySessionID(svr.Conn, sess.ID)
			if err != nil {
				svr.Log(errors.New("unable to find purchase event by stripe session id"), fmt.Sprintf("session id %s", sess.ID))
				svr.JSON(w, http.StatusBadRequest, nil)
				return
			}
			jobToken, err := database.TokenByJobID(svr.Conn, job.ID)
			if err != nil {
				svr.Log(errors.New("unable to find token for job id"), fmt.Sprintf("session id %s job id %d", sess.ID, job.ID))
				svr.JSON(w, http.StatusBadRequest, nil)
				return
			}
			if job.ApprovedAt != nil && job.AdType != database.JobAdSponsoredPinnedFor30Days && job.AdType != database.JobAdSponsoredPinnedFor7Days && (purchaseEvent.AdType == database.JobAdSponsoredPinnedFor7Days || job.AdType != database.JobAdSponsoredPinnedFor30Days) {
				err := database.UpdateJobAdType(svr.Conn, purchaseEvent.AdType, job.ID)
				if err != nil {
					svr.Log(errors.New("unable to update job to new ad type"), fmt.Sprintf("unable to update job id %d to new ad type %d for session id %s", job.ID, purchaseEvent.AdType, sess.ID))
					svr.JSON(w, http.StatusBadRequest, nil)
					return
				}
				err = svr.GetEmail().SendEmail("Diego from Golang Cafe <team@golang.cafe>", purchaseEvent.Email, email.GolangCafeEmailAddress, "Your Job Ad on Golang Cafe", fmt.Sprintf("Your Job Ad has been upgraded successfully and it's now pinned to the home page. You can edit the Job Ad at any time and check page views and clickouts by following this link https://golang.cafe/edit/%s", jobToken))
				if err != nil {
					svr.Log(err, "unable to send email while upgrading job ad")
				}
			}
			svr.JSON(w, http.StatusOK, nil)
			return
		}

		svr.JSON(w, http.StatusOK, nil)
	}
}
