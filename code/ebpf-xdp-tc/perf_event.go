package main

import (
	"bytes"
	"encoding/binary"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/perf"
)

func NewPerfEventReader(m *ebpf.Map, perCPUBufferSize int) (reader *PerfEventReader, err error) {
	reader = new(PerfEventReader)
	if reader.reader, err = perf.NewReader(m, perCPUBufferSize); err != nil {
		return nil, err
	} else {
		return reader, nil
	}
}

type PerfEventReader struct {
	reader *perf.Reader
}

func (r *PerfEventReader) Read() (*Packet, error) {
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

func (r *PerfEventReader) Close() error {
	return r.reader.Close()
}
