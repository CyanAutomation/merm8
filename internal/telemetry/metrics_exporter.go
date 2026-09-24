package telemetry

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
)

var defaultHistogramBuckets = []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10}

var (
	labelEscaper = strings.NewReplacer("\\", "\\\\", "\n", "\\n", `"`, `\"`)
	helpEscaper  = strings.NewReplacer("\\", "\\\\", "\n", "\\n")
)

type prometheusMetric interface {
	appendPrometheus(*strings.Builder)
}

type counterVec struct {
	name       string
	help       string
	labelNames []string
	mu         sync.RWMutex
	series     map[string]counterSeries
}

type counterSeries struct {
	labels []string
	value  uint64
}

func newCounterVec(name, help string, labelNames ...string) *counterVec {
	return &counterVec{
		name:       name,
		help:       help,
		labelNames: append([]string(nil), labelNames...),
		series:     make(map[string]counterSeries),
	}
}

func (cv *counterVec) inc(labelValues ...string) {
	validateLabelValues(cv.name, cv.labelNames, labelValues)
	key := labelValuesKey(labelValues)

	cv.mu.Lock()
	defer cv.mu.Unlock()

	series, ok := cv.series[key]
	if !ok {
		series.labels = append([]string(nil), labelValues...)
	}
	series.value++
	cv.series[key] = series
}

func (cv *counterVec) appendPrometheus(out *strings.Builder) {
	appendMetricHeader(out, cv.name, cv.help, "counter")

	cv.mu.RLock()
	defer cv.mu.RUnlock()

	for _, key := range sortedCounterKeys(cv.series) {
		series := cv.series[key]
		fmt.Fprintf(out, "%s%s %d\n", cv.name, formatPrometheusLabels(cv.labelNames, series.labels), series.value)
	}
}

type histogramVec struct {
	name       string
	help       string
	labelNames []string
	buckets    []float64
	mu         sync.RWMutex
	series     map[string]histogramSeries
}

type histogramSeries struct {
	labels  []string
	buckets []uint64
	count   uint64
	sum     float64
}

func newHistogramVec(name, help string, labelNames ...string) *histogramVec {
	return &histogramVec{
		name:       name,
		help:       help,
		labelNames: append([]string(nil), labelNames...),
		buckets:    append([]float64(nil), defaultHistogramBuckets...),
		series:     make(map[string]histogramSeries),
	}
}

func (hv *histogramVec) observe(value float64, labelValues ...string) {
	validateLabelValues(hv.name, hv.labelNames, labelValues)
	key := labelValuesKey(labelValues)

	hv.mu.Lock()
	defer hv.mu.Unlock()

	series, ok := hv.series[key]
	if !ok {
		series.labels = append([]string(nil), labelValues...)
		series.buckets = make([]uint64, len(hv.buckets))
	}
	series.count++
	series.sum += value
	for i, upperBound := range hv.buckets {
		if value <= upperBound {
			series.buckets[i]++
		}
	}
	hv.series[key] = series
}

func (hv *histogramVec) appendPrometheus(out *strings.Builder) {
	appendMetricHeader(out, hv.name, hv.help, "histogram")

	hv.mu.RLock()
	defer hv.mu.RUnlock()

	for _, key := range sortedHistogramKeys(hv.series) {
		series := hv.series[key]
		for i, upperBound := range hv.buckets {
			labels := append(append([]string(nil), series.labels...), strconv.FormatFloat(upperBound, 'f', -1, 64))
			labelNames := append(append([]string(nil), hv.labelNames...), "le")
			fmt.Fprintf(out, "%s_bucket%s %d\n", hv.name, formatPrometheusLabels(labelNames, labels), series.buckets[i])
		}

		labels := append(append([]string(nil), series.labels...), "+Inf")
		labelNames := append(append([]string(nil), hv.labelNames...), "le")
		fmt.Fprintf(out, "%s_bucket%s %d\n", hv.name, formatPrometheusLabels(labelNames, labels), series.count)
		fmt.Fprintf(out, "%s_sum%s %s\n", hv.name, formatPrometheusLabels(hv.labelNames, series.labels), strconv.FormatFloat(series.sum, 'g', -1, 64))
		fmt.Fprintf(out, "%s_count%s %d\n", hv.name, formatPrometheusLabels(hv.labelNames, series.labels), series.count)
	}
}

func appendMetricHeader(out *strings.Builder, name, help, kind string) {
	fmt.Fprintf(out, "# HELP %s %s\n# TYPE %s %s\n", name, helpEscaper.Replace(help), name, kind)
}

func validateLabelValues(metricName string, labelNames, labelValues []string) {
	if len(labelNames) != len(labelValues) {
		panic(fmt.Sprintf("%s: expected %d label values, got %d", metricName, len(labelNames), len(labelValues)))
	}
}

func labelValuesKey(labelValues []string) string {
	var key strings.Builder
	for _, value := range labelValues {
		fmt.Fprintf(&key, "%d:", len(value))
		key.WriteString(value)
	}
	return key.String()
}

func formatPrometheusLabels(labelNames, labelValues []string) string {
	if len(labelNames) == 0 {
		return ""
	}
	validateLabelValues("", labelNames, labelValues)

	var labels strings.Builder
	labels.WriteByte('{')
	for i, name := range labelNames {
		if i > 0 {
			labels.WriteByte(',')
		}
		fmt.Fprintf(&labels, "%s=\"%s\"", name, labelEscaper.Replace(labelValues[i]))
	}
	labels.WriteByte('}')
	return labels.String()
}

func sortedCounterKeys(series map[string]counterSeries) []string {
	keys := make([]string, 0, len(series))
	for key := range series {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func sortedHistogramKeys(series map[string]histogramSeries) []string {
	keys := make([]string, 0, len(series))
	for key := range series {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
