// Command offstage-relay takes beta reports from testers and files them in
// a private Plane project. See internal/relay.
//
// Environment:
//
//	RELAY_LISTEN       address to listen on (default :8080)
//	PLANE_URL          Plane base URL, for example https://plane.example.com
//	PLANE_WORKSPACE    workspace slug
//	PLANE_PROJECT      project UUID
//	PLANE_IDENTIFIER   project identifier, for example OFS
//	PLANE_TOKEN_FILE   file that holds the bot's API token
//	RELAY_KEYS_FILE    file with one "tester sha256-of-key" line per tester
//	RELAY_DATA         directory for the store (default /data)
//
// "offstage-relay hash" reads a key on stdin and prints its keys-file hash.
package main

import (
	"bufio"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/hexadecimil/offstage/internal/relay"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "hash" {
		line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
		fmt.Println(relay.HashKey(strings.TrimSpace(line)))
		return
	}
	need := func(k string) string {
		v := os.Getenv(k)
		if v == "" {
			log.Fatalf("%s is not set", k)
		}
		return v
	}
	token, err := os.ReadFile(need("PLANE_TOKEN_FILE"))
	if err != nil {
		log.Fatal(err)
	}
	keys, err := relay.LoadKeys(need("RELAY_KEYS_FILE"))
	if err != nil {
		log.Fatal(err)
	}
	dir := os.Getenv("RELAY_DATA")
	if dir == "" {
		dir = "/data"
	}
	store, err := relay.OpenStore(filepath.Join(dir, "store.json"))
	if err != nil {
		log.Fatal(err)
	}
	srv := &relay.Server{
		Plane: &relay.Plane{
			URL:       strings.TrimRight(need("PLANE_URL"), "/"),
			Workspace: need("PLANE_WORKSPACE"),
			Project:   need("PLANE_PROJECT"),
			Token:     strings.TrimSpace(string(token)),
		},
		Store:      store,
		Keys:       keys,
		Identifier: need("PLANE_IDENTIFIER"),
	}
	// RELAY_HOOK_FILE holds the URL of the automatic fix hook. The URL
	// carries a token, so it stays out of the environment and the logs.
	if f := os.Getenv("RELAY_HOOK_FILE"); f != "" {
		hook, err := os.ReadFile(f)
		if err != nil {
			log.Fatal(err)
		}
		srv.Hook = strings.TrimSpace(string(hook))
	}
	addr := os.Getenv("RELAY_LISTEN")
	if addr == "" {
		addr = ":8080"
	}
	log.Printf("offstage-relay on %s, %d testers", addr, len(keys))
	hs := &http.Server{Addr: addr, Handler: srv, ReadTimeout: 2 * time.Minute, WriteTimeout: 2 * time.Minute}
	log.Fatal(hs.ListenAndServe())
}
