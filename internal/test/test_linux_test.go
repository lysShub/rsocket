package test_test

import (
	"io"
	"net"
	"net/netip"
	"testing"
	"time"

	"github.com/lysShub/relraw/internal/test"
	"github.com/stretchr/testify/require"
)

func Test_Create_Tuns(t *testing.T) {
	tt, err := test.CreateTunTuple()
	require.NoError(t, err)

	var (
		saddr = netip.AddrPortFrom(tt.Addr1, test.RandPort())
		caddr = netip.AddrPortFrom(tt.Addr2, test.RandPort())
	)

	go func() {
		l, err := net.ListenTCP("tcp", test.TCPAddr(saddr))
		require.NoError(t, err)

		for {
			conn, err := l.AcceptTCP()
			require.NoError(t, err)
			go func() {
				io.Copy(conn, conn)
			}()
		}
	}()
	time.Sleep(time.Second)

	conn, err := net.DialTCP(
		"tcp",
		test.TCPAddr(caddr),
		test.TCPAddr(saddr),
	)
	require.NoError(t, err)

	_, err = conn.Write([]byte("hello world"))
	require.NoError(t, err)

	var b = make([]byte, 64)
	n, err := conn.Read(b)
	require.NoError(t, err)

	require.Equal(t, "hello world", string(b[:n]))
}