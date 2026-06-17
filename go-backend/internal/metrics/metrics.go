package metrics

import (
	"net/http"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"thz-service/internal/models"
)

type MetricsCollector struct {
	registry          *prometheus.Registry
	mu                sync.Mutex

	TaskQueueDepth    prometheus.Gauge
	ResultQueueDepth  prometheus.Gauge
	ActiveWorkers     prometheus.Gauge

	FFTFitDuration    prometheus.Histogram
	ParamsFitDuration prometheus.Histogram
	PredictionDuration prometheus.Histogram
	TotalDuration     prometheus.Histogram

	TasksTotal        prometheus.Counter
	TasksCompleted    prometheus.Counter
	TasksFailed       prometheus.Counter
	TasksInvalid      prometheus.Counter
	CacheHits         prometheus.Counter
	CacheMisses       prometheus.Counter

	AverageFFTDuration    prometheus.Gauge
	AverageParamsDuration prometheus.Gauge
	AveragePredictionDuration prometheus.Gauge

	fftDurationSum    float64
	fftCount          int64
	paramsDurationSum float64
	paramsCount       int64
	predictionDurationSum float64
	predictionCount     int64
}

func NewMetricsCollector() *MetricsCollector {
	reg := prometheus.NewRegistry()

	m := &MetricsCollector{
		registry: reg,

		TaskQueueDepth: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "thz_task_queue_depth",
			Help: "Current number of tasks in the task queue",
		}),
		ResultQueueDepth: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "thz_result_queue_depth",
			Help: "Current number of results in the result queue",
		}),
		ActiveWorkers: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "thz_active_workers",
			Help: "Number of active worker threads",
		}),

		FFTFitDuration: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "thz_fft_fit_duration_seconds",
			Help:    "Duration of FFT fitting in seconds",
			Buckets: prometheus.DefBuckets,
		}),
		ParamsFitDuration: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "thz_params_fit_duration_seconds",
			Help:    "Duration of Dorney-Duvillaret parameter fitting in seconds",
			Buckets: prometheus.DefBuckets,
		}),
		PredictionDuration: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "thz_prediction_duration_seconds",
			Help:    "Duration of PLSR prediction in seconds",
			Buckets: prometheus.DefBuckets,
		}),
		TotalDuration: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "thz_total_duration_seconds",
			Help:    "Total processing duration in seconds",
			Buckets: prometheus.DefBuckets,
		}),

		TasksTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "thz_tasks_total",
			Help: "Total number of tasks received",
		}),
		TasksCompleted: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "thz_tasks_completed_total",
			Help: "Total number of tasks completed successfully",
		}),
		TasksFailed: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "thz_tasks_failed_total",
			Help: "Total number of tasks failed",
		}),
		TasksInvalid: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "thz_tasks_invalid_total",
			Help: "Total number of invalid samples detected",
		}),
		CacheHits: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "thz_cache_hits_total",
			Help: "Total number of cache hits",
		}),
		CacheMisses: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "thz_cache_misses_total",
			Help: "Total number of cache misses",
		}),

		AverageFFTDuration: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "thz_average_fft_duration_seconds",
			Help: "Average FFT fitting duration in seconds",
		}),
		AverageParamsDuration: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "thz_average_params_duration_seconds",
			Help: "Average Dorney-Duvillaret parameter fitting duration in seconds",
		}),
		AveragePredictionDuration: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "thz_average_prediction_duration_seconds",
			Help: "Average PLSR prediction duration in seconds",
		}),
	}

	reg.MustRegister(
		m.TaskQueueDepth,
		m.ResultQueueDepth,
		m.ActiveWorkers,
		m.FFTFitDuration,
		m.ParamsFitDuration,
		m.PredictionDuration,
		m.TotalDuration,
		m.TasksTotal,
		m.TasksCompleted,
		m.TasksFailed,
		m.TasksInvalid,
		m.CacheHits,
		m.CacheMisses,
		m.AverageFFTDuration,
		m.AverageParamsDuration,
		m.AveragePredictionDuration,
	)

	return m
}

func (m *MetricsCollector) RecordFFTDuration(duration time.Duration) {
	d := duration.Seconds()
	m.FFTFitDuration.Observe(d)
	m.mu.Lock()
	m.fftDurationSum += d
	m.fftCount++
	if m.fftCount > 0 {
		m.AverageFFTDuration.Set(m.fftDurationSum / float64(m.fftCount))
	}
	m.mu.Unlock()
}

func (m *MetricsCollector) RecordParamsDuration(duration time.Duration) {
	d := duration.Seconds()
	m.ParamsFitDuration.Observe(d)
	m.mu.Lock()
	m.paramsDurationSum += d
	m.paramsCount++
	if m.paramsCount > 0 {
		m.AverageParamsDuration.Set(m.paramsDurationSum / float64(m.paramsCount))
	}
	m.mu.Unlock()
}

