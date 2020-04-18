package handler

import (
	"net/http"

	"github.com/0x13a/golang.cafe/pkg/server"
)

func GetAuthPageHandler(svr server.Server) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		svr.Render(w, http.StatusOK, "auth.html", nil)
	}
}
