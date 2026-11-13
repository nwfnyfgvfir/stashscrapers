package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
	_ "time/tzdata"

	"stashscrapers/internal/engine"
	"stashscrapers/internal/service"
)

func main() {
	dataDefault := os.Getenv("STASHSCRAPERS_DATA")
	if dataDefault == "" {
		dataDefault = "data"
	}
	data := flag.String("data", dataDefault, "persistent data directory")
	listen := flag.String("listen", os.Getenv("STASHSCRAPERS_LISTEN"), "override HTTP listen address")
	version := flag.Bool("version", false, "print engine version")
	healthcheck := flag.Bool("healthcheck", false, "check the running HTTP service for Docker")
	healthURL := flag.String("health-url", "http://127.0.0.1:8080/api/health", "HTTP health endpoint")
	flag.Parse()
	if *healthcheck {
		if err := checkHealth(*healthURL); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	if *version {
		fmt.Println(engine.Version)
		return
	}
	started := time.Now()
	a, e := service.New(*data)
	if e != nil {
		log.Fatal(e)
	}
	addr := a.Config().Listen
	if *listen != "" {
		addr = *listen
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	listener, e := net.Listen("tcp", addr)
	if e != nil {
		log.Fatal(e)
	}
	server := &http.Server{Handler: a.Handler(), ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 1 << 20}
	go a.RunScheduler(ctx)
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdown)
	}()
	log.Printf("engine=%s address=%s startup_ms=%d", engine.Version, listener.Addr(), time.Since(started).Milliseconds())
	if e = server.Serve(listener); e != nil && e != http.ErrServerClosed {
		log.Fatal(e)
	}
}

func checkHealth(endpoint string) error {
	client := &http.Client{Timeout: 4 * time.Second, Transport: &http.Transport{Proxy: nil}}
	response, err := client.Get(endpoint)
	if err != nil {
		return fmt.Errorf("health endpoint unavailable")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("health endpoint returned HTTP %d", response.StatusCode)
	}
	var body struct {
		Status string `json:"status"`
	}
	if json.NewDecoder(response.Body).Decode(&body) != nil || body.Status != "ok" {
		return fmt.Errorf("health endpoint is not healthy")
	}
	return nil
}
