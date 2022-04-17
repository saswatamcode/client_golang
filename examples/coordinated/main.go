// Copyright 2015 The Prometheus Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// A minimal example of how to coordinate short-living functionality with Prometheus scrapes.
package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var addr = flag.String("listen-address", ":8080", "The address to listen on for HTTP requests.")

func main() {
	flag.Parse()
	process := make(chan struct{}, 1)

	m := http.NewServeMux()
	s := http.Server{Addr: *addr, Handler: m}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var (
		// Create a counter to track the number of operations done by
		// this short living job, in this case just ticking for 30 seconds.
		// Thus the value of this counter after a successful run would always
		// be 30.
		shortJobOpCounter = prometheus.NewCounter(
			prometheus.CounterOpts{
				Name: "short_job_op_counter",
				Help: "Counts number of operations of a short job.",
			},
		)
	)

	prometheus.MustRegister(shortJobOpCounter)

	// Once the handler receives a message from the process channel,
	// indicating that the process is complete, it immediately calls
	// DoneFn() in a new goroutine, which in this case shuts down the
	// server by calling cancel().

	// This means that after completion of the main functionality,
	// the process waits for the last Prometheus scrape before
	// shutting down entirely, ensuring that all its metrics are gathered.
	m.Handle("/metrics", promhttp.InstrumentMetricHandler(
		prometheus.DefaultRegisterer,
		promhttp.HandlerFor(
			prometheus.DefaultGatherer,
			promhttp.HandlerOpts{
				Process: process,
				DoneFn: func() {
					log.Println("short job done, executing DoneFn in final scrape")
					cancel()
				},
			}),
	))

	// This simulates a short living job which runs for 30 seconds,
	// and increments the short_job_op_counter every second,
	// which upon completion sends a message to the process channel.
	done := make(chan struct{}, 1)

	go func() {
		time.Sleep(30 * time.Second)
		done <- struct{}{}
	}()

	go func() {
		ticker := time.NewTicker(1 * time.Second)
		i := 0
		for {
			select {
			case <-ticker.C:
				i++
				shortJobOpCounter.Inc()
				log.Println("tick ", i)
			case <-done:
				ticker.Stop()
				log.Println("short job done, sending process signal")
				process <- struct{}{}
			}
		}
	}()

	// Run the server inside of a goroutine, and shutdown based on context.
	go func() {
		if err := s.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal(err)
		}
	}()

	<-ctx.Done()
	s.Shutdown(ctx)
}
