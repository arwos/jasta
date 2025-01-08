package jasta

import (
	"context"
	"fmt"
	"net/http"

	"go.osspkg.com/logx"
	"go.osspkg.com/network/listen"

	"go.arwos.org/jasta/internal/pkg/spool"
)

func (v *Jasta) startup(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
			func() {
				defer v.spool.Stop()

				v.spool.AddConfig(&spool.Config{
					Network: "tcp",
					Address: "127.0.0.1:8443",
					Domains: map[string]func(http.ResponseWriter, *http.Request){
						"*": func(w http.ResponseWriter, r *http.Request) {
							fmt.Println(r.Host, r.RequestURI)
							w.WriteHeader(200)
						},
					},
					Certs: []listen.Certificate{
						{
							Addresses:    []string{"localhost"},
							AutoGenerate: true,
						},
					},
				})

				if e := v.spool.Start(); e != nil {
					logx.Error("Start Pool HTTP", "err", e)
				}
			}()
		}
	}
}
