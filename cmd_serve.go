package main

import (
	"flag"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
)

func runServe(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	port := fs.String("port", "8000", "port to listen on")
	if err := fs.Parse(args); err != nil {
		return err
	}

	cwd, err := os.Getwd()
	if err != nil {
		return err
	}

	wwwDir := filepath.Join(cwd, "dist", "www")
	if _, err := os.Stat(wwwDir); os.IsNotExist(err) {
		return fmt.Errorf("dist/www not found — run  fz build --web  first")
	}

	handler := coiHandler(http.FileServer(http.Dir(wwwDir)))
	addr := ":" + *port
	fmt.Printf("Serving dist/www on http://localhost%s\n", addr)
	fmt.Println("Press Ctrl+C to stop.")
	return http.ListenAndServe(addr, handler)
}

// coiHandler wraps a handler and adds the headers required for cross-origin
// isolation, which browsers need before they will expose SharedArrayBuffer.
func coiHandler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cross-Origin-Opener-Policy", "same-origin")
		w.Header().Set("Cross-Origin-Embedder-Policy", "require-corp")
		next.ServeHTTP(w, r)
	})
}
