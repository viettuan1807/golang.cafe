package handler

import (
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"

	"github.com/0x13a/golang.cafe/pkg/database"
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
			svr.JSON(w, http.StatusBadRequest, nil)
			return
		}
		if sess != nil {
			log.Printf("session: %+v\n", sess)
			// send email "thanks for your payment"
			affectedRows, err := database.SaveSuccessfulPayment(svr.Conn, sess.ID)
			if err != nil {
				svr.Log(err, "error while saving successful payment")
				svr.JSON(w, http.StatusBadRequest, nil)
			}
			if affectedRows != 1 {
				svr.Log(errors.New("invalid number of rows affected when saving payment"), fmt.Sprintf("got %d expected 1", affectedRows))
				svr.JSON(w, http.StatusBadRequest, nil)
			}
			svr.JSON(w, http.StatusOK, nil)
			return
		}

		svr.JSON(w, http.StatusOK, nil)
	}
}
