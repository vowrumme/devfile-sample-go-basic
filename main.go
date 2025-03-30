package main

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/jackpal/gateway"
	"github.com/mitchellh/go-ps"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	version  = "1.2"
	httpReqs = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "http_requests_total",
		Help: "How many HTTP requests processed, partitioned by status code and HTTP method.",
	})
	requestCount = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "http_request_count_total",
		Help: "Counter of HTTP requests made.",
	}, []string{"code", "method"})
	requestDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "http_request_duration_seconds",
		Help:    "A histogram of latencies for requests.",
		Buckets: append([]float64{0.000001, 0.001, 0.003}, prometheus.DefBuckets...),
	}, []string{"code", "method"})
	responseSize = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "http_response_size_bytes",
		Help:    "A histogram of response sizes for requests.",
		Buckets: []float64{0, 2, 4, 6, 8, 10, 12, 14, 16, 18, 20},
	}, []string{"code", "method"})
)

func init() {
	log.Printf("initializing this app...")
	prometheus.MustRegister(httpReqs)
	prometheus.MustRegister(requestCount)
	prometheus.MustRegister(requestDuration)
	prometheus.MustRegister(responseSize)
}

func main() {
	//http.HandleFunc("/", helloHandler)
	// Instrument helloHandler
	helloHandler := http.HandlerFunc(doHelloHandler)
	wrappedHelloHandler := promhttp.InstrumentHandlerCounter(
		requestCount,
		promhttp.InstrumentHandlerDuration(
			requestDuration,
			promhttp.InstrumentHandlerResponseSize(
				responseSize,
				helloHandler),
		),
	)
	http.Handle("/", wrappedHelloHandler)
	http.HandleFunc("/oneline", onelineHandler)
	http.HandleFunc("/ps", psHandler)
	http.HandleFunc("/version", versionHandler)

	// serve metrics.
	log.Printf("serving metrics at: %s", ":9090")
	go http.ListenAndServe(":9090", promhttp.Handler())

	// serve our handlers.
	if err := http.ListenAndServe(":8080", nil); err != nil {
		log.Panicf("error while serving: %s", err)
	}
}

func getLocalIP() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return ""
	}
	for _, address := range addrs {
		// check the address type and if it is not a loopback the display it
		if ipnet, ok := address.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ipnet.IP.To4() != nil {
				return ipnet.IP.String()
			}
		}
	}
	return ""
}

func getProcCmdArgs(p *ps.UnixProcess) []string {
	cmdPath := fmt.Sprintf("/proc/%d/cmdline", p.Pid())
	data, err := ioutil.ReadFile(cmdPath)
	if err != nil {
		return nil
	}
	args := strings.Split(string(bytes.TrimRight(data, string("\x00"))), string(byte(0)))
	return args
}

func getProcesses() {
	processes, err := ps.Processes()
	if err != nil {
		fmt.Printf("ps.Processes(): %v\n", err)
	}
	for _, p := range processes {
		fmt.Printf("* %s\t%s\n", p.Executable(), getProcCmdArgs(p.(*ps.UnixProcess)))
	}
}

func getTimestamp() string {
	t := time.Now()
	const layout = "2006/01/02 15:04:05"
	return fmt.Sprintf("%s", t.Format(layout))
}

func getOnelineLog(r *http.Request) string {
	logstr := fmt.Sprintf("%s Hello, World: Host=%s, LocalAddr=%s, RemoteAddr=%s", getTimestamp(), r.Host, getLocalIP(), r.RemoteAddr)
	fwdAddr := r.Header.Get("X-Forwarded-For")
	if fwdAddr != "" {
		logstr = fmt.Sprintf("%s, X-Forwarded-For=%s", logstr, fwdAddr)
	}
	return logstr
}

func doHelloHandler(w http.ResponseWriter, r *http.Request) {
	fmt.Printf("%s <helloHandler>\n", getOnelineLog(r))
	fmt.Fprintf(os.Stderr, "(STDERR) %s <helloHandler>\n", getOnelineLog(r))
	h := r.Header
	keys := make([]string, len(h))
	i := 0
	for k := range h {
		keys[i] = k
		i++
	}

	//fmt.Println(keys)
	fmt.Fprintln(w, "Hello, World!")

	hostname, err := os.Hostname()
	if err != nil {
		fmt.Printf("os.Hostname(): %v\n", err)
		return
	}
	fmt.Fprintf(w, "  Timestamp: %s\n", getTimestamp())
	fmt.Fprintf(w, "  Hostname: %s\n", hostname)
	fmt.Fprintf(w, "  LocalAddress: %s\n", getLocalIP())

	gw, err := gateway.DiscoverGateway()
	if err != nil {
		fmt.Printf("gateway.DiscoverGateway(): %v\n", err)
		return
	}
	fmt.Fprintf(w, "  Gateway: %s\n", gw.String())

	fmt.Fprintln(w, "  Headers:")

	sort.Strings(keys)
	for _, k := range keys {
		fmt.Fprintf(w, "    %s: %s\n", k, h[k])
	}

	fmt.Fprintf(w, "  Host: %s\n", r.Host)
	fmt.Fprintf(w, "  RemoteAddress: %s\n", r.RemoteAddr)

	httpReqs.Inc()
}

func onelineHandler(w http.ResponseWriter, r *http.Request) {
	fmt.Printf("%s <onelineHandler>\n", getOnelineLog(r))
	fmt.Fprintf(os.Stderr, "(STDERR) %s <onelineHandler>\n", getOnelineLog(r))
	fmt.Fprintf(w, "%s\n", getOnelineLog(r))

	httpReqs.Inc()
}

func versionHandler(w http.ResponseWriter, r *http.Request) {
	fmt.Printf("%s <versionHandler>\n", getOnelineLog(r))
	fmt.Fprintf(w, "%s\n", version)

	httpReqs.Inc()
}

func psHandler(w http.ResponseWriter, r *http.Request) {
	fmt.Printf("%s <psHandler>\n", getOnelineLog(r))

	processes, err := ps.Processes()
	if err != nil {
		fmt.Fprintf(w, "ps.Processes(): %v\n", err)
	}
	for _, p := range processes {
		fmt.Fprintf(w, "* %s\t%s\n", p.Executable(), getProcCmdArgs(p.(*ps.UnixProcess)))
	}

	httpReqs.Inc()
}
