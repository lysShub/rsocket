package test

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"net"
	"net/netip"
	"sync"
	"testing"
	"time"

	"github.com/lysShub/relraw"
	"github.com/stretchr/testify/require"
	"gvisor.dev/gvisor/pkg/buffer"
	"gvisor.dev/gvisor/pkg/tcpip"
	"gvisor.dev/gvisor/pkg/tcpip/checksum"
	"gvisor.dev/gvisor/pkg/tcpip/header"
	"gvisor.dev/gvisor/pkg/tcpip/link/channel"
	"gvisor.dev/gvisor/pkg/tcpip/network/ipv4"
	"gvisor.dev/gvisor/pkg/tcpip/stack"
	"gvisor.dev/gvisor/pkg/tcpip/transport/tcp"
)

func calcChecksum() func(ipHdr header.IPv4) header.IPv4 {
	var (
		first   = true
		calcIP  = false
		calcTCP = true
	)
	return func(ipHdr header.IPv4) header.IPv4 {
		tcpHdr := header.TCP(ipHdr.Payload())
		if first {
			calcIP = !ipHdr.IsChecksumValid()
			calcTCP = !tcpHdr.IsChecksumValid(
				ipHdr.SourceAddress(),
				ipHdr.DestinationAddress(),
				checksum.Checksum(tcpHdr.Payload(), 0),
				uint16(len(tcpHdr.Payload())),
			)
			first = false
		}

		if calcIP {
			ipHdr.SetChecksum(0)
			s := checksum.Checksum(ipHdr[:ipHdr.HeaderLength()], 0)
			ipHdr.SetChecksum(^s)
		}

		if calcTCP {

			s := header.PseudoHeaderChecksum(
				tcp.ProtocolNumber,
				ipHdr.SourceAddress(),
				ipHdr.DestinationAddress(),
				uint16(len(tcpHdr)),
			)
			s = checksum.Checksum(tcpHdr.Payload(), s)
			tcpHdr.SetChecksum(0)
			s = tcpHdr.CalculateChecksum(s)
			tcpHdr.SetChecksum(^s)
		}
		return ipHdr
	}
}

func BuildTCPSync(t require.TestingT, laddr, raddr netip.AddrPort) header.TCP {
	var b = make(header.TCP, header.TCPMinimumSize)
	b.Encode(&header.TCPFields{
		SrcPort:    uint16(laddr.Port()),
		DstPort:    uint16(raddr.Port()),
		SeqNum:     rand.Uint32(),
		AckNum:     rand.Uint32(),
		DataOffset: header.TCPMinimumSize,
		Flags:      header.TCPFlagAck | header.TCPFlagPsh,
		WindowSize: 83,
		Checksum:   0,
	})

	sum := header.PseudoHeaderChecksum(
		tcp.ProtocolNumber,
		Address(laddr.Addr()), Address(raddr.Addr()),
		uint16(len(b)),
	)
	sum = checksum.Checksum(b, sum)
	b.SetChecksum(sum)

	require.True(t,
		b.IsChecksumValid(
			tcpip.AddrFromSlice(laddr.Addr().AsSlice()),
			tcpip.AddrFromSlice(raddr.Addr().AsSlice()),
			checksum.Checksum(b.Payload(), 0),
			uint16(len(b.Payload())),
		),
	)

	return b
}

func BuildRawTCP(t *testing.T, laddr, raddr netip.AddrPort, payload []byte) header.IPv4 {
	require.True(t, laddr.Addr().Is4())

	iptcp := header.IPv4MinimumSize + header.TCPMinimumSize

	totalSize := iptcp + len(payload)
	var b = make([]byte, totalSize)
	copy(b[iptcp:], payload)

	ts := uint32(time.Now().UnixNano())
	tcphdr := header.TCP(b[header.IPv4MinimumSize:])
	tcphdr.Encode(&header.TCPFields{
		SrcPort:    uint16(laddr.Port()),
		DstPort:    uint16(raddr.Port()),
		SeqNum:     501 + ts,
		AckNum:     ts,
		DataOffset: header.TCPMinimumSize,
		Flags:      header.TCPFlagAck | header.TCPFlagPsh,
		WindowSize: 83,
		Checksum:   0,
	})

	s := relraw.NewIPStack(laddr.Addr(), raddr.Addr(), header.TCPProtocolNumber)
	p := relraw.ToPacket(s.Size(), b)
	s.AttachOutbound(p)

	// psoSum := s.AttachHeader(b, header.TCPProtocolNumber)

	// tcphdr.SetChecksum(^checksum.Checksum(tcphdr, psoSum))

	require.True(t, header.IPv4(b).IsChecksumValid())
	require.True(t,
		tcphdr.IsChecksumValid(
			tcpip.AddrFromSlice(laddr.Addr().AsSlice()),
			tcpip.AddrFromSlice(raddr.Addr().AsSlice()),
			checksum.Checksum(tcphdr.Payload(), 0),
			uint16(len(tcphdr.Payload())),
		),
	)

	return b
}

func RandPort() uint16 {
	p := uint16(rand.Uint32())
	if p < 1024 {
		p += 1536
	} else if p > 0xffff-64 {
		p -= 128
	}
	return p
}

