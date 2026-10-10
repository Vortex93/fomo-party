package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"familyparty/internal/server"
	"familyparty/web"
)

func main() {
	addr := flag.String("addr", "0.0.0.0:8080", "HTTP listen address")
	publicURL := flag.String("public-url", os.Getenv("PUBLIC_URL"), "optional share origin for QR codes (e.g. http://192.168.1.5:8080)")
	internetURL := flag.String("internet-url", server.DefaultInternetURL, "HTTPS page used by PeerJS internet invites")
	flag.Parse()
	if err := server.ValidatePublicURL(*publicURL); err != nil {
		log.Fatal(err)
	}
	app := server.New(web.Assets, *publicURL)
	if err := app.ConfigurePeers(*internetURL, os.Getenv("FOMO_ICE_SERVERS")); err != nil {
		log.Fatal(err)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	go app.Run(ctx)
	httpServer := &http.Server{
		Addr: *addr, Handler: app,
		ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second,
		// No global write timeout: SSE is long-lived. Each stream write has its
		// own deadline in events(). Reverse proxies must disable buffering.
		IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 * 1024,
	}
	listener, err := net.Listen("tcp", *addr)
	if err != nil {
		log.Fatal(err)
	}
	_, port, _ := net.SplitHostPort(listener.Addr().String())
	fmt.Printf("\n  FAMILY PARTY\n  Open http://localhost:%s\n", port)
	bindHost, _, _ := net.SplitHostPort(listener.Addr().String())
	bindIP := net.ParseIP(bindHost)
	if *publicURL != "" {
		fmt.Printf("  Share %s with your family.\n", *publicURL)
	} else if bindIP != nil && bindIP.IsLoopback() {
		fmt.Println("  Loopback-only: use -addr 0.0.0.0:8080 to allow phones on your private Wi-Fi.")
	} else {
		addresses, _ := net.InterfaceAddrs()
		for _, address := range addresses {
			network, ok := address.(*net.IPNet)
			if ok && !network.IP.IsLoopback() && network.IP.To4() != nil {
				fmt.Printf("  On the same Wi-Fi: http://%s:%s\n", network.IP, port)
			}
		}
		fmt.Println("  For phone QR codes, open the Wi-Fi address (not localhost).")
	}
	fmt.Println("  Internet invites use PeerJS: create a room and keep its host browser tab open.\n  STUN is enabled; configure FOMO_ICE_SERVERS for a TURN relay when required.")
	fmt.Println("  Rooms live in memory. Stopping the server clears them.\n  Press Ctrl+C to stop.")
	go func() {
		<-ctx.Done()
		app.Hub.Close()
		stopCtx, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		_ = httpServer.Shutdown(stopCtx)
	}()
	if err := httpServer.Serve(listener); !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}
