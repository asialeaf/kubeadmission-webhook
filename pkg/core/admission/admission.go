package admission

import (
	"log"
	"sync"

	"git.harmonycloud.cn/yeyazhou/kubeadmission-webhook/pkg/config"
)

type API struct {
	// Protect against config, template and http client
	mtx sync.RWMutex

	conf   *config.Config
	logger log.Logger
}