var LocIP = func() netip.Addr {
	c, _ := net.DialUDP("udp", nil, &net.UDPAddr{IP: net.ParseIP("8.8.8.8"), Port: 53})
	return netip.MustParseAddrPort(c.LocalAddr().String()).Addr()
}()

var tunTupleAddrsGener = func() func() []netip.Addr {
	var mu sync.RWMutex
	var addr = netip.AddrFrom4([4]byte{10, 3, 3, 0})

	return func() []netip.Addr {
		mu.Lock()
		defer mu.Unlock()

		var r = []netip.Addr{}
		for len(r) < 2 {
			addr = addr.Next()
			for tail := addr.As4()[3]; tail == 0 || tail == 0xff; {
				addr = addr.Next()
			}
			r = append(r, addr)
		}
		return r
	}
}()

func TCPAddr(a netip.AddrPort) *net.TCPAddr {
	return &net.TCPAddr{IP: a.Addr().AsSlice(), Port: int(a.Port())}
}

func UDPAddr(a netip.AddrPort) *net.UDPAddr {
	return &net.UDPAddr{IP: a.Addr().AsSlice(), Port: int(a.Port())}
}
func Address(a netip.Addr) tcpip.Address {
	return tcpip.AddrFromSlice(a.AsSlice())
}
func FullAddress(a netip.AddrPort) tcpip.FullAddress {
	return tcpip.FullAddress{
		Addr: Address(a.Addr()),
		Port: a.Port(),
	}
}

type ustack struct {
	addr  tcpip.Address
	stack *stack.Stack

	link *channel.Endpoint
}

func NewUstack(t require.TestingT, addr netip.Addr) *ustack {
	require.True(t, addr.Is4())

	laddr := tcpip.AddrFromSlice(addr.AsSlice())
	st := stack.New(stack.Options{
		NetworkProtocols:   []stack.NetworkProtocolFactory{ipv4.NewProtocol},
		TransportProtocols: []stack.TransportProtocolFactory{tcp.NewProtocol},
		// HandleLocal:        true,
	})
	l := channel.New(4, 1536, "")

	const nicid tcpip.NICID = 1234
	err := st.CreateNIC(nicid, l)
	require.Nil(t, err)
	st.AddProtocolAddress(nicid, tcpip.ProtocolAddress{
		Protocol:          header.IPv4ProtocolNumber,
		AddressWithPrefix: laddr.WithPrefix(),
	}, stack.AddressProperties{})
	st.SetRouteTable([]tcpip.Route{{Destination: header.IPv4EmptySubnet, NIC: nicid}})

	var u = &ustack{
		addr:  laddr,
		stack: st,
		link:  l,
	}
	return u
}

func BindRaw(t require.TestingT, ctx context.Context, us *ustack, raw relraw.RawConn) {
	go func() {
		var ip = relraw.ToPacket(0, make([]byte, 1536))
		sum := calcChecksum()
		for {
			select {
			case <-ctx.Done():
				return
			default:
			}
			err := raw.ReadCtx(ctx, ip)
			if errors.Is(err, context.Canceled) {
				return
			}
			require.NoError(t, err)
			if ip.Len() == 0 {
				fmt.Println(0)
			}
			sum(ip.Data()) // todo: TX

			// iphdr := header.IPv4(ip.Bytes())
			// tcphdr := header.TCP(iphdr.Payload())
			// fmt.Printf(
			// 	"%s:%d-->%s:%d	%s\n",
			// 	iphdr.SourceAddress(), tcphdr.SourcePort(),
			// 	iphdr.DestinationAddress(), tcphdr.DestinationPort(),
			// 	tcphdr.Flags(),
			// )

			us.Inject(ip.Data())
		}
	}()

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			default:
			}

			ip := us.Read(ctx)
			if ip == nil {
				return
			}

			// iphdr := header.IPv4(ip)
			// tcphdr := header.TCP(iphdr.Payload())
			// fmt.Printf(
			// 	"%s:%d-->%s:%d	%s\n",
			// 	iphdr.SourceAddress(), tcphdr.SourcePort(),
			// 	iphdr.DestinationAddress(), tcphdr.DestinationPort(),
			// 	tcphdr.Flags(),
			// )

			_, err := raw.Write(ip)
			require.NoError(t, err)
		}
	}()
}

func (u *ustack) Inject(ip []byte) {
	pkb := stack.NewPacketBuffer(stack.PacketBufferOptions{Payload: buffer.MakeWithData(ip)})
	u.link.InjectInbound(header.IPv4ProtocolNumber, pkb)
}

func (u *ustack) Read(ctx context.Context) []byte {
	pkb := u.link.ReadContext(ctx)
	if pkb.IsNil() {
		return nil // ctx cancel
	}
	defer pkb.DecRef()
	return pkb.ToView().AsSlice()
}

func (u *ustack) Addr() tcpip.Address {
	return u.addr
}

func (s *ustack) NetworkProtocolNumber() tcpip.TransportProtocolNumber {
	return header.ICMPv4ProtocolNumber
}

func (s *ustack) Stack() *stack.Stack { return s.stack }