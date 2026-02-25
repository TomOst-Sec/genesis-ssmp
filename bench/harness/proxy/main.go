package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
)

func main() {
	listen := flag.String("listen", ":9999", "proxy listen address")
	target := flag.String("target", "https://api.anthropic.com", "upstream API URL")
	outputDir := flag.String("output-dir", "/tmp/bench/runs", "directory for JSONL output")
	runID := flag.String("run-id", "run-001", "run identifier tag")
	tool := flag.String("tool", "claude", "tool being proxied (claude|genesis)")
	saveBodies := flag.Bool("save-bodies", false, "save gzipped request/response bodies")
	flag.Parse()

	if err := os.MkdirAll(*outputDir, 0o755); err != nil {
		log.Fatalf("create output dir: %v", err)
	}

	jsonlPath := filepath.Join(*outputDir, fmt.Sprintf("%s-%s.jsonl", *tool, *runID))
	recorder, err := NewRecorder(jsonlPath, *runID, *tool)
	if err != nil {
		log.Fatalf("create recorder: %v", err)
	}
	defer recorder.Close()

	proxy, err := NewProxyServer(*target, recorder, *saveBodies)
	if err != nil {
		log.Fatalf("create proxy: %v", err)
	}

	log.Printf("[bench-proxy] listening on %s -> %s (run=%s tool=%s output=%s)",
		*listen, *target, *runID, *tool, jsonlPath)

	if err := http.ListenAndServe(*listen, proxy.Handler()); err != nil {
		log.Fatalf("serve: %v", err)
	}
}
