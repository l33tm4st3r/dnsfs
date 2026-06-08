package main

import (
	"crypto/md5"
	"encoding/base64"
	"fmt"
	"log"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/miekg/dns"
)

func startDNSListener(socket net.PacketConn) {
	go DNSLoop(socket)
	go sinkSenderOutput()
}

type storageRequest struct {
	storageNotifications chan bool
	content              string
	replications         int
}

var (
	uploadPendingMap      map[string]storageRequest
	uploadPendingMapMutex sync.RWMutex
)

func DNSLoop(socket net.PacketConn) {
	uploadPendingMapMutex.Lock()
	uploadPendingMap = make(map[string]storageRequest)
	uploadPendingMapMutex.Unlock()
	for {
		dnsin := make([]byte, 1500)
		inbytes, inaddr, err := socket.ReadFrom(dnsin)
		if err != nil {
			continue
		}

		inmsg := &dns.Msg{}

		if unpackErr := inmsg.Unpack(dnsin[0:inbytes]); unpackErr != nil {
			log.Printf("Unable to unpack DNS request: %s", unpackErr.Error())
			continue
		}

		if len(inmsg.Question) != 1 {
			log.Printf("More than one quesion in query (%d), droppin %+v", len(inmsg.Question), inmsg)
			continue
		}

		iqn := strings.ToLower(inmsg.Question[0].Name)

		if !strings.Contains(iqn, *dnsbase) {
			continue
		}

		TrackResolverRequest(inaddr.String())

		outmsg := &dns.Msg{}

		queryname := strings.Replace(
			iqn, fmt.Sprintf(".%s.", *dnsbase), "", 1)
		queryname = strings.TrimSuffix(queryname, ".")
		AddLog("Inbound DNS query for chunk '%s' from resolver %s", queryname, inaddr.String())

		ttl := uint32(2147483646)
		content := ""
		uploadPendingMapMutex.Lock()
		req, exists := uploadPendingMap[queryname]
		if !exists || req.content == "" {
			content = "kittens"
			ttl = 1
		} else {
			content = req.content
			req.replications++
			uploadPendingMap[queryname] = req
		}
		uploadPendingMapMutex.Unlock()

		ostring := make([]string, 1)
		ostring[0] = content

		outmsg.Id = inmsg.Id
		outmsg = inmsg.SetReply(outmsg)

		outmsg.Answer = make([]dns.RR, 1)
		outmsg.Answer[0] = &dns.TXT{
			Hdr: dns.RR_Header{
				Name:   iqn,
				Rrtype: dns.TypeTXT,
				Class:  dns.ClassINET,
				Ttl:    ttl},
			Txt: ostring,
		}
		outputb, err := outmsg.Pack()

		if err != nil {
			log.Printf("unable to pack response to thing: %s", err)
			continue
		}

		socket.WriteTo(outputb, inaddr)
	}
}

func verifyNSsetup(name string) bool {
	s, err := net.LookupTXT("tokentest" + name)
	if err != nil {
		return false
	}

	if len(s) != 1 {
		return false
	}

	return true
}

func uploadChunk(filename string, chunk int, data string) {
	endpoints := make([]string, 0)
	queryname := ""
	for replications := 0; replications < 3; replications++ {
		var IP string
		IP, queryname = getDNSserverShard(filename, chunk, replications)
		endpoints = append(endpoints, IP)
	}

	uploadPendingMapMutex.Lock()
	uploadPendingMap[queryname] = storageRequest{
		content:      data,
		replications: 0,
	}
	uploadPendingMapMutex.Unlock()

	var targetEndpoints []string
	if *mockMode {
		targetEndpoints = []string{"127.0.0.1:" + *dnsport}
	} else {
		targetEndpoints = endpoints
	}

	AddLog("Uploading chunk %d (hash: %s) for file '%s' targeting resolvers: %v", chunk, queryname, filename, targetEndpoints)

	m1 := new(dns.Msg)
	m1.Id = dns.Id()
	m1.RecursionDesired = true
	m1.Question = make([]dns.Question, 1)
	m1.Question[0] = dns.Question{
		Name:   fmt.Sprintf("%s.%s.", queryname, *dnsbase),
		Qtype:  dns.TypeTXT,
		Qclass: dns.ClassINET,
	}
	dnspacket, _ := m1.Pack()

	for _, ip := range targetEndpoints {
		var targetAddr string
		if strings.Contains(ip, ":") {
			targetAddr = ip
		} else {
			targetAddr = ip + ":53"
		}
		addr, err := net.ResolveUDPAddr("udp", targetAddr)
		if err != nil {
			AddLog("WARNING: failed to resolve resolver IP %s: %s", ip, err.Error())
			continue
		}
		globalSender.WriteTo(dnspacket, addr)
	}

	if !*mockMode {
		defer func() {
			uploadPendingMapMutex.Lock()
			delete(uploadPendingMap, queryname)
			uploadPendingMapMutex.Unlock()
		}()
	}

	tout := 0
	for {
		time.Sleep(time.Millisecond * 250)
		uploadPendingMapMutex.RLock()
		reps := uploadPendingMap[queryname].replications
		uploadPendingMapMutex.RUnlock()

		if reps != 0 {
			AddLog("Chunk %d (hash: %s) successfully cached (replicated %d times)", chunk, queryname, reps)
			return
		}
		if tout == 10 {
			AddLog("WARNING: Chunk %d (hash: %s) storage check timed out! Data may be lost.", chunk, queryname)
			return
		}

		tout++
	}
}

