package main

import (
	"bytes"
	"context"
	_ "embed"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"

	manager "github.com/DataDog/ebpf-manager"
	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/features"
)

var (
	//go:embed ebpf/bin/probe.o
	ebpfTCProgram []byte

	xdpMonitorPerf = &manager.Probe{
		ProbeIdentificationPair: manager.ProbeIdentificationPair{
			EBPFSection:  "xdp/perf",
			EBPFFuncName: "xdp_ingress_perf",
		},
		Ifname:           "eth0",
		NetworkDirection: manager.Ingress,
	}

	xdpMonitorRing = &manager.Probe{
		ProbeIdentificationPair: manager.ProbeIdentificationPair{
			EBPFSection:  "xdp/ring",
			EBPFFuncName: "xdp_ingress_ring",
		},
		Ifname:           "eth0",
		NetworkDirection: manager.Ingress,
	}

	mgr = &manager.Manager{
		Probes: []*manager.Probe{
			{
				ProbeIdentificationPair: manager.ProbeIdentificationPair{
					EBPFSection:  "classifier/egress",
					EBPFFuncName: "egress",
				},
				Ifname:           "eth0",
				NetworkDirection: manager.Egress,
			},
			{
				ProbeIdentificationPair: manager.ProbeIdentificationPair{
					EBPFSection:  "classifier/ingress",
					EBPFFuncName: "ingress",
				},
				Ifname:           "eth0",
				NetworkDirection: manager.Ingress,
			},
		},
	}
)

type (
	packetMonitorMode uint8
	packetReader      interface {
		Read() (*Packet, error)
		Close() error
	}
)

const (
	packetMonitorModeRing packetMonitorMode = iota
	packetMonitorModePerfEvent
)

func main() {
	var monitorMode packetMonitorMode
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, os.Kill)
	defer cancel()

	mgrOpts := manager.Options{
		ExcludedFunctions: nil,
		RLimit:            nil,
	}

	if err := features.HaveMapType(ebpf.RingBuf); err != nil {
		if errors.Is(err, ebpf.ErrNotSupported) {
			log.Println("Falling back to perf event reader")
			mgr.Probes = append(mgr.Probes, xdpMonitorPerf)
			monitorMode = packetMonitorModePerfEvent
			mgrOpts.ExcludedFunctions = append(mgrOpts.ExcludedFunctions, xdpMonitorRing.EBPFFuncName)
		} else {
			log.Fatalf("God knows what happened: %v\n", err)
		}
	} else {
		log.Println("Using fancy new ringbuf reader")
		mgr.Probes = append(mgr.Probes, xdpMonitorRing)
		monitorMode = packetMonitorModeRing
		mgrOpts.ExcludedFunctions = append(mgrOpts.ExcludedFunctions, xdpMonitorPerf.EBPFFuncName)
	}

	if err := mgr.InitWithOptions(bytes.NewReader(ebpfTCProgram), mgrOpts); err != nil {
		log.Fatalf("Failed to init manager: %v", err)
	}

	if err := mgr.Start(); err != nil {
		log.Fatalf("Failed to start manager: %v", err)
	}

	runHTTPServer()

	var reader packetReader
	switch monitorMode {
	case packetMonitorModeRing:
		if r, err := createRingBufReader(mgr); err != nil {
			log.Fatalf("Failed to create rinbuf reader: %v\n", err)
		} else {
			reader = r
		}
	case packetMonitorModePerfEvent:
		if r, err := createPerfEventReader(mgr); err != nil {
			log.Fatalf("Failed to create perf_event reader: %v\n", err)
		} else {
			reader = r
		}
	}

	go logEventsFromReader(ctx, reader)

	<-ctx.Done()

	if err := mgr.Stop(manager.CleanAll); err != nil {
		log.Fatalf("Failed to stop manager: %v", err)
	}
}

func logEventsFromReader(ctx context.Context, reader packetReader) {
	log.Println("Waiting for received packets")
	defer func() {
		if err := reader.Close(); err != nil {
			log.Fatalf("Failed to close reader: %v\n", err)
		}
	}()
	for ctx.Err() == nil {
		if pkt, err := reader.Read(); err != nil {
			log.Printf("Error occurred while reading packet: %v\n", err)
		} else {
			log.Println(pkt)
		}
	}
}

func createRingBufReader(mgr *manager.Manager) (packetReader, error) {
	if m, present, err := mgr.GetMap("ring_observed_packets"); err != nil {
		return nil, err
	} else if !present {
		return nil, fmt.Errorf("ring_observed_packets map not loaded")
	} else {
		return NewRingBufReader(m)
	}

}

func createPerfEventReader(mgr *manager.Manager) (packetReader, error) {
	if m, present, err := mgr.GetMap("perf_observed_packets"); err != nil {
		return nil, err
	} else if !present {
		return nil, errors.New("perf_observed_packets map not loaded")
	} else {
		return NewPerfEventReader(m, 8)
	}
}

func runHTTPServer() {
	log.Println("Listening on: 0.0.0.0:80")
	go func() {
		err := http.ListenAndServe("0.0.0.0:80", http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			log.Println("Handling request")
			writer.WriteHeader(200)
			_, _ = writer.Write([]byte("Hello, world!"))
		}))

		if err != nil {
			if errors.Is(err, http.ErrServerClosed) {
				return
			}
			log.Printf("Error serving HTTP: %v", err)
		}
	}()
}
