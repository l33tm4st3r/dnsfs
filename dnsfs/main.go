package main

import (
	"flag"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"strings"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	baddr        = flag.String("addr", "185.230.223.69", "IP addr you want to send out on")
	dnsport      = flag.String("dnsport", "53", "Port to listen for DNS queries")
	ipfname      = flag.String("file", "iplist.txt", "The IP list to send queries to")
	dnsbase      = flag.String("dbase", "s.flm.me.uk", "the zone that has the NS record pointing to it")
	mockMode     = flag.Bool("mock", false, "Enable mock mode (local in-memory caching for offline testing)")
	ipList       = make([]string, 0)
	globalSender net.PacketConn
)

func main() {
	flag.Parse()

	txListener, err := net.ListenPacket("udp4", "0.0.0.0:34123")
	if err != nil {
		log.Fatalf("failed to listen on UDP 34123 (for tx) %s", err.Error())
	}
	globalSender = txListener

	ipList = parseIPList(*ipfname)
	initResolverStats()

	rxListener, err := net.ListenPacket("udp4", *baddr+":"+*dnsport)
	if err != nil {
		log.Fatalf("failed to listen on UDP %s %s", *dnsport, err.Error())
	}

	startDNSListener(rxListener)

	// if !verifyNSsetup(*dnsbase) {
	// 	log.Fatalf("failed to confirm NS records are setup correctly, exiting")
	// }

	http.HandleFunc("/upload", handleUpload)
	http.HandleFunc("/fetch", handleDownload)
	http.HandleFunc("/api/stats", handleGetStats)
	http.HandleFunc("/api/resolvers", handleGetResolvers)
	http.HandleFunc("/api/resolvers/test", handleTestResolvers)
	http.HandleFunc("/api/logs", handleGetLogs)
	http.Handle("/metrics", promhttp.Handler())
	http.Handle("/", http.FileServer(http.Dir("./static")))

	if *mockMode {
		AddLog("Mock mode enabled: local in-memory caching and loopback query redirection active.")
	}
	AddLog("DNSFS started. Listening on %s:%s (UDP) and %s:5050 (HTTP).", *baddr, *dnsport, *baddr)
	log.Fatalf("Failed to listen on HTTP: %s", http.ListenAndServe(*baddr+":5050", nil))
}

func parseIPList(path string) []string {
	bytes, err := ioutil.ReadFile(path)
	if err != nil {
		log.Fatalf("Unable to read IP list, %s", err.Error())
	}

	lines := strings.Split(string(bytes), "\n")
	var result []string
	for ln, s := range lines {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		t := net.ParseIP(s)
		if t == nil {
			log.Fatalf("Error in IP list on line %d", ln)
		}
		result = append(result, s)
	}

	return result
}
