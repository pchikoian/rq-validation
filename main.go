package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"k8s-resource-webhook/webhook"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

func main() {
	port       := env("PORT",        "8443")
	healthPort := env("HEALTH_PORT", "8080")
	certFile   := env("TLS_CERT",    "certs/tls.crt")
	keyFile    := env("TLS_KEY",     "certs/tls.key")

	cfg, err := rest.InClusterConfig()
	if err != nil {
		log.Fatalf("in-cluster config: %v", err)
	}
	kube, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		log.Fatalf("kubernetes client: %v", err)
	}

	h := webhook.New(webhook.NewK8sQuotaFetcher(kube))

	mux := http.NewServeMux()
	mux.Handle("/validate", h)

	healthMux := http.NewServeMux()
	healthMux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	tlsSrv    := &http.Server{Addr: ":" + port,       Handler: mux}
	healthSrv := &http.Server{Addr: ":" + healthPort, Handler: healthMux}

	go func() {
		log.Printf("health  → :%s", healthPort)
		if err := healthSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("health server: %v", err)
		}
	}()
	go func() {
		log.Printf("webhook → :%s (TLS)", port)
		if err := tlsSrv.ListenAndServeTLS(certFile, keyFile); err != nil && err != http.ErrServerClosed {
			log.Fatalf("webhook server: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGTERM, syscall.SIGINT)
	<-quit

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = tlsSrv.Shutdown(ctx)
	_ = healthSrv.Shutdown(ctx)
	log.Println("shutdown complete")
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
