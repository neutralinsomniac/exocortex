package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/neutralinsomniac/exocortex/db"
)

func main() {
	addr := flag.String("addr", ":8765", "listen address")
	dbFile := flag.String("db", "", "path to exocortex.db (required)")
	token := flag.String("token", "", "shared Bearer token for auth (required)")
	certFile := flag.String("cert", "", "TLS certificate file (PEM)")
	keyFile := flag.String("key", "", "TLS key file (PEM)")
	flag.Parse()

	if *dbFile == "" {
		fmt.Fprintln(os.Stderr, "error: -db is required")
		os.Exit(1)
	}
	if *token == "" {
		fmt.Fprintln(os.Stderr, "error: -token is required")
		os.Exit(1)
	}
	if (*certFile == "") != (*keyFile == "") {
		fmt.Fprintln(os.Stderr, "error: -cert and -key must be provided together")
		os.Exit(1)
	}

	// Use symmetric encryption when TLS is not configured.
	encrypt := *certFile == ""

	exoDB := &db.ExoDB{}
	if err := exoDB.Open(*dbFile); err != nil {
		log.Fatal("open db:", err)
	}
	defer exoDB.Close()
	if err := exoDB.LoadSchema(); err != nil {
		log.Fatal("load schema:", err)
	}
	if err := exoDB.Migrate(); err != nil {
		log.Fatal("migrate:", err)
	}

	http.HandleFunc("/sync", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		bodyBytes, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "read body: "+err.Error(), http.StatusBadRequest)
			return
		}
		if encrypt {
			// In symmetric mode, successful decryption proves knowledge of
			// the token — no plaintext Authorization header needed or checked.
			bodyBytes, err = db.DecryptPayload(*token, bodyBytes)
			if err != nil {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
		} else {
			// In TLS mode, the Bearer token in the header is the auth check.
			if r.Header.Get("Authorization") != "Bearer "+*token {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
		}

		var req db.SyncPayload
		if err := json.Unmarshal(bodyBytes, &req); err != nil {
			http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
			return
		}

		// Snapshot server state before applying client changes so we return
		// only what the client doesn't already know about.
		serverPayload, err := exoDB.BuildSyncPayload(req.Since)
		if err != nil {
			http.Error(w, "build payload: "+err.Error(), http.StatusInternalServerError)
			return
		}
		serverPayload.ServerTS = time.Now().UnixNano()

		if err := exoDB.ApplyChanges(req); err != nil {
			http.Error(w, "apply changes: "+err.Error(), http.StatusInternalServerError)
			return
		}

		respBytes, err := json.Marshal(serverPayload)
		if err != nil {
			http.Error(w, "marshal: "+err.Error(), http.StatusInternalServerError)
			return
		}
		if encrypt {
			respBytes, err = db.EncryptPayload(*token, respBytes)
			if err != nil {
				http.Error(w, "encrypt: "+err.Error(), http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/octet-stream")
		} else {
			w.Header().Set("Content-Type", "application/json")
		}
		w.Write(respBytes)
	})

	if *certFile != "" {
		log.Printf("exo-server listening on %s (TLS)", *addr)
		log.Fatal(http.ListenAndServeTLS(*addr, *certFile, *keyFile, nil))
	} else {
		log.Printf("exo-server listening on %s (AES-256-GCM encrypted)", *addr)
		log.Fatal(http.ListenAndServe(*addr, nil))
	}
}
