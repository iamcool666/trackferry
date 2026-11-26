package main

import (
	"context"
	"crypto/tls"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/grandcat/zeroconf"

	"trackferry/internal/store"
	"trackferry/internal/tv"
)

type tvListener struct {
	net.Listener
	key    tls.Certificate
	pairer *tv.Pairer
}

func listenTV(address, private string, st *store.Store, log *slog.Logger) *tvListener {
	if address == "off" || address == "" {
		return nil
	}
	ln, err := net.Listen("tcp", address)
	if err != nil {
		log.Warn("电视　未能打开连接端口", "原因", err)
		return nil
	}
	cert, pin, err := tv.Identity(filepath.Join(private, "tv-key.der"))
	if err != nil {
		ln.Close()
		log.Warn("电视　无法加载服务身份", "原因", err)
		return nil
	}
	return &tvListener{ln, cert, tv.NewPairer(pin, serverName(os.Hostname), st)}
}

// serverName is what a television shows for this server: the machine's own name, as the owner
// knows it from the NAS. A container that does not share the host's network gets a random id
// for a name; that and an empty name read as plain "TrackFerry".
func serverName(hostname func() (string, error)) string {
	name, err := hostname()
	name = strings.TrimSuffix(strings.TrimSpace(name), ".local")
	if err != nil || name == "" || strings.EqualFold(name, "localhost") || containerID.MatchString(name) || utf8.RuneCountInString(name) > 40 {
		return "TrackFerry"
	}
	return name
}

var containerID = regexp.MustCompile(`^[0-9a-f]{12}([0-9a-f]{52})?$`)

func (t *tvListener) serve(ctx context.Context, handler http.Handler, log *slog.Logger) func() {
	srv := &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second,
		IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 << 10,
		TLSConfig:   &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{t.key}},
		BaseContext: func(net.Listener) context.Context { return ctx },
	}
	announceCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() { defer close(done); t.announce(announceCtx, log) }()
	go func() {
		if err := srv.ServeTLS(t.Listener, "", ""); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Warn("电视　连接服务已停止", "原因", err)
			cancel()
		}
	}()
	return func() {
		cancel()
		srv.Close()
		<-done
	}
}

func (t *tvListener) announce(ctx context.Context, log *slog.Logger) {
	addr := t.Addr().(*net.TCPAddr)
	if addr.IP.IsLoopback() {
		return
	}
	var current *zeroconf.Server
	defer func() {
		if current != nil {
			current.Shutdown()
		}
	}()
	var previous []string
	refresh := func() {
		addrs, err := net.InterfaceAddrs()
		if err != nil {
			return
		}
		ips := []string{}
		for _, a := range addrs {
			ip, _, err := net.ParseCIDR(a.String())
			if err == nil && ip.IsGlobalUnicast() && (addr.IP.IsUnspecified() || addr.IP.Equal(ip)) {
				ips = append(ips, ip.String())
			}
		}
		slices.Sort(ips)
		if current != nil && slices.Equal(previous, ips) {
			return
		}
		if current != nil {
			current.Shutdown()
			current = nil
		}
		if len(ips) == 0 {
			return
		}
		// The instance name stays unique per server; the name to show and the version travel
		// in the record, so the television can list servers before it connects to any.
		current, err = zeroconf.RegisterProxy("TrackFerry "+t.pairer.Pin[:6], "_trackferry._tcp", "local.", addr.Port,
			"trackferry-"+t.pairer.Pin[:12]+".local.", ips,
			[]string{"protocol=1", "id=" + t.pairer.Pin, "name=" + t.pairer.Name, "version=" + version}, nil)
		if err != nil {
			log.Debug("电视　局域网广播暂不可用", "原因", err)
			return
		}
		previous = ips
	}
	refresh()
	tick := time.NewTicker(15 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			refresh()
		}
	}
}
