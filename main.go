package main

import (
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/go-kit/kit/log/level"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/prometheus/common/promlog"
	"github.com/prometheus/common/promlog/flag"
	"github.com/prometheus/common/version"
	kingpin "gopkg.in/alecthomas/kingpin.v2"
)

const (
	namespace        = "mini"
	metricsGroupDemo = "demo"
)

var (
	allMetrics          map[string]metricInfo
	collectMetricsGroup map[string]bool
)

type metricInfo struct {
	Desc *prometheus.Desc
	Type prometheus.ValueType
}

func MetricsGroup_Values() []string {
	return []string{
		metricsGroupDemo,
	}
}

func newMetricInfo(instanceName string, metricName string, docString string, t prometheus.ValueType, variableLabels []string, constLabels prometheus.Labels) metricInfo {
	return metricInfo{
		Desc: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, instanceName, metricName),
			docString,
			variableLabels,
			constLabels,
		),
		Type: t,
	}
}

func createMetrics(instanceName string) {
	allMetrics = make(map[string]metricInfo)
	allMetrics["demo"] = newMetricInfo(instanceName, "demo", "Mini overall demo show current unix time", prometheus.GaugeValue, nil, nil)
}

type MiniExporter struct {
	instance string
	// Cache-releated
	cacheEnabled    bool
	cacheDuration   time.Duration
	lastCollectTime time.Time
	cache           []prometheus.Metric
	collectMutex    sync.Mutex
}

// NewMiniExporter constructs a MiniExporter instance
func NewMiniExporter() *MiniExporter {
	return &MiniExporter{
		cache:           make([]prometheus.Metric, 0),
		lastCollectTime: time.Unix(0, 0),
		collectMutex:    sync.Mutex{},
	}
}

// Describe describes all the metrics ever exported by the Mini exporter. It
// implements prometheus.Collector.
func (e *MiniExporter) Describe(ch chan<- *prometheus.Desc) {
	for _, m := range allMetrics {
		ch <- m.Desc
	}
}

// Collect fetches the stats from configured Mini location and delivers them
// as Prometheus metrics. It implements prometheus.Collector.
func (e *MiniExporter) Collect(outCh chan<- prometheus.Metric) {
	if e.cacheEnabled {
		e.collectMutex.Lock()
		defer e.collectMutex.Unlock()
		expiry := e.lastCollectTime.Add(e.cacheDuration)
		if time.Now().Before(expiry) {
			// Return cached
			for _, cachedMetric := range e.cache {
				outCh <- cachedMetric
			}
			return
		}
		// Reset cache for fresh sampling, but re-use underlying array
		e.cache = e.cache[:0]
	}

	samplesCh := make(chan prometheus.Metric)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		for metric := range samplesCh {
			outCh <- metric
			if e.cacheEnabled {
				e.cache = append(e.cache, metric)
			}
		}
		wg.Done()
	}()

	e.collectDemoMetric(samplesCh)

	close(samplesCh)
	e.lastCollectTime = time.Now()
	wg.Wait()
}

func main() {
	var (
		listenAddress = kingpin.Flag(
			"web.listen-address",
			"Address on which to expose metrics and web interface.",
		).Default(":9100").String()

		metricsPath = kingpin.Flag(
			"web.telemetry-path",
			"Path under which to expose metrics.",
		).Default("/metrics").String()
	)

	var miniInstance = NewMiniExporter()

	promlogConfig := &promlog.Config{}
	flag.AddFlags(kingpin.CommandLine, promlogConfig)
	kingpin.HelpFlag.Short('h')
	kingpin.Parse()
	logger := promlog.New(promlogConfig)

	collectMetricsGroup = make(map[string]bool)
	for _, v := range MetricsGroup_Values() {
		collectMetricsGroup[v] = true
	}

	for k, v := range collectMetricsGroup {
		level.Info(logger).Log("metrics_group", k, "collect", v)
	}

	createMetrics(miniInstance.instance)

	prometheus.MustRegister(miniInstance)
	prometheus.MustRegister(version.NewCollector("mini_exporter"))
	http.Handle(*metricsPath, promhttp.Handler())

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {

		w.Write([]byte(`<html>
             <head><title>Mini Exporter</title></head>
             <body>
             <h1>Mini Exporter</h1>
             <p><a href='` + *metricsPath + `'>Metrics</a></p>
             <h2>Build</h2>
             <pre>` + version.Info() + ` ` + version.BuildContext() + `</pre>
             </body>
             </html>`))
	})

	if err := http.ListenAndServe(*listenAddress, nil); err != nil {
		level.Error(logger).Log("msg", "Error starting HTTP server", "err", err)
		os.Exit(1)
	}

}

// 收集指标数据: 即当前Unix时间戳
func (e *MiniExporter) collectDemoMetric(ch chan<- prometheus.Metric) {
	ch <- prometheus.MustNewConstMetric(
		allMetrics["demo"].Desc, allMetrics["demo"].Type, float64(time.Now().Local().Unix()),
	)
}
