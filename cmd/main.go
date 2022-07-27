package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/prometheus/common/promlog"
	"github.com/prometheus/common/promlog/flag"
	"github.com/prometheus/common/version"
	"gopkg.in/alecthomas/kingpin.v2"

	"git.harmonycloud.cn/yeyazhou/kubeadmission-webhook/pkg/config"
	"git.harmonycloud.cn/yeyazhou/kubeadmission-webhook/pkg/core/web"
)

func main() {
	os.Exit(run())
}

func run() int {
	var (
		listenAddress = kingpin.Flag(
			"web.listen-address",
			"The address to listen on for web interface.",
		).Default(":8443").String()
		enableLifecycle = kingpin.Flag(
			"web.enable-lifecycle",
			"Enable reload via HTTP request.",
		).Default("false").Bool()
		configFile = kingpin.Flag(
			"config.file",
			"Path to the configuration file.",
		).Default("/etc/webhook/config/config.json").ExistingFile()
		tlsCertFile = kingpin.Flag(
			"tls.cert-file",
			"File containing the default x509 Certificate for HTTPS. (CA cert, if any, concatenated after server cert).",
		).Default("/etc/webhook/certs/tls.crt").ExistingFile()
		tlsKeyFile = kingpin.Flag(
			"tls.private-key-file",
			"File containing the default x509 private key matching --tls-cert-file.",
		).Default("/etc/webhook/certs/tls.key").ExistingFile()
	)

	promlogConfig := &promlog.Config{}
	flag.AddFlags(kingpin.CommandLine, promlogConfig)
	// add version info
	addVersion()
	kingpin.Version(version.Print("kubeadmission-webhook"))
	kingpin.HelpFlag.Short('h')
	kingpin.Parse()

	logger := promlog.New(promlogConfig)
	level.Info(logger).Log("msg", "Starting kubeadmission-webhook", "version", version.Info())
	level.Info(logger).Log("msg", "Build context", version.BuildContext())

	webHandler := web.New(log.With(logger, "component", "web"), &web.Options{
		ListenAddress:   *listenAddress,
		EnableLifecycle: *enableLifecycle,
		CertFile:        *tlsCertFile,
		KeyFile:         *tlsKeyFile,
		// Flags:           flagsMap,
	})

	configLogger := log.With(logger, "component", "configuration")
	configCoordinator := config.NewCoordinator(*configFile, configLogger)

	configCoordinator.Subscribe(func(conf *config.Config) error {
		return webHandler.ApplyConfig(conf)
	})

	if err := configCoordinator.Reload(); err != nil {
		return 1
	}

	ctxWeb, cancelWeb := context.WithCancel(context.Background())
	defer cancelWeb()

	srvCh := make(chan error, 1)
	go func() {
		defer close(srvCh)

		if err := webHandler.Run(ctxWeb); err != nil {
			level.Error(logger).Log("msg", "Error starting HTTP server", "err", err)
			srvCh <- err
		}
	}()

	var (
		reloadReady = make(chan struct{})
		hup         = make(chan os.Signal, 1)
		term        = make(chan os.Signal, 1)
	)
	signal.Notify(hup, syscall.SIGHUP)
	signal.Notify(term, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-reloadReady
		for {
			select {
			case <-ctxWeb.Done():
				return
			case <-hup:
				// ignore error, already logged in `reload()`
				_ = configCoordinator.Reload()
			case rc := <-webHandler.Reload():
				if err := configCoordinator.Reload(); err != nil {
					rc <- err
				} else {
					rc <- nil
				}
			}
		}
	}()

	// Wait for reload or termination signals.
	close(reloadReady) // Unblock SIGHUP handler.
	webHandler.Ready()

	for {
		select {
		case <-term:
			level.Info(logger).Log("msg", "Received SIGTERM, exiting gracefully...")
			cancelWeb()
		case err := <-srvCh:
			if err != nil {
				return 1
			}

			return 0
		}
	}
}

func addVersion() {
	version.Version = "v1.0"
	version.Branch = "main"
	version.BuildUser = "yeyazhou@harmonycloud.cn"
	version.BuildDate = time.Now().Format("2006-01-02 15:04:05 MST Mon")
}
