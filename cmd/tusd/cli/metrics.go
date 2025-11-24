package cli

import (
	"net/http"

	"github.com/JimAlex927/tusd-backend/pkg/handler"
	"github.com/JimAlex927/tusd-backend/pkg/hooks"
	"github.com/JimAlex927/tusd-backend/pkg/prometheuscollector"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var MetricsOpenConnections = prometheus.NewGauge(prometheus.GaugeOpts{
	Name: "tusd_connections_open",
	Help: "Current number of open connections.",
})

func SetupMetrics(mux *http.ServeMux, handler *handler.Handler) {
	prometheus.MustRegister(MetricsOpenConnections)
	prometheus.MustRegister(hooks.MetricsHookErrorsTotal)
	prometheus.MustRegister(hooks.MetricsHookInvocationsTotal)
	prometheus.MustRegister(prometheuscollector.New(handler.Metrics))

	printStartupLog("Using %s as the metrics path.\n", Flags.MetricsPath)
	mux.Handle(Flags.MetricsPath, promhttp.Handler())
}
