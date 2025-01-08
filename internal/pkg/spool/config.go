package spool

import (
	"context"
	"net/http"

	"github.com/google/uuid"
	"go.osspkg.com/logx"
	"go.osspkg.com/network/listen"

	"go.arwos.org/jasta/internal/pkg/global"
)

type Config struct {
	Network string
	Address string
	Domains map[string]func(http.ResponseWriter, *http.Request)
	Certs   []listen.Certificate
}

type handler struct {
	Domains map[string]func(http.ResponseWriter, *http.Request)
}

func (v *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()

	w.Header().Set("X-Server-Id", "jasta")

	reqId, err := uuid.NewV7()
	if err != nil {
		logx.Error("Generate Request ID", "err", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.Header().Set("X-Req-Id", reqId.String())
	r.WithContext(context.WithValue(r.Context(), global.XRequestID, reqId.String()))

	if next, ok := v.Domains[r.Host]; ok {
		next(w, r)
		return
	}

	if next, ok := v.Domains[global.AllDomain]; ok {
		next(w, r)
		return
	}

	w.WriteHeader(http.StatusNotFound)
}
