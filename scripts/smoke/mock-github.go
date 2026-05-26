// mock-github stands in for api.github.com during the argus smoke test.
// It serves /repos/<o>/<r>/commits/<branch> and /repos/<o>/<r>/contents/<path>
// from an in-memory fixture that the orchestrator can flip via /switch/<name>.
package main

import (
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
)

type fixture struct {
	sha     string
	compose string
}

var (
	fixtures = map[string]fixture{
		"good": {
			sha: "0000000000000000000000000000000000000001",
			compose: `services:
  app:
    image: hashicorp/http-echo
    command: ["-listen=:8080", "-text=ok"]
    ports:
      - "8080:8080"
`,
		},
		"bad": {
			// Different SHA forces argus to attempt a new deploy. The compose
			// listens on the wrong port, so the /health probe on 8080 will fail
			// and rollback should fire.
			sha: "0000000000000000000000000000000000000002",
			compose: `services:
  app:
    image: hashicorp/http-echo
    command: ["-listen=:8081", "-text=oops"]
    ports:
      - "8081:8081"
`,
		},
	}

	mu      sync.RWMutex
	current = "good"
)

func main() {
	addr := flag.String("addr", ":17070", "listen address")
	flag.Parse()

	mux := http.NewServeMux()
	mux.HandleFunc("/repos/", reposHandler)
	mux.HandleFunc("/switch/", switchHandler)
	mux.HandleFunc("/state", stateHandler)

	log.Printf("mock-github listening on %s (current fixture: %s)", *addr, current)
	if err := http.ListenAndServe(*addr, mux); err != nil {
		log.Fatal(err)
	}
}

func reposHandler(w http.ResponseWriter, r *http.Request) {
	mu.RLock()
	f := fixtures[current]
	mu.RUnlock()

	switch {
	case strings.Contains(r.URL.Path, "/commits/"):
		_ = json.NewEncoder(w).Encode(map[string]string{"sha": f.sha})
	case strings.Contains(r.URL.Path, "/contents/"):
		_ = json.NewEncoder(w).Encode(map[string]string{
			"content":  base64.StdEncoding.EncodeToString([]byte(f.compose)),
			"encoding": "base64",
		})
	default:
		http.NotFound(w, r)
	}
}

func switchHandler(w http.ResponseWriter, r *http.Request) {
	target := strings.TrimPrefix(r.URL.Path, "/switch/")
	mu.Lock()
	defer mu.Unlock()
	if _, ok := fixtures[target]; !ok {
		http.Error(w, "unknown fixture", http.StatusBadRequest)
		return
	}
	current = target
	fmt.Fprintf(w, "switched to %s (sha=%s)\n", current, fixtures[current].sha)
}

func stateHandler(w http.ResponseWriter, _ *http.Request) {
	mu.RLock()
	defer mu.RUnlock()
	fmt.Fprintf(w, "current=%s sha=%s\n", current, fixtures[current].sha)
}
