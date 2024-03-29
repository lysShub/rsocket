package test

import (
	"context"
	"io"
	"math"
	"math/rand"
	"net/netip"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lysShub/rsocket/conn"
	"github.com/lysShub/rsocket/helper/ipstack"
	"github.com/lysShub/rsocket/packet"
	"github.com/stretchr/testify/require"
	"gvisor.dev/gvisor/pkg/tcpip"
	"gvisor.dev/gvisor/pkg/tcpip/header"
)

type MockRaw struct {
	options
	id            string
	t             require.TestingT
	proto         tcpip.TransportProtocolNumber
	local, remote netip.AddrPort
	ip            *ipstack.IPStack

	in       chan header.IPv4
	out      chan<- header.IPv4
	closed   bool
	closedMu sync.RWMutex
}

type options struct {
	*conn.Config

	validAddr     bool
	validChecksum bool
	delay         time.Duration
	pl            float32
}

var defaultOptions = options{
	Config: conn.Options(),

	validAddr:     false,
	validChecksum: false,
	delay:         0,
	pl:            0,
}

type Option func(*options)

func RawOpts(opts ...conn.Option) Option {
	return func(o *options) {
		o.Config = conn.Options(opts...)
	}
}

func ValidAddr(o *options) {
	o.validAddr = true
}

func ValidChecksum(o *options) {
	o.validChecksum = true
}

// todo:
// func Delay(delay time.Duration) Option {
// 	return func(o *options) {
// 		o.delay = delay
// 	}
// }

func PacketLoss(pl float32) Option {
	return func(o *options) {
		o.pl = pl
	}
}

func NewMockRaw(
	t require.TestingT,
	proto tcpip.TransportProtocolNumber,
	clientAddr, serverAddr netip.AddrPort,
	opts ...Option,
) (client, server *MockRaw) {
	require.True(t, clientAddr.Addr().Is4())

	var a = make(chan header.IPv4, 16)
	var b = make(chan header.IPv4, 16)
	var err error

	client = &MockRaw{
		options: defaultOptions,
		id:      "client",
		t:       t,
		local:   clientAddr,
		remote:  serverAddr,
		proto:   proto,
		out:     a,
		in:      b,
	}
	for _, opt := range opts {
		opt(&client.options)
	}
	client.ip, err = ipstack.New(
		client.local.Addr(), client.remote.Addr(),
		proto,
		client.options.Config.IPStack.Unmarshal(),
	)
	require.NoError(t, err)

	server = &MockRaw{
		options: defaultOptions,
		id:      "server",
		t:       t,
		local:   serverAddr,
		remote:  clientAddr,
		proto:   proto,
		out:     b,
		in:      a,
	}
	for _, opt := range opts {
		opt(&client.options)
	}
	server.ip, err = ipstack.New(
		server.local.Addr(), server.remote.Addr(),
		proto,
		server.options.Config.IPStack.Unmarshal(),
	)
	require.NoError(t, err)

	for _, opt := range opts {
		opt(&client.options)
		opt(&server.options)
	}
	return client, server
}

var _ conn.RawConn = (*MockRaw)(nil)

func (r *MockRaw) Close() error {
	r.closedMu.Lock()
	defer r.closedMu.Unlock()
	r.closed = true
	close(r.out)
	return nil
}

