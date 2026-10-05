package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/hibiken/asynq"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/redis/go-redis/v9"

	"github.com/apaderin/octoconv/internal/cancel"
	"github.com/apaderin/octoconv/internal/convert"
	"github.com/apaderin/octoconv/internal/db"
	"github.com/apaderin/octoconv/internal/jobs"
	"github.com/apaderin/octoconv/internal/queue"
	"github.com/apaderin/octoconv/internal/sandbox"
	"github.com/apaderin/octoconv/internal/storage"
	"github.com/apaderin/octoconv/internal/worker"
)

const cadSandboxDisabledEnv = "CAD_SANDBOX_DISABLED"

const cadConcurrencyEnv = "CAD_WORKER_CONCURRENCY"

const cadPreflightTimeout = 60 * time.Second

func main() {
	if sandbox.RunExecModeIfRequested(os.Args) {
		return
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	sandboxHelper, err := sandbox.ResolveHelper(cadSandboxDisabledEnv)
	if err != nil {
		log.Fatalf("cad sandbox: %v", err)
	}

	concurrency := envInt(cadConcurrencyEnv, 1)
	log.Printf("📐 %s=%d", cadConcurrencyEnv, concurrency)
	engineTimeout := envDuration("CAD_ENGINE_TIMEOUT", convert.CADDefaultEngineTimeout)
	conv, err := convert.ConfinedCAD(
		sandboxHelper,
		concurrency,
		cadConcurrencyEnv,
		envDuration("CAD_GUARD_TIMEOUT", convert.CADDefaultGuardTimeout),
		envDuration("CAD_MESH_TIMEOUT", convert.CADDefaultMeshTimeout),
		envDuration("CAD_RENDER_TIMEOUT", convert.CADDefaultRenderTimeout),
		engineTimeout,
	)
	if err != nil {
		log.Fatalf("cad sandbox: %v", err)
	}

	convert.Default.Replace(conv)
	convert.Default.Replace(convert.CADRenderConverter{CADConverter: conv})
	if len(sandboxHelper) == 0 {
		log.Printf("⚠️  %s=true — the engines can read every file in this container", cadSandboxDisabledEnv)
	} else {
		log.Printf("🔒 cad sandbox: landlock via %s (convert and render)", strings.Join(sandboxHelper, " "))
	}

	preflightCtx, cancelPreflight := context.WithTimeout(ctx, cadPreflightTimeout)
	if err := convert.CADPreflight(preflightCtx, conv); err != nil {
		cancelPreflight()
		log.Fatalf("cad sandbox preflight: %v", err)
	}
	cancelPreflight()
	if len(sandboxHelper) > 0 {
		log.Printf("🔒 cad sandbox proven: cad-guard and cad-mesh ran inside the job directory and were denied a file outside it")
	}

	pool, err := db.Connect(ctx)
	if err != nil {
		log.Fatalf("postgres: %v", err)
	}
	defer pool.Close()

	store, err := storage.New(ctx)
	if err != nil {
		log.Fatalf("storage: %v", err)
	}

	redisOpt, err := queue.RedisOpt()
	if err != nil {
		log.Fatalf("redis: %v", err)
	}

	cancelRDB := redis.NewClient(&redis.Options{Addr: redisOpt.Addr})
	defer cancelRDB.Close()
	go cancel.NewSubscriber(cancelRDB, cancel.Default).Run(ctx)

	qc, err := queue.NewClient()
	if err != nil {
		log.Fatalf("queue client: %v", err)
	}
	defer qc.Close()

	repo := jobs.NewRepo(pool)

	h := worker.NewHandler(
		repo,
		store,
		convert.Default,
		engineTimeout,
		nil,
		nil,
		qc,
		nil,
		0,
		0,
		nil,
	)

	mux := asynq.NewServeMux()
	mux.HandleFunc(queue.TypeCADJob, worker.NewRunner(h, worker.CADDescriptor).Handle)

	srv := asynq.NewServer(redisOpt, asynq.Config{
		Concurrency:     concurrency,
		Queues:          map[string]int{queue.QueueCAD: 1},
		RetryDelayFunc:  queue.RetryDelayFunc,
		ShutdownTimeout: engineTimeout + 10*time.Second,
	})

	log.Printf("🐙 cad-worker starting (queue=%s)", queue.QueueCAD)
	if err := srv.Start(mux); err != nil {
		log.Fatalf("cad-worker: %v", err)
	}

	metricsAddr := os.Getenv("METRICS_ADDR")
	if metricsAddr == "" {
		metricsAddr = "127.0.0.1:9090"
	}
	metricsSrv := &http.Server{
		Addr:              metricsAddr,
		Handler:           promhttp.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		log.Printf("📊 metrics listening on %s", metricsAddr)
		if err := metricsSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("metrics listen: %v", err)
		}
	}()

	<-ctx.Done()
	log.Println("🛑 shutting down cad-worker...")
	srv.Shutdown()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := metricsSrv.Shutdown(shutdownCtx); err != nil {
		log.Printf("metrics graceful shutdown failed: %v", err)
	}
	log.Println("bye 👋")
}

func envInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(firstField(v)); err == nil {
			return n
		}
	}
	return def
}

func envDuration(key string, def time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(firstField(v)); err == nil {
			return d
		}
	}
	return def
}

func firstField(s string) string {
	for i := 0; i < len(s); i++ {
		if s[i] == ' ' || s[i] == '\t' {
			return s[:i]
		}
	}
	return s
}
