package main

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/miekg/dns"
)

type LogEntry struct {
	Timestamp string `json:"timestamp"`
	Message   string `json:"message"`
}

var (
	logsMutex sync.Mutex
	logsQueue = make([]LogEntry, 0)
)

func AddLog(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	entry := LogEntry{
		Timestamp: time.Now().Format("15:04:05.000"),
		Message:   msg,
	}
	logsMutex.Lock()
	logsQueue = append(logsQueue, entry)
	if len(logsQueue) > 100 {
		logsQueue = logsQueue[1:]
	}
	logsMutex.Unlock()
}

type FileInfo struct {
	Name      string    `json:"name"`
	Size      int64     `json:"size"`
	Chunks    int       `json:"chunks"`
	CreatedAt time.Time `json:"createdAt"`
}

var (
	filesMutex    sync.RWMutex
	uploadedFiles = make(map[string]FileInfo)
)

func TrackFile(name string, size int64, chunks int) {
	filesMutex.Lock()
	defer filesMutex.Unlock()
	uploadedFiles[name] = FileInfo{
		Name:      name,
		Size:      size,
		Chunks:    chunks,
		CreatedAt: time.Now(),
	}
}

type ResolverInfo struct {
	IP         string  `json:"ip"`
	Requests   int64   `json:"requests"`
	LatencyMs  float64 `json:"latencyMs"`
	LastActive string  `json:"lastActive"`
	Status     string  `json:"status"` // Active, Inactive, Unknown
}

var (
	resolverMutex sync.RWMutex
	resolverStats = make(map[string]*ResolverInfo)
)

func TrackResolverRequest(ip string) {
	resolverMutex.Lock()
	defer resolverMutex.Unlock()

	host, _, err := net.SplitHostPort(ip)
	if err == nil {
		ip = host
	}

	info, exists := resolverStats[ip]
	if !exists {
		info = &ResolverInfo{IP: ip, Status: "Active"}
		resolverStats[ip] = info
	}
	info.Requests++
	info.LastActive = time.Now().Format("15:04:05")
	info.Status = "Active"
}

func initResolverStats() {
	resolverMutex.Lock()
	defer resolverMutex.Unlock()
	for _, ip := range ipList {
		if _, exists := resolverStats[ip]; !exists {
			resolverStats[ip] = &ResolverInfo{
				IP:        ip,
				Requests:  0,
				LatencyMs: 0.0,
				Status:    "Unknown",
			}
		}
	}
}

func testResolverLatency(ip string) float64 {
	tempSocket, err := net.ListenPacket("udp4", "0.0.0.0:0")
	if err != nil {
		return -1
	}
	defer tempSocket.Close()

	m1 := new(dns.Msg)
	m1.Id = dns.Id()
	m1.RecursionDesired = true
	m1.Question = make([]dns.Question, 1)
	m1.Question[0] = dns.Question{
		Name:   "google.com.",
		Qtype:  dns.TypeA,
		Qclass: dns.ClassINET,
	}
	dnspacket, _ := m1.Pack()

	addr, err := net.ResolveUDPAddr("udp", fmt.Sprintf("%s:53", ip))
	if err != nil {
		return -1
	}

	tempSocket.SetDeadline(time.Now().Add(time.Millisecond * 800))
	start := time.Now()
	_, err = tempSocket.WriteTo(dnspacket, addr)
	if err != nil {
		return -1
	}

	dnsraw := make([]byte, 1500)
	_, _, err = tempSocket.ReadFrom(dnsraw)
	if err != nil {
		return -1
	}

	return float64(time.Since(start).Microseconds()) / 1000.0
}

func handleGetStats(rw http.ResponseWriter, req *http.Request) {
	filesMutex.RLock()
	fileList := make([]FileInfo, 0, len(uploadedFiles))
	for _, f := range uploadedFiles {
		fileList = append(fileList, f)
	}
	filesMutex.RUnlock()

	resolverMutex.RLock()
	resolverCount := len(resolverStats)
	resolverMutex.RUnlock()

	response := map[string]interface{}{
		"addr":          *baddr,
		"dbase":         *dnsbase,
		"files":         fileList,
		"resolverCount": resolverCount,
		"time":          time.Now().Format(time.RFC3339),
	}

	rw.Header().Set("Content-Type", "application/json")
	json.NewEncoder(rw).Encode(response)
}

func handleGetResolvers(rw http.ResponseWriter, req *http.Request) {
	resolverMutex.RLock()
	list := make([]*ResolverInfo, 0, len(resolverStats))
	for _, r := range resolverStats {
		list = append(list, r)
	}
	resolverMutex.RUnlock()

	rw.Header().Set("Content-Type", "application/json")
	json.NewEncoder(rw).Encode(list)
}

func handleTestResolvers(rw http.ResponseWriter, req *http.Request) {
	resolverMutex.Lock()
	ips := make([]string, 0, len(resolverStats))
	for ip := range resolverStats {
		ips = append(ips, ip)
	}
	resolverMutex.Unlock()

	AddLog("Triggered latency tests for %d resolvers...", len(ips))

	var wg sync.WaitGroup
	for _, ip := range ips {
		wg.Add(1)
		go func(ip string) {
			defer wg.Done()
			lat := testResolverLatency(ip)

			resolverMutex.Lock()
			defer resolverMutex.Unlock()
			r := resolverStats[ip]
			if r != nil {
				if lat >= 0 {
					r.LatencyMs = lat
					r.Status = "Active"
				} else {
					r.LatencyMs = -1
					r.Status = "Inactive"
				}
			}
		}(ip)
	}
	wg.Wait()

	AddLog("Resolver latency test finished.")

	rw.Header().Set("Content-Type", "application/json")
	rw.Write([]byte(`{"status": "ok"}`))
}

func handleGetLogs(rw http.ResponseWriter, req *http.Request) {
	logsMutex.Lock()
	defer logsMutex.Unlock()

	rw.Header().Set("Content-Type", "application/json")
	json.NewEncoder(rw).Encode(logsQueue)
}