func (r *MockRaw) Read(ctx context.Context, p *packet.Packet) (err error) {
	var ip header.IPv4
	ok := false
	select {
	case <-ctx.Done():
		return ctx.Err()
	case ip, ok = <-r.in:
		if !ok {
			return io.EOF
		}
	}
	// r.valid(ip, true)

	b := p.Data()
	b = b[:cap(b)]
	n := copy(b, ip)
	if n < len(ip) {
		return io.ErrShortBuffer
	}

	p.SetLen(n)
	switch header.IPVersion(b) {
	case 4:
		iphdr := int(header.IPv4(b).HeaderLength())
		p.SetHead(p.Head() + iphdr)
	case 6:
		p.SetHead(p.Head() + header.IPv6MinimumSize)
	default:
		panic("")
	}

	return nil
}
func (r *MockRaw) Write(ctx context.Context, p *packet.Packet) (err error) {
	r.closedMu.RLock()
	defer r.closedMu.RUnlock()
	if r.closed {
		return os.ErrClosed
	}

	// r.valid(p.Data(), false)
	if r.loss() {
		return nil
	}

	r.ip.AttachOutbound(p)

	tmp := make([]byte, p.Len())
	copy(tmp, p.Data())
	select {
	case <-ctx.Done():
		return ctx.Err()
	case r.out <- tmp:
	}
	return nil
}
func (r *MockRaw) Inject(ctx context.Context, p *packet.Packet) (err error) {
	// r.valid(p.Data(), true)

	var tmp = make([]byte, p.Len())
	copy(tmp, p.Data())

	defer func() {
		if recover() != nil {
			err = os.ErrClosed
		}
	}()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case r.in <- tmp:
		return nil
	}
}
func (r *MockRaw) LocalAddr() netip.AddrPort  { return r.local }
func (r *MockRaw) RemoteAddr() netip.AddrPort { return r.remote }

func (r *MockRaw) loss() bool {
	if r.pl < 0.000001 {
		return false
	}
	return rand.Uint32() <= uint32(float32(math.MaxUint32)*r.pl)
}

// func (r *MockRaw) valid(ip header.IPv4, inboud bool) {
// 	r.validAddr(ip, inboud)
// 	r.validChecksum(ip)
// }

// func (r *MockRaw) validChecksum(ip header.IPv4) {
// 	if !r.options.validChecksum {
// 		return
// 	}

// 	ValidIP(r.t, ip)
// }

// func (r *MockRaw) validAddr(ip header.IPv4, inbound bool) {
// 	if !r.options.validAddr {
// 		return
// 	}
// 	require.Equal(r.t, r.proto, ip.TransportProtocol())

// 	var tp header.Transport
// 	switch ip.TransportProtocol() {
// 	case header.TCPProtocolNumber:
// 		tp = header.TCP(ip.Payload())
// 	case header.UDPProtocolNumber:
// 		tp = header.UDP(ip.Payload())
// 	case header.ICMPv4ProtocolNumber:
// 		tp = header.ICMPv4(ip.Payload())
// 	case header.ICMPv6ProtocolNumber:
// 		tp = header.ICMPv6(ip.Payload())
// 	default:
// 		panic("")
// 	}

// 	src := netip.AddrPortFrom(
// 		netip.AddrFrom4(ip.SourceAddress().As4()),
// 		tp.SourcePort(),
// 	)
// 	dst := netip.AddrPortFrom(
// 		netip.AddrFrom4(ip.DestinationAddress().As4()),
// 		tp.DestinationPort(),
// 	)
// 	if inbound {
// 		require.Equal(r.t, r.remote, src)
// 		require.Equal(r.t, r.local, dst)
// 	} else {
// 		require.Equal(r.t, r.local, src)
// 		require.Equal(r.t, r.remote, dst)
// 	}
// }

type MockListener struct {
	addr netip.AddrPort
	raws chan conn.RawConn

	closed atomic.Bool
}

var _ conn.Listener = (*MockListener)(nil)

func NewMockListener(t require.TestingT, raws ...conn.RawConn) *MockListener {
	var addr = raws[0].LocalAddr()
	for _, e := range raws {
		require.Equal(t, addr, e.LocalAddr())
	}

	var l = &MockListener{
		addr: addr,
		raws: make(chan conn.RawConn, len(raws)),
	}
	for _, e := range raws {
		l.raws <- e
	}
	return l
}

func (l *MockListener) Accept() (conn.RawConn, error) {
	raw, ok := <-l.raws
	if !ok || l.closed.Load() {
		return nil, os.ErrClosed
	}
	return raw, nil
}

func (l *MockListener) Addr() netip.AddrPort { return l.addr }
func (l *MockListener) Close() error {
	if l.closed.CompareAndSwap(false, true) {
		close(l.raws)
	}
	return nil
}
