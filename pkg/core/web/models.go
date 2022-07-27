package web

import (
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-kit/log"
	"go.uber.org/atomic"

	"git.harmonycloud.cn/yeyazhou/kubeadmission-webhook/pkg/config"
	"git.harmonycloud.cn/yeyazhou/kubeadmission-webhook/pkg/core/admission"
)

// Options for the web Handler.
type Options struct {
	ListenAddress   string
	EnableLifecycle bool
	// Flags           map[string]string
}

type Handler struct {
	mtx    sync.RWMutex
	logger log.Logger

	admission *admission.API

	router   chi.Router
	reloadCh chan chan error
	options  *Options
	config   *config.Config
	birth    time.Time
	cwd      string

	ready atomic.Bool // ready is uint32 rather than boolean to be able to use atomic functions.
}
