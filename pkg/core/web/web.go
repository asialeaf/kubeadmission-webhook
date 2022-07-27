package web

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	stdlog "log"
	"net/http"
	"os"
	"time"

	"git.harmonycloud.cn/yeyazhou/kubeadmission-webhook/pkg/config"
	"git.harmonycloud.cn/yeyazhou/kubeadmission-webhook/pkg/core/admission"
	"github.com/go-chi/chi/v5"
	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
)

func New(logger log.Logger, o *Options) *Handler {
	if logger == nil {
		logger = log.NewNopLogger()
	}

	cwd, err := os.Getwd()
	if err != nil {
		cwd = "<error retrieving current working directory>"
	}

	router := chi.NewRouter()

	h := &Handler{
		logger: logger,

		router:   router,
		reloadCh: make(chan chan error),
		options:  o,
		birth:    time.Now(),
		cwd:      cwd,
	}

	h.admission = admission.NewAPI(logger)
	router.Mount("/admission", h.admission.Routes())

	if o.EnableLifecycle {
		router.Post("/-/reload", h.reload)
		router.Put("/-/reload", h.reload)
	} else {
		forbiddenAPINotEnabled := func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusForbidden)
			io.WriteString(w, "Lifecycle API is not enabled.")
		}

		router.Post("/-/reload", forbiddenAPINotEnabled)
		router.Put("/-/reload", forbiddenAPINotEnabled)
	}

	readyf := h.testReady
	router.Get("/-/healthy", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		io.WriteString(w, "OK.\n")
	})
	router.Get("/-/ready", readyf(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "OK.\n")
	}))

	return h
}

func (h *Handler) ApplyConfig(conf *config.Config) error {
	h.mtx.Lock()
	defer h.mtx.Unlock()

	h.config = conf
	h.admission.Update(conf)
	return nil
}

// Run serves the HTTP endpoints.
func (h *Handler) Run(ctx context.Context) error {
	level.Info(h.logger).Log("msg", "Start listening for connections", "address", h.options.ListenAddress)
	// listener, err := net.Listen("tcp", h.options.ListenAddress)
	// if err != nil {
	// 	return err
	// }

	errlog := stdlog.New(log.NewStdlibAdapter(level.Error(h.logger)), "", 0)
	httpSrv := &http.Server{
		Addr:      h.options.ListenAddress,
		Handler:   h.router,
		ErrorLog:  errlog,
		TLSConfig: h.configTLS(h.options),
	}

	errCh := make(chan error)
	go func() {
		errCh <- httpSrv.ListenAndServeTLS("", "")
	}()

	select {
	case e := <-errCh:
		return e
	case <-ctx.Done():
		httpSrv.Shutdown(ctx)
		return nil
	}
}

// Reload returns the receive-only channel that signals configuration reload requests.
func (h *Handler) Reload() <-chan chan error {
	return h.reloadCh
}

func (h *Handler) reload(w http.ResponseWriter, r *http.Request) {
	rc := make(chan error)
	h.reloadCh <- rc
	if err := <-rc; err != nil {
		http.Error(w, fmt.Sprintf("failed to reload config: %s", err), http.StatusInternalServerError)
		return
	}

	io.WriteString(w, "OK")
}

// Ready sets Handler to be ready.
func (h *Handler) Ready() {
	h.ready.Store(true)
}

// Verifies whether the server is ready or not.
func (h *Handler) isReady() bool {
	return h.ready.Load()
}

// Checks if server is ready, calls f if it is, returns 503 if it is not.
func (h *Handler) testReady(f http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if h.isReady() {
			f(w, r)
		} else {
			w.WriteHeader(http.StatusServiceUnavailable)
			io.WriteString(w, "Service Unavailable")
		}
	}
}

func (h *Handler) configTLS(o *Options) *tls.Config {
	sCert, err := tls.LoadX509KeyPair(o.CertFile, o.KeyFile)
	if err != nil {
		// klog.Fatal(err)
		level.Error(h.logger).Log("msg", "Failed to loadX509keypair", "err", err)
	}
	return &tls.Config{
		Certificates: []tls.Certificate{sCert},
		// TODO: uses mutual tls after we agree on what cert the apiserver should use.
		// ClientAuth:   tls.RequireAndVerifyClientCert,
	}
}
