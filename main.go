package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

type Backend struct {
	Upstream       string
	Server         string
	RequestCounter int
	reporting      bool
}

var (
	backends = []Backend{}

	httpRpsPerBackend = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "http_rps_per_backend",
		Help: "Requests per second on each nginx backend",
	}, []string{"upstream", "server"})
)

func init() {
	prometheus.MustRegister(httpRpsPerBackend)
}

func main() {

	go http.ListenAndServe(":8080", prometheus.Handler())

	url := os.Getenv("VTS_URL")
	sd := os.Getenv("SCRAPE_DURATION")

	d, err := time.ParseDuration(sd)
	if err != nil {
		panic(err)
	}

	tck := time.Tick(d)

	lastRecalc := time.Time{}
	for {
		rd, err := GetVts(url)
		if err != nil {
			log.Println(err)
			continue
		}

		bs, err := Parse(rd)
		if err != nil {
			fmt.Println(err)
			continue
		}

		now := time.Now()
		err = RecalcMetrics(bs, lastRecalc, now)
		if err != nil {
			log.Println(err)
			continue
		}

		lastRecalc = now
		<-tck
	}
}

func GetVts(url string) (*bytes.Buffer, error) {

	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	buf := &bytes.Buffer{}
	_, err = io.Copy(buf, resp.Body)
	if err != nil {
		return nil, err
	}

	return buf, err
}

func Parse(buf *bytes.Buffer) ([]Backend, error) {

	d := struct {
		UpstreamZones map[string][]Backend
	}{}

	err := json.NewDecoder(buf).Decode(&d)
	if err != nil {
		return nil, err
	}

	bs := []Backend{}

	for name, upbs := range d.UpstreamZones {
		for i, _ := range upbs {
			upbs[i].Upstream = name
			bs = append(bs, upbs[i])
		}
	}

	return bs, nil
}

func RecalcMetrics(newBs []Backend, lastRecalc, now time.Time) error {

	ts := now.Sub(lastRecalc).Seconds()

	if len(backends) == 0 && len(newBs) > 0 {
		backends = newBs
		return nil
	}

	for old, oldb := range backends {
		for _, newb := range newBs {
			if oldb.Upstream != newb.Upstream || oldb.Server != newb.Server {
				continue
			}

			backends[old].reporting = true

			// XXX check overflow

			// detect reload (and skip reporting the metric)
			if oldb.RequestCounter > newb.RequestCounter {
				continue
			}

			httpRpsPerBackend.With(prometheus.Labels{"upstream": oldb.Upstream, "server": oldb.Server}).Set(float64(newb.RequestCounter-oldb.RequestCounter) / ts)
		}
	}

	// delete removed backends from prom
	for _, oldb := range backends {
		if !oldb.reporting {
			httpRpsPerBackend.Delete(prometheus.Labels{"upstream": oldb.Upstream, "server": oldb.Server})
		}
	}

	backends = newBs

	return nil
}
