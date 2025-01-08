package spool

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"time"

	"go.osspkg.com/errors"
	"go.osspkg.com/logx"
	"go.osspkg.com/network/listen"
	"go.osspkg.com/syncing"
	"golang.org/x/net/http2"
)

type SPool struct {
	ctx    context.Context
	config []*Config
	pool   []*http.Server
	wg     syncing.Group
}

func NewSPool(ctx context.Context) *SPool {
	return &SPool{
		ctx:    ctx,
		pool:   make([]*http.Server, 0, 10),
		config: make([]*Config, 0, 10),
		wg:     syncing.NewGroup(),
	}
}

func (v *SPool) AddConfig(c *Config) {
	for _, conf := range v.config {
		if conf.Network == c.Network && conf.Address == c.Address {

			for d, f := range c.Domains {
				conf.Domains[d] = f
			}

			conf.Certs = append(conf.Certs, c.Certs...)

			return
		}
	}

	v.config = append(v.config, c)
}

func (v *SPool) Start() (err error) {
	for _, c := range v.config {

		var (
			lc net.ListenConfig
			l  net.Listener
		)
		if l, err = lc.Listen(v.ctx, "tcp", c.Address); err != nil {
			return fmt.Errorf("start listener for %s: %w", c.Address, err)
		}
		if len(c.Certs) > 0 {
			var conf *tls.Config
			if conf, err = listen.NewTLSConfig(c.Certs...); err != nil {
				return fmt.Errorf("tls configuration for %s: %w", c.Address, err)
			}
			conf.NextProtos = []string{"h2", "http/1.1", "acme-tls/1"}
			l = tls.NewListener(l, conf)
		}

		servHttp2 := &http2.Server{
			PermitProhibitedCipherSuites: true,
			PingTimeout:                  1 * time.Second,
			ReadIdleTimeout:              5 * time.Second,
			WriteByteTimeout:             5 * time.Second,
			IdleTimeout:                  5 * time.Second,
			MaxUploadBufferPerConnection: 65535,
			MaxUploadBufferPerStream:     1,
		}

		servHttp := &http.Server{
			Addr:                         c.Address,
			Handler:                      &handler{Domains: c.Domains},
			ReadTimeout:                  5 * time.Second,
			WriteTimeout:                 5 * time.Second,
			IdleTimeout:                  5 * time.Second,
			DisableGeneralOptionsHandler: true,
			ErrorLog:                     log.New(io.Discard, "", 0),
		}

		if err = http2.ConfigureServer(servHttp, servHttp2); err != nil {
			return fmt.Errorf("http2 configuration for %s: %w", c.Address, err)
		}

		v.pool = append(v.pool, servHttp)

		v.wg.Background(func() {
			servHttp.SetKeepAlivesEnabled(true)

			logx.Info("Public HTTP Server Start", "addr", c.Address)
			if e := servHttp.Serve(l); e != nil && !errors.Is(e, http.ErrServerClosed) {
				logx.Error("Public HTTP Server Stop", "err", e, "addr", c.Address)
			}
		})
	}

	v.wg.Wait()
	return nil
}

func (v *SPool) Stop() {
	ctx, cncl := context.WithTimeout(v.ctx, 1*time.Second)
	defer cncl()

	for _, s := range v.pool {
		s := s
		v.wg.Background(func() {
			logx.Info("Public HTTP Server Stop", "addr", s.Addr)
			if e := s.Shutdown(ctx); e != nil && !errors.Is(e, http.ErrServerClosed) {
				logx.Error("Public HTTP Server Stop", "err", e, "addr", s.Addr)
			}
		})
	}

	v.wg.Wait()

	v.config = v.config[:0]
	v.pool = v.pool[:0]
}
