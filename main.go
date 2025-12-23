package main

import (
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/subhammurarka/LedgerKV/db"
	"github.com/subhammurarka/LedgerKV/server"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var walSizeGauge = prometheus.NewGauge(
	prometheus.GaugeOpts{
		Name: "bitcask_wal_size_bytes",
		Help: "Total size of Bitcask WAL on disk",
	},
)

func init() {
	prometheus.MustRegister(walSizeGauge)
}

func main() {
	store, err := db.Open("data")
	if err != nil {
		log.Fatal(err)
	}
	defer store.Close()

	go func() {
		log.Println("Prometheus metrics on :2112/metrics")
		http.Handle("/metrics", promhttp.Handler())
		http.ListenAndServe(":2112", nil)
	}()

	startWALSizeCollector("./data")

	srv := server.New(store)
	log.Fatal(srv.Listen(":7379"))
}

func startWALSizeCollector(dir string) {
	go func() {
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()

		for range ticker.C {
			var total int64 = 0

			filepath.Walk(dir, func(_ string, info os.FileInfo, err error) error {
				if err == nil && !info.IsDir() {
					total += info.Size()
				}
				return nil
			})

			walSizeGauge.Set(float64(total))
		}
	}()
}
