package main

import (
	"bytes"
	"encoding/binary"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/ringbuf"
)

func NewRingBufReader(m *ebpf.Map) (reader *RingBufReader, err error) {
	reader = new(RingBufReader)
	if reader.reader, err = ringbuf.NewReader(m); err != nil {
		return nil, err
	} else {
		return reader, nil
	}
}

type RingBufReader struct {
	reader *ringbuf.Reader
}

func (r *RingBufReader) Read() (*Packet, error) {
	var pkt observedPacket
	if rec, err := r.reader.Read(); err != nil {
		return nil, err
	} else {
		if err = binary.Read(bytes.NewReader(rec.RawSample), binary.LittleEndian, &pkt); err != nil {
			return nil, err
		} else {
			return pkt.ToPacket(), nil
		}
	}
}

func (r *RingBufReader) Close() error {
	return r.reader.Close()
}