func fetchFromShard(filename string, chunk int) []byte {
	tempSocket, err := net.ListenPacket("udp4", "0.0.0.0:0")
	if err != nil {
		AddLog("ERROR: failed to listen on UDP for fetch: %s", err.Error())
		return []byte{}
	}
	defer tempSocket.Close()
	endpoints := make([]string, 0)
	queryname := ""

	for replications := 0; replications < 3; replications++ {
		var IP string
		IP, queryname = getDNSserverShard(filename, chunk, replications)
		endpoints = append(endpoints, IP)
	}

	var targetEndpoints []string
	if *mockMode {
		targetEndpoints = []string{"127.0.0.1:" + *dnsport}
	} else {
		targetEndpoints = endpoints
	}

	AddLog("Fetching chunk %d (hash: %s) for file '%s' from endpoints: %v", chunk, queryname, filename, targetEndpoints)

	m1 := new(dns.Msg)
	m1.Id = dns.Id()
	m1.RecursionDesired = true
	m1.Question = make([]dns.Question, 1)
	m1.Question[0] = dns.Question{
		Name:   fmt.Sprintf("%s.%s.", queryname, *dnsbase),
		Qtype:  dns.TypeTXT,
		Qclass: dns.ClassINET,
	}
	dnspacket, _ := m1.Pack()

	for _, endpoint := range targetEndpoints {
		tempSocket.SetReadDeadline(time.Now().Add(time.Millisecond * 400))
		var targetAddr string
		if strings.Contains(endpoint, ":") {
			targetAddr = endpoint
		} else {
			targetAddr = endpoint + ":53"
		}
		addr, err := net.ResolveUDPAddr("udp", targetAddr)
		if err != nil {
			AddLog("WARNING: failed to resolve address %s: %s", endpoint, err.Error())
			continue
		}
		tempSocket.WriteTo(dnspacket, addr)
		dnsraw := make([]byte, 1500)
		bytecount, _, err := tempSocket.ReadFrom(dnsraw)
		if err != nil {
			continue
		}

		msg := &dns.Msg{}
		err = msg.Unpack(dnsraw[:bytecount])

		if err != nil {
			log.Printf("err parsing %s", err)
			continue
		}

		if len(msg.Answer) != 1 {
			continue
		}

		// Okay I'm really sorry but I am not sure right now how to
		// actually get the TXT records so I'm just going to string and
		// regex the output, because that's how poor of a programmer I am.

		so := msg.Answer[0].String()
		bad := strings.Split(so, "\"")
		if len(bad) == 1 {
			continue
		}

		if bad[1] == "kittens" {
			continue
		}

		bytes, err := base64.StdEncoding.DecodeString(bad[1])
		if err != nil {
			continue
		}

		AddLog("Successfully retrieved chunk %d from resolver %s", chunk, endpoint)
		return bytes
	}

	AddLog("ERROR: Failed to retrieve chunk %d (hash: %s) from all resolvers!", chunk, queryname)
	return []byte{}
}

func sinkSenderOutput() {
	for {
		b := make([]byte, 1500)
		globalSender.ReadFrom(b)
	}
}

func getDNSserverShard(filename string, chunk int, copy int) (IP string, query string) {
	key := md5.Sum([]byte(fmt.Sprintf("%s.%d", filename, chunk)))
	hashmini := fmt.Sprintf("%02x%02x%02x%02x%02x%02x",
		key[0], key[1], key[2], key[3], key[4], key[5])
	numberkey, _ := strconv.ParseInt(hashmini, 16, 64)
	IP = ipList[(int(numberkey)+copy)%len(ipList)]
	query = fmt.Sprintf("dfs-%s", hashmini)
	return IP, query
}
