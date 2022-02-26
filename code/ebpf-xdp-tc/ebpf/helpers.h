#include <linux/if_ether.h>
#include <linux/ip.h>
#include <linux/in.h>
#include <linux/tcp.h>
#include <linux/udp.h>

#include <bpf/bpf_helpers.h>
#include <bpf/bpf_endian.h>

#include "types.h"

static inline unsigned short checksum(unsigned short *buf, int bufsz) {
    unsigned long sum = 0;

    while (bufsz > 1) {
        sum += *buf;
        buf++;
        bufsz -= 2;
    }

    if (bufsz == 1) {
        sum += *(unsigned char *) buf;
    }

    sum = (sum & 0xffff) + (sum >> 16);
    sum = (sum & 0xffff) + (sum >> 16);

    return ~sum;
}

static inline struct tcphdr *extract_tcp_meta(struct observed_packet *pkt, void *iph, __u64 off, void *data_end) {
    struct tcphdr *hdr = iph + off;
    if ((void *) hdr + sizeof(*hdr) > data_end) {
        return NULL;
    }
    pkt->transport_proto = TCP;
    pkt->sourcePort = bpf_ntohs(hdr->source);
    pkt->destPort = bpf_ntohs(hdr->dest);

    return hdr;
}

static inline struct udphdr *extract_udp_meta(struct observed_packet *pkt, void *iph, __u64 off, void *data_end) {
    struct udphdr *hdr = iph + off;
    if ((void *) hdr + sizeof(*hdr) > data_end) {
        return NULL;
    }

    pkt->transport_proto = UDP;
    pkt->sourcePort = bpf_ntohs(hdr->source);
    pkt->destPort = bpf_ntohs(hdr->dest);
    return hdr;
}
