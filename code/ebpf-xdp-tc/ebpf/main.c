#include <linux/bpf.h>
#include <linux/pkt_cls.h>

#include "helpers.h"

#define IP_FRAGMENTED 65343

char LICENSE[] SEC("license") = "Dual MIT/GPL";

struct {
    __uint(type, BPF_MAP_TYPE_PERF_EVENT_ARRAY);
} perf_observed_packets SEC(".maps");

struct {
    __uint(type, BPF_MAP_TYPE_RINGBUF);
    __uint(max_entries, 1 << 24);
} ring_observed_packets SEC(".maps");

struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __type(key, sizeof(struct two_tuple));
    __type(value, sizeof(struct two_tuple));
    __uint(max_entries, 1024);
} conn_track SEC(".maps");

SEC("classifier/egress")
int egress(struct __sk_buff *skb) {
    bpf_printk("new packet captured on egress (TC)\n");
    void *data = (void *) (long) skb->data;
    void *data_end = (void *) (long) skb->data_end;

    struct ethhdr *eth = data;

    if ((void *) eth + sizeof(*eth) > data_end) {
        return TC_ACT_OK;
    }

    if(eth->h_proto != ETH_P_IP && eth->h_proto != ETH_P_IPV6) {
        return TC_ACT_OK;
    }

    struct iphdr *iph = data + sizeof(*eth);
    if ((void *) iph + sizeof(*iph) > data_end) {
        return TC_ACT_OK;
    }

    /* do not support fragmented packets as L4 headers may be missing */
    if (iph->frag_off & IP_FRAGMENTED) {
        return TC_ACT_OK;
    }

    if (iph->protocol != IPPROTO_TCP) {
        bpf_printk("Packet's not TCP - forwarding");
        return TC_ACT_OK;
    }

    struct tcphdr *tcp = (void *) iph + sizeof(*iph);
    if ((void *) tcp + sizeof(*tcp) > data_end) {
        return TC_ACT_SHOT;
    }

    struct two_tuple dst = {
            .ip = iph->daddr,
            .port = tcp->dest
    };

    struct two_tuple *orig_src = bpf_map_lookup_elem(&conn_track, &dst);

    if (orig_src == NULL) {
        bpf_printk("No translation found - pass it through");
        return TC_ACT_OK;
    }

    bpf_printk("Restore original source IP");

    iph->saddr = orig_src->ip;
    tcp->source = orig_src->port;

    iph->tos = 7 << 2;
    iph->check = 0;
    iph->check = checksum((unsigned short *) iph, sizeof(struct iphdr));

    return TC_ACT_OK;
};

SEC("classifier/ingress")
int ingress(struct __sk_buff *skb) {
    bpf_printk("new packet captured on ingress (TC)");
    void *data = (void *) (long) skb->data;
    void *data_end = (void *) (long) skb->data_end;

    struct ethhdr *eth = data;

    if ((void *) eth + sizeof(*eth) > data_end) {
        return TC_ACT_OK;
    }

    struct iphdr *iph = data + sizeof(*eth);
    if ((void *) iph + sizeof(*iph) > data_end) {
        return TC_ACT_OK;
    }

    /* do not support fragmented packets as L4 headers may be missing */
    if (iph->frag_off & IP_FRAGMENTED) {
        return TC_ACT_OK;
    }

    if (iph->protocol != IPPROTO_TCP) {
        bpf_printk("Packet's not TCP - forwarding");
        return TC_ACT_OK;
    }

    if (iph->daddr == 16845322) {
        bpf_printk("We're the destination - don't touch it");
        return TC_ACT_OK;
    }

    struct tcphdr *tcp = (void *) iph + sizeof(*iph);
    if ((void *) tcp + sizeof(*tcp) > data_end) {
        return TC_ACT_SHOT;
    }

    struct two_tuple src = {
            .ip = iph->saddr,
            .port = tcp->source
    };

    struct two_tuple dst = {
            .ip = iph->daddr,
            .port = tcp->dest
    };

    bpf_map_update_elem(&conn_track, &src, &dst, 0);

    bpf_printk("Forward packet to localhost (TC)");
    iph->daddr = 16845322;
    iph->tos = 7 << 2;
    iph->check = 0;
    iph->check = checksum((unsigned short *) iph, sizeof(struct iphdr));
    return TC_ACT_OK;
};

static inline enum xdp_action extract_meta(struct xdp_md *ctx, struct observed_packet *pkt) {
    void *data = (void *) (long) ctx->data;
    void *data_end = (void *) (long) ctx->data_end;
    struct ethhdr *eth = data;
    __u16 proto;

    if (data + sizeof(struct ethhdr) > data_end) {
        bpf_printk("Packet apparently not ethernet");
        return XDP_DROP;
    }

    proto = eth->h_proto;
    if (proto != bpf_htons(ETH_P_IP) && proto != bpf_htons(ETH_P_IPV6)) {
        bpf_printk("Not an IP packet");
        return XDP_PASS;
    }

    struct iphdr *iph = data + sizeof(*eth);
    if ((void *) iph + sizeof(struct iphdr) > data_end) {
        return XDP_DROP;
    }

    /* do not support fragmented packets as L4 headers may be missing */
    if (iph->frag_off & IP_FRAGMENTED) {
        return XDP_DROP;
    }

    pkt->sourceIp = iph->saddr;
    pkt->destIp = iph->daddr;

    __u8 ip_proto = iph->protocol;

    if (ip_proto == IPPROTO_TCP) {
        struct tcphdr *tcph = extract_tcp_meta(pkt, (void *) iph, sizeof(struct iphdr), data_end);
        // if ACK flag is set we just pass it through because it belongs to an already established connection
        if (tcph == NULL || tcph->ack) {
            return XDP_PASS;
        }
    } else if (ip_proto == IPPROTO_UDP) {
        struct udphdr *udph = extract_udp_meta(pkt, (void *) iph, sizeof(struct iphdr), data_end);
        // could also check if we're the source
        if (udph == NULL) {
            return XDP_PASS;
        }
    }

    return XDP_PASS;
}

SEC("xdp/perf")
int xdp_ingress_perf(struct xdp_md *ctx) {
    struct observed_packet pkt;

    enum xdp_action action = extract_meta(ctx, &pkt);

    if (pkt.destIp == 0 || pkt.sourceIp == 0) {
        return action;
    }

    if (!bpf_perf_event_output(ctx, &perf_observed_packets, BPF_F_CURRENT_CPU, &pkt, sizeof(struct observed_packet))) {
        bpf_printk("Failed to submit observed packet");
    }

    return XDP_PASS;
}

SEC("xdp/ring")
int xdp_ingress_ring(struct xdp_md *ctx) {
    struct observed_packet pkt = {};

    enum xdp_action action = extract_meta(ctx, &pkt);

    if (pkt.destIp == 0 || pkt.sourceIp == 0) {
        return action;
    }

    bpf_ringbuf_output(&ring_observed_packets, &pkt, sizeof(pkt), 0);

    return XDP_PASS;
}