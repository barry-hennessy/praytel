package receiver

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"sync"
	"time"

	"github.com/barry-hennessy/bones/observability"
)

// http2Preface is how every gRPC connection opens. No HTTP/1.x method starts
// with it, and every HTTP/1.x request line is longer, so reading this many
// bytes never blocks an HTTP/1.x client.
var http2Preface = []byte("PRI")

const sniffTimeout = 5 * time.Second

// demux accepts on every port and routes each connection by its first bytes:
// the HTTP/2 preface to the gRPC server, anything else to the HTTP server.
// Exporters often ignore OTEL_EXPORTER_OTLP_PROTOCOL, so any port has to
// serve either protocol.
type demux struct {
	lt observability.LoggingTracer

	outer []net.Listener
	grpc  *subListener
	http  *subListener

	mu      sync.Mutex
	closed  bool
	sniffed map[net.Conn]struct{}
	wg      sync.WaitGroup
}

func newDemux(lt observability.LoggingTracer, outer ...net.Listener) *demux {
	return &demux{
		lt:      lt,
		outer:   outer,
		grpc:    newSubListener(outer[0].Addr()),
		http:    newSubListener(outer[0].Addr()),
		sniffed: map[net.Conn]struct{}{},
	}
}

// accept routes connections from one outer listener until it is closed.
func (d *demux) accept(ctx context.Context, lis net.Listener) error {
	_, log, span := d.lt.Start(ctx)
	defer span.End()

	for {
		conn, err := lis.Accept()
		if errors.Is(err, net.ErrClosed) {
			return nil
		}
		if err != nil {
			log.Warn("otlp accept", "err", err)
			time.Sleep(10 * time.Millisecond)

			continue
		}

		if !d.track(conn) {
			_ = conn.Close()
			return nil
		}

		d.wg.Go(func() { d.route(conn) })
	}
}

func (d *demux) route(conn net.Conn) {
	head := make([]byte, len(http2Preface))

	_ = conn.SetReadDeadline(time.Now().Add(sniffTimeout))
	n, err := io.ReadFull(conn, head)
	_ = conn.SetReadDeadline(time.Time{})

	if !d.untrack(conn) || err != nil {
		_ = conn.Close()
		return
	}

	to := d.http
	if bytes.Equal(head[:n], http2Preface) {
		to = d.grpc
	}

	to.deliver(&sniffedConn{Conn: conn, r: io.MultiReader(bytes.NewReader(head[:n]), conn)})
}

func (d *demux) track(conn net.Conn) bool {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.closed {
		return false
	}
	d.sniffed[conn] = struct{}{}

	return true
}

// untrack reports false when Close already closed conn.
func (d *demux) untrack(conn net.Conn) bool {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.closed {
		return false
	}
	delete(d.sniffed, conn)

	return true
}

// Close stops accepting and drops connections still being sniffed. The sub
// listeners are left for the gRPC and HTTP servers to close as they stop, so
// that neither reports a closed listener as a serve error.
func (d *demux) Close(context.Context) error {
	d.mu.Lock()
	d.closed = true
	for conn := range d.sniffed {
		_ = conn.Close()
	}
	d.mu.Unlock()

	for _, lis := range d.outer {
		_ = lis.Close()
	}
	d.wg.Wait()

	return nil
}

// subListener is the net.Listener a server sees: connections the demux routed
// to it.
type subListener struct {
	addr  net.Addr
	conns chan net.Conn
	done  chan struct{}
	once  sync.Once
}

func newSubListener(addr net.Addr) *subListener {
	return &subListener{
		addr:  addr,
		conns: make(chan net.Conn),
		done:  make(chan struct{}),
	}
}

func (l *subListener) deliver(conn net.Conn) {
	select {
	case l.conns <- conn:
	case <-l.done:
		_ = conn.Close()
	}
}

func (l *subListener) Accept() (net.Conn, error) {
	select {
	case conn := <-l.conns:
		return conn, nil
	case <-l.done:
		return nil, net.ErrClosed
	}
}

func (l *subListener) Close() error {
	l.once.Do(func() { close(l.done) })
	return nil
}

func (l *subListener) Addr() net.Addr {
	return l.addr
}

// sniffedConn replays the bytes read while routing.
type sniffedConn struct {
	net.Conn
	r io.Reader
}

func (c *sniffedConn) Read(p []byte) (int, error) {
	return c.r.Read(p)
}
