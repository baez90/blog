package main

import (
	"encoding/binary"
	"net"
)

type Protocol uint8

const (
	ProtocolTCP Protocol = iota
	ProtocolUDP
)

type Packet struct {
	SourceIP   net.IP
	DestIP     net.IP
	SourcePort uint16
	DestPort   uint16
	Transport  Protocol
}

type observedPacket struct {
	SourceIP   uint32
	DestIP     uint32
	SourcePort uint16
	DestPort   uint16
	Transport  Protocol
}

func (p *observedPacket) ToPacket() *Packet {
	return &Packet{
		SourceIP:   int2ip(p.SourceIP),
		DestIP:     int2ip(p.DestIP),
		SourcePort: p.SourcePort,
		DestPort:   p.DestPort,
		Transport:  p.Transport,
	}
}

func int2ip(nn uint32) net.IP {
	ip := make(net.IP, net.IPv4len)
	binary.LittleEndian.PutUint32(ip, nn)
	return ip
}

func ip2int(ip net.IP) uint32 {
	b := make([]byte, net.IPv4len)
	copy(b, ip.To4())
	reverse(b)
	return binary.BigEndian.Uint32(b)
}

func reverse(input []byte) {
	for i := 0; i < len(input)/2; i++ {
		input[i], input[len(input)-1-i] = input[len(input)-1-i], input[i]
	}
}