func (m *MetricsCollector) RecordPredictionDuration(duration time.Duration) {
	d := duration.Seconds()
	m.PredictionDuration.Observe(d)
	m.mu.Lock()
	m.predictionDurationSum += d
	m.predictionCount++
	if m.predictionCount > 0 {
		m.AveragePredictionDuration.Set(m.predictionDurationSum / float64(m.predictionCount))
	}
	m.mu.Unlock()
}

func (m *MetricsCollector) RecordTotalDuration(duration time.Duration) {
	m.TotalDuration.Observe(duration.Seconds())
}

func (m *MetricsCollector) IncrTask()            { m.TasksTotal.Inc() }
func (m *MetricsCollector) IncrCompleted()       { m.TasksCompleted.Inc() }
func (m *MetricsCollector) IncrFailed()          { m.TasksFailed.Inc() }
func (m *MetricsCollector) IncrInvalid()         { m.TasksInvalid.Inc() }
func (m *MetricsCollector) IncrCacheHit()        { m.CacheHits.Inc() }
func (m *MetricsCollector) IncrCacheMiss()      { m.CacheMisses.Inc() }

func (m *MetricsCollector) SetTaskQueueDepth(depth float64) {
	m.TaskQueueDepth.Set(depth)
}

func (m *MetricsCollector) SetResultQueueDepth(depth float64) {
	m.ResultQueueDepth.Set(depth)
}

func (m *MetricsCollector) SetActiveWorkers(count float64) {
	m.ActiveWorkers.Set(count)
}

func (m *MetricsCollector) Handler() http.Handler {
	return promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{})
}

func (m *MetricsCollector) GetRegistry() *prometheus.Registry {
	return m.registry
}

func (m *MetricsCollector) GetSummary() map[string]interface{} {
	m.mu.Lock()
	defer m.mu.Unlock()

	return map[string]interface{}{
		"average_fft_duration_seconds":    m.fftDurationSum / float64(m.fftCount+1),
		"average_params_duration_seconds": m.paramsDurationSum / float64(m.paramsCount+1),
		"average_prediction_duration_seconds": m.predictionDurationSum / float64(m.predictionCount+1),
		"fft_count":            m.fftCount,
		"params_count":          m.paramsCount,
		"prediction_count":    m.predictionCount,
	}
}

func (m *MetricsCollector) GetMetricsResponse() *models.MetricsResponse {
	m.mu.Lock()
	defer m.mu.Unlock()

	avgFFT := 0.0
	avgParams := 0.0
	avgPred := 0.0
	cacheHitRate := 0.0

	if m.fftCount > 0 {
		avgFFT = m.fftDurationSum / float64(m.fftCount)
	}
	if m.paramsCount > 0 {
		avgParams = m.paramsDurationSum / float64(m.paramsCount)
	}
	if m.predictionCount > 0 {
		avgPred = m.predictionDurationSum / float64(m.predictionCount)
	}

	ch := getCounterValue(m.CacheHits)
	cm := getCounterValue(m.CacheMisses)
	if ch+cm > 0 {
		cacheHitRate = (ch / (ch + cm)) * 100.0
	}

	return &models.MetricsResponse{
		TaskQueueDepth:            getGaugeValue(m.TaskQueueDepth),
		ResultQueueDepth:          getGaugeValue(m.ResultQueueDepth),
		ActiveWorkers:             getGaugeValue(m.ActiveWorkers),
		TasksTotal:                getCounterValue(m.TasksTotal),
		TasksCompleted:            getCounterValue(m.TasksCompleted),
		TasksFailed:               getCounterValue(m.TasksFailed),
		TasksInvalid:              getCounterValue(m.TasksInvalid),
		CacheHits:                 ch,
		CacheMisses:               cm,
		CacheHitRate:              cacheHitRate,
		AverageFFTDuration:        avgFFT,
		AverageParamsDuration:     avgParams,
		AveragePredictionDuration: avgPred,
	}
}

func getGaugeValue(g prometheus.Gauge) float64 {
	metric := prometheus.NewRegistry()
	metric.MustRegister(g)
	mfs, _ := metric.Gather()
	for _, mf := range mfs {
		if mf.GetMetric() != nil && len(mf.GetMetric()) > 0 {
			return mf.GetMetric()[0].GetGauge().GetValue()
		}
	}
	return 0
}

func getCounterValue(c prometheus.Counter) float64 {
	metric := prometheus.NewRegistry()
	metric.MustRegister(c)
	mfs, _ := metric.Gather()
	for _, mf := range mfs {
		if mf.GetMetric() != nil && len(mf.GetMetric()) > 0 {
			return mf.GetMetric()[0].GetCounter().GetValue()
		}
	}
	return 0
}
