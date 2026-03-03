package prometheus

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
)

var DefBuckets = []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10}

type CounterOpts struct{ Name, Help string }
type HistogramOpts struct {
	Name, Help string
	Buckets    []float64
}

type Collector interface{ writeProm(*strings.Builder) }

type Registry struct{ collectors []Collector }

func NewRegistry() *Registry                     { return &Registry{} }
func (r *Registry) MustRegister(cs ...Collector) { r.collectors = append(r.collectors, cs...) }
func (r *Registry) Gather() string {
	var b strings.Builder
	for _, c := range r.collectors {
		c.writeProm(&b)
	}
	return b.String()
}

type CounterVec struct {
	opts   CounterOpts
	labels []string
	mu     sync.Mutex
	values map[string]float64
}

type counterObserver struct {
	cv  *CounterVec
	key string
}

func NewCounterVec(opts CounterOpts, labels []string) *CounterVec {
	return &CounterVec{opts: opts, labels: labels, values: map[string]float64{}}
}
func (cv *CounterVec) WithLabelValues(vals ...string) *counterObserver {
	return &counterObserver{cv: cv, key: cv.key(vals)}
}
func (o *counterObserver) Inc()                  { o.cv.mu.Lock(); defer o.cv.mu.Unlock(); o.cv.values[o.key]++ }
func (cv *CounterVec) key(vals []string) string  { return strings.Join(vals, "\xff") }
func (cv *CounterVec) parse(key string) []string { return strings.Split(key, "\xff") }
func (cv *CounterVec) writeProm(b *strings.Builder) {
	fmt.Fprintf(b, "# HELP %s %s\n# TYPE %s counter\n", cv.opts.Name, cv.opts.Help, cv.opts.Name)
	cv.mu.Lock()
	defer cv.mu.Unlock()
	keys := make([]string, 0, len(cv.values))
	for k := range cv.values {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Fprintf(b, "%s%s %g\n", cv.opts.Name, formatLabels(cv.labels, cv.parse(k)), cv.values[k])
	}
}

type HistogramVec struct {
	opts   HistogramOpts
	labels []string
	mu     sync.Mutex
	values map[string]*histSeries
}
type histSeries struct {
	counts []uint64
	count  uint64
	sum    float64
}
type histObserver struct {
	hv  *HistogramVec
	key string
}

func NewHistogramVec(opts HistogramOpts, labels []string) *HistogramVec {
	if len(opts.Buckets) == 0 {
		opts.Buckets = DefBuckets
	}
	return &HistogramVec{opts: opts, labels: labels, values: map[string]*histSeries{}}
}
func (hv *HistogramVec) WithLabelValues(vals ...string) *histObserver {
	return &histObserver{hv: hv, key: strings.Join(vals, "\xff")}
}
func (o *histObserver) Observe(v float64) {
	hv := o.hv
	hv.mu.Lock()
	defer hv.mu.Unlock()
	s := hv.values[o.key]
	if s == nil {
		s = &histSeries{counts: make([]uint64, len(hv.opts.Buckets))}
		hv.values[o.key] = s
	}
	s.count++
	s.sum += v
	for i, b := range hv.opts.Buckets {
		if v <= b {
			s.counts[i]++
		}
	}
}
func (hv *HistogramVec) writeProm(b *strings.Builder) {
	name := hv.opts.Name
	fmt.Fprintf(b, "# HELP %s %s\n# TYPE %s histogram\n", name, hv.opts.Help, name)
	hv.mu.Lock()
	defer hv.mu.Unlock()
	keys := make([]string, 0, len(hv.values))
	for k := range hv.values {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		vals := strings.Split(k, "\xff")
		s := hv.values[k]
		for i, bkt := range hv.opts.Buckets {
			lblNames := append(append([]string{}, hv.labels...), "le")
			lblVals := append(append([]string{}, vals...), strconv.FormatFloat(bkt, 'f', -1, 64))
			fmt.Fprintf(b, "%s_bucket%s %d\n", name, formatLabels(lblNames, lblVals), s.counts[i])
		}
		lblNames := append(append([]string{}, hv.labels...), "le")
		lblVals := append(append([]string{}, vals...), "+Inf")
		fmt.Fprintf(b, "%s_bucket%s %d\n", name, formatLabels(lblNames, lblVals), s.count)
		fmt.Fprintf(b, "%s_sum%s %g\n", name, formatLabels(hv.labels, vals), s.sum)
		fmt.Fprintf(b, "%s_count%s %d\n", name, formatLabels(hv.labels, vals), s.count)
	}
}

func formatLabels(names, vals []string) string {
	if len(names) == 0 {
		return ""
	}
	parts := make([]string, 0, len(names))
	for i, n := range names {
		v := ""
		if i < len(vals) {
			v = vals[i]
		}
		v = strings.ReplaceAll(v, "\\", "\\\\")
		v = strings.ReplaceAll(v, "\"", "\\\"")
		parts = append(parts, fmt.Sprintf("%s=\"%s\"", n, v))
	}
	return "{" + strings.Join(parts, ",") + "}"
}
