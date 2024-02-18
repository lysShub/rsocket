package relraw

type Packet struct {
	//             |    data     |
	//  b  --------+++++++++++++++--------
	//     |  head |             | tail  |
	//
	// 	head = i
	// 	tail = cap(b) - len(b)

	i int
	b []byte
}

func NewPacket(ns ...int) *Packet {
	var (
		head int = defaulfHead
		n    int = defaulfData
		tail int = defaulfTail
	)
	if len(ns) > 0 {
		head = ns[0]
	}
	if len(ns) > 1 {
		n = ns[1]
	}
	if len(ns) > 2 {
		tail = ns[2]
	}

	return &Packet{
		i: head,
		b: make([]byte, head+n, head+n+tail),
	}
}

const (
	defaulfHead = 32
	defaulfData = 64
	defaulfTail = 16
)

func ToPacket(off int, b []byte) *Packet {
	return &Packet{
		i: off,
		b: b,
	}
}

func (p *Packet) Data() []byte {
	return p.b[p.i:]
}

// Head head section size
func (p *Packet) Head() int { return p.i }

// Len data section size
func (p *Packet) Len() int { return len(p.b) - p.i }

// Tail tail section size
func (p *Packet) Tail() int { return cap(p.b) - len(p.b) }

// Attach attach b ahead data-section, use head-section firstly, if head section too short,
// will re-alloc memory.
func (p *Packet) Attach(b []byte) {
	delta := p.i - len(b)
	if delta >= 0 {
		p.i -= copy(p.b[delta:], b)
	} else {
		n := len(p.b) + defaulfHead - delta
		tmp := make([]byte, n, n+defaulfTail)

		i := copy(tmp[defaulfHead:], b)
		copy(tmp[defaulfHead+i:], p.Data())

		p.b = tmp
		p.i = defaulfHead
	}
}

// SetLen set head section size, delta-mem from next section
func (p *Packet) SetHead(head int) {
	_ = p.b[:head]

	n := p.Len()

	p.i = head
	p.b = p.b[:min(head+n, cap(p.b))]
}

// SetLen set data section size, delta-mem from tail section
func (p *Packet) SetLen(n int) {
	_ = p.b[:n+p.i]

	p.b = p.b[:p.i+n]
}

// Sets set head and data section size, delta-mem from next section
func (p *Packet) Sets(head, n int) {
	_ = p.b[:head+n]

	p.i = head
	p.b = p.b[:head+n]
}

func (p *Packet) AllocTail(tail int) bool {
	delta := p.Tail() - tail
	if delta < 0 {
		if tail < defaulfTail {
			tail = defaulfTail
		}
		tmp := make([]byte, len(p.b), len(p.b)+tail)
		copy(tmp, p.b)
		p.b = tmp

		return true
	} else {
		return false
	}
}