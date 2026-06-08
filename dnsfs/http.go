package main

import (
	"encoding/base64"
	"fmt"
	"io/ioutil"
	"net/http"
	"time"
)

func handleDownload(rw http.ResponseWriter, req *http.Request) {
	if req.URL.Query().Get("name") == "" {
		http.Error(rw, "Please supply a file name as ?name=", http.StatusBadRequest)
		return
	}
	filename := req.URL.Query().Get("name")
	AddLog("Downloading file '%s'...", filename)
	chunk := 0
	for {
		o := fetchFromShard(filename, chunk)
		if len(o) == 0 {
			if chunk == 0 {
				AddLog("ERROR: File '%s' not found or empty.", filename)
				http.Error(rw, "File not found or empty", http.StatusNotFound)
				return
			}
			AddLog("Finished downloading file '%s' (%d chunks retrieved).", filename, chunk)
			return
		}
		chunk++
		_, err := rw.Write(o)
		if err != nil {
			AddLog("Download of '%s' aborted by client: %s", filename, err.Error())
			return
		}
	}
}

func handleUpload(rw http.ResponseWriter, req *http.Request) {
	if req.URL.Query().Get("name") == "" {
		http.Error(rw, "Please supply a file name as ?name=", http.StatusBadRequest)
		return
	}
	filename := req.URL.Query().Get("name")

	fullfile, err := ioutil.ReadAll(req.Body)
	if err != nil {
		http.Error(rw, "Unable to read data that was submitted", http.StatusInternalServerError)
		return
	}

	chunkcount := (len(fullfile) + 179) / 180
	AddLog("Starting upload for file '%s' (%d bytes, %d chunks)...", filename, len(fullfile), chunkcount)

	var submissionSlice []byte
	for bytePos := 0; bytePos < len(fullfile); bytePos = bytePos + 180 {
		if bytePos+180 > len(fullfile) {
			submissionSlice = fullfile[bytePos:]
		} else {
			submissionSlice = fullfile[bytePos : bytePos+180]
		}

		b64string := base64.StdEncoding.EncodeToString(submissionSlice)

		go uploadChunk(filename, bytePos/180, b64string)
		time.Sleep(time.Millisecond * 100)
	}

	TrackFile(filename, int64(len(fullfile)), chunkcount)
	rw.Header().Set("Content-Type", "application/json")
	rw.Write([]byte(fmt.Sprintf(`{"status":"success","chunks":%d}`, chunkcount)))
}
