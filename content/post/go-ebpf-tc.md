+++
author = "Peter Kurfer"
title = "eBPF traffic control a.k.a tc with Go"
date = "2022-02-24"
description = "Build your own DNAT with eBPF traffic control and Go"
tags = [
    "golang",
    "ebpf",
    "tc"
]
+++

While working on my 'pet project' [INetMock](https://gitlab.com/inetmock/inetmock) I realized early that it would be amazing to be able to monitor traffic that 'flows by' to see not only traffic I could actually handle but also to get some information which high ports were requested.
Unfortunately back then I was only aware of PCAP capturing and it seemed rather complicated to implement what I wanted on top of PCAPs - or at least rather computation intense.

## eBPF for the rescue

At work I'm part of the platform team maintaining a bunch of Kubernetes clusters and this is where I first stumbled upon eBPF when we migrated from Calico to Cilium.
So you might ask what is eBPF?!
Very short answer: [RTFM](https://ebpf.io/what-is-ebpf) :nerd:

eBPF is the abbreviation for 'Extended Berkeley Packet Filter'.
While originally intended to be used for network traffic analysis it got some major upgrades over the past years so that you can now:

* monitor/control syscalls like [Falco](https://sysdig.com/opensource/falco/) is doing it
* implement a very mighty [keylogger](https://arighi.blogspot.com/2018/12/linux-easy-keylogger-with-ebpf.html)
* [build DDoS attack prevention systems](https://blog.cloudflare.com/l4drop-xdp-ebpf-based-ddos-mitigations/)
* [L4 load balancers](https://blog.cloudflare.com/unimog-cloudflares-edge-load-balancer/)
* [Making a firewall using eBPFs and cgroups](https://nfil.dev/coding/security/ebpf-firewall-with-cgroups/)
* ...

After some initial research I figured using XDP (eXpress Data Path) would be best to implement the kind of monitoring I had in mind.
XDP is also used by Cloudflare for their DDoS prevention system and for their load balancer hence what's good enough for them could be good enough for me (irony intended).
So I was looking for some 'examples' a.k.a. code I can copy and adopt to get started quickly.
I stumbled upon a project monitoring that monitored the API server traffic in a Kubernetes cluster and this was a lot like what I wanted to do!
At least it looked like it at this point.
It took me a few evening to get my prototype working and I read a lot more about the topic whenever possible.

Coincidentally we had a firewall/network misconfiguration issue at work at the same time when I tried to get my first prototype live in the staging environment and it hit me:
 why not using ebpf/XDP not only for monitoring but also to get rid of `iptables` and build my own 'firewall'! :heart_eyes:
Don't misunderstand me, `iptables` is a perfectly fine and perfectly working solution and it served me well!
But it's always a bit annoying when I start the server and forgot to run the ominous `iptables` command I originally copied from the _INetSim_ docs that takes care of the DNATing and suddenly nothing works.

## 1st approach: XDP

To be honest: I used and configured both SNAT and DNAT often in the past and I had a basic idea how this all works but...this 'tiny bit of knowledge' rapidly crumbled into nothing as soon as I tried to build it myself.

The idea is simple.
In general the communication of a client with an external server looks like this:

{{<mermaid>}}
%%{init: {'theme': 'neutral'}}%%
flowchart LR
    client([Client]) --> |IP packet|router{{Router}}
    router --> |IP packet|srv>Server]
    srv -->|response|router
    router-->|esponse|client
{{< /mermaid>}}

(Ignoring everything that is actually required like DNS, TCP handshakes,...)

What I wanted to achieve looks like this:

{{<mermaid>}}
%%{init: {'theme': 'neutral'}}%%
flowchart LR
    client([Client]) --> |IP packet|router{{Router}}
    router --> |modifiedIP packet|router
    router-->|faked response|client
{{< /mermaid>}}

I already knew it's possible and even intended to re-route packets with XDP because that's exactly what Cloudflare is doing with Unimog.
I tried to find some more examples on how to do redirect traffic and at this point I realized how much I **did/do** not know: 

* Do I need to update any checksums?
* If I only modify the IP header, do I also need to fix TCP/UDP checksums?
* Is modifying the IP enough or do I also need to modify the destination MAC address of the ethernet packet? 

And I bet there's even more I still haven't even thought about...

It's kind of hart to admit but it took me actually a lot of time to realize why my XDP approach wasn't working and will not ever work in the setup I have in mind.
One disadvantage of diving directly into the code and focusing on examples is: you've no idea what you're actually doing!
XDP is so great because depending on your hardware it is even possible to execute the program directly on the NIC but one huge disadvantage (for me) is, that it only captures **_ingress_** traffic!

So I perfectly screwed up because my whole traffic was within a single network segment and looked like in the following diagram.
I'm now using a sequence diagram to make things a bit more explicit.

Assuming the following network configuration:

{{< table "center" >}}
| Actor  | IP             |
| ------ | -------------- |
| Client | 192.168.178.51 |
| Router | 192.168.178.1  |
{{</ table >}}

{{<mermaid>}}
%%{init: {'theme': 'neutral'}}%%
sequenceDiagram
    Note over Client,Router: .51:34578 &rarr; 1.2.3.4:80
    Client->>Router: TCP SYN 
    Note over Client,Router: .51:34578 &rarr; 192.168.178.1:80
    Router->>Router: Redirect to 192.168.178.1:80
    Note over Client,Router: .51:34578 &larr; .1:80
    Router->>Client: SYN-ACK
{{< /mermaid>}}

But the `Client` didn't try to connect to `192.168.178.1` hence it won't accept the packet.
While this is kind of obvious it took me quite some time to get my head around this.
Not only but also because it's hard to observe this if you're using XDP because XDP forwarded/modified packets are not included in a PCAP.
Fortunately there's [`xdp-dump`](https://github.com/xdp-project/xdp-tools) to get a better understanding what's going on.

Okay, so I've to maintain a mapping of the original 4-tuple to my modified one to be able to restore the original source after the network stack handled the packet.
eBPF has a few different map types to store data between program invocations (I'll cover that later) so this wasn't a problem.
But now it hit me, with XDP I cannot manipulate egress (outgoing) packets.
So XDP's a dead end for this use case (although perfectly fine for the monitoring!).

## What if XDP is not enough?

Another round of 'research' revealed there are even more points in the network stack already where I could attach eBPF programs.
Every trace point has slightly different options (and therefore make sense to be there).

There are:

* `BPF_PROG_TYPE_XDP`: earliest possible point to get your hands on ingress traffic, can be used to monitor (and pass) incoming traffic, drop or redirect traffic
* `BPF_PROG_TYPE_SOCKET_FILTER`: drop or trim packets at socket level (much later than with XDP)
* `BPF_PROG_TYPE_CGROUP_SOCK`: much like XDP but within network cgroups
* ...

_A full list of program types can be found in [include/uapi/linux/bpf.h](https://github.com/torvalds/linux/blob/master/include/uapi/linux/bpf.h#L920) in the Linux kernel source.
A good introduction into the different types of eBPF programs can be found [here](https://blogs.oracle.com/linux/post/bpf-a-tour-of-program-types) (can't believe I'm linking an Oracle document).

All of the aforementioned options have in common that they can be used to filter packets but what I needed was an option to _modify egress_ packets.
AFAIK the only available option for this is `tc` (a.k.a. traffic control) which is Linux' QoS system.
Even though you get a lot of 'high level' information about what it is, that it supports (e)BPF and that there's the `tc` CLI to interact with it - also lot's of examples how to use the CLI - I could barely find a library/documentation about the API to not requiring shell outs.
Finally I found DataDog's [ebpf-manager](https://github.com/DataDog/ebpf-manager/) which uses Cilium's [ebpf](https://github.com/cilium/ebpf) and [go-tc](https://github.com/florianl/go-tc) to attach eBPF programs to the `tc` subsystem.
Actually not only this it also comes with a pretty handy manager layer to make working with eBPF a charm.

## `tc` in action

From now I'd like to dig a bit into the source code - all sources can be found [in the repo](https://github.com/baez90/baez90.github.io) under `code/ebpf-xdp-tc`.
The setup for experimenting looks like this:

{{<mermaid>}}
%%{init: {'theme': 'neutral'}}%%
flowchart LR
    subgraph libvirtNet [isolated Libvirt network]
        vm["Windows VM"] --> container["Podman container"]
    end
{{< /mermaid>}}

and is based on my post on how to [join a Podman container to a Libvirt network]({{< relref "libvirt-podman-network-mesh.md" >}}).

The resulting workflow should be looking like so:

{{<mermaid>}}
%%{init: {'theme': 'neutral'}}%%
sequenceDiagram    
    Client->>Ingress: IP packet
    Ingress->>Ingress: Rewrite packet and store original destination
    Ingress->>Server: Forward packet
    Server->>Egress: Intercept response packet
    Egress->>Egress: Restore original destination as source
    Egress->>Client: Forward packet
{{< /mermaid>}}

So in short words: the client sends an IP packet to an IP outside of the local network hence it will be sent to the gateway (which happens to be my Podman container).
The eBPF program attached to the `tc` ingress hook takes the incoming packet, rewrites it's destination to the local IP of the Podman container and passes the modified packet on.
In my experiment I'm using a simple HTTP server to respond to every HTTP request with a plain text message and a status code `200`.
The network stack processes the packet and replies e.g. with a `SYN-ACK` to the client's IP address.
The eBPF program attached to the `tc` egress hook intercepts the response, restores the original source source IP based on the client's IP address and TCP/UDP port and forwards the packet.

### Ingress traffic

Every eBPF program needs a section identifier and has to fulfill some constraints to pass the verifier.
Currently this means for instance that loops are not allowed to ensure the program has a guaranteed end.

The simplest ingress hook would look like this:

```c
#include <linux/bpf.h>
#include <linux/pkt_cls.h>

SEC("classifier/ingress")
int ingress(struct __sk_buff *skb) {
    return TC_ACT_SHOT;
}
```

This program would simply drop every incoming packet but it's a valid program.
The parameter - in this case `struct __sk_buff *skb` - depends on the trace point.
The `skb` is the most powerful one and in theory it's even possible to extract HTTP parameters out of it.
See for example [this article](http://vger.kernel.org/~davem/skb_data.html) for further details.
XDP programs are receiving another parameter that is a bit more generic but also less expensive to initialize.
Either way, you can easily (and rather cheap) extract the different parts of the IP packet just with a few lines of code.

A quick reminder how an IP packet looks like:

{{< figure 
    src="https://upload.wikimedia.org/wikipedia/commons/3/3b/UDP_encapsulation.svg"
    link="https://commons.wikimedia.org/wiki/File:UDP_encapsulation.svg"
    target="__blank"
    caption="en:User:Cburnett original work, colorization by en:User:Kbrose, [CC BY-SA 3.0](http://creativecommons.org/licenses/by-sa/3.0/), via Wikimedia Commons"
    title="UDP encapsulation"
>}}

In my simplified case I assume the `Frame header` will be a `struct ethhdr`, followed by the `struct iphdr` and then either a `struct udphdr` or a `struct tcphdr`.

To satisfy the verifier you've to validate after every cast that you haven't reached already the end of the current `skb` to ensure memory safety which is both: a bit annoying and a lot calming because memory issues are avoided right away.
So assuming we just want to 'print' the source and destination address of every packet reaching our ingress hook we would do the following:


```c
#include <linux/bpf.h>
#include <linux/if_ether.h>
#include <linux/in.h>
#include <linux/ip.h>
#include <linux/pkt_cls.h>

SEC("classifier/ingress")
int ingress(struct __sk_buff *skb) {
    void *data = (void *) (long) skb->data;
    void *data_end = (void *) (long) skb->data_end;

    struct ethhdr *eth = data;

    // apparently not an ethernet packet
    if ((void *) eth + sizeof(*eth) > data_end) {
        return TC_ACT_OK;
    }

    // ignore packet that are neither IPv4 nor IPv6
    if(eth->h_proto != ETH_P_IP && eth->h_proto != ETH_P_IPV6) {
        return TC_ACT_OK;
    }

    struct iphdr *iph = data + sizeof(*eth);
    if ((void *) iph + sizeof(*iph) > data_end) {
        return TC_ACT_OK;
    }

    bpf_printk("Packet from %d to %d\n", iph->saddr, iph->daddr);

    return TC_ACT_OK;
}
```

That's already a bit more code, isn't it?
So we start by capturing the beginning and the end of the packet.
As mentioned previously it's possible to 'extract' the individual parts of the packet just by casting the right offsets.
Of course some additional sanity checks are necessary to make sure we don't misinterpret anything.
For instance if the current packet is not an IP packet but probably an ARP packet we just let it pass.
What's worth mentioned is the `bpf_printk` macro because it's particularly useful - although it's slower than other options but it's pretty easy to use for debugging.
To get the message we're sending with it you can simply do

```sh
sudo cat /sys/kernel/debug/tracing/trace_pipe
```

and you're good to go!

#### eBPF maps

So remembering the sequence diagram above we are already almost finished regarding the parsing of the required information but how do we store the gather information?
eBPF comes with a bunch of different maps.
All types can be found in [include/uapi/linux/bpf.h](https://github.com/torvalds/linux/blob/master/include/uapi/linux/bpf.h#L878) or with more details [here](https://prototype-kernel.readthedocs.io/en/latest/bpf/ebpf_maps_types.html).

Some of them are equivalent to ordinary data structures most developers are used to like:

* `BPF_MAP_TYPE_ARRAY` behaves like an ordinary array
* `BPF_MAP_TYPE_HASH` behaves like an ordinary map/dictionary

but others like `BPF_MAP_TYPE_PERF_EVENT_ARRAY` or `BPF_MAP_TYPE_RINGBUF` are rather special.
Fow now we're focusing on `BPF_MAP_TYPE_HASH` because we can use it to store the  `orig-src` &rarr; `orig-dest` mapping.
To store data in a map and load it later on eBPF exposes some [bpf-helpers](https://www.man7.org/linux/man-pages/man7/bpf-helpers.7.html):

* `long bpf_map_update_elem(struct bpf_map *map, const void *key, const void *value, u64 flags)` to store data in a map
* `void *bpf_map_lookup_elem(struct bpf_map *map, const void *key)` to load data

An extension of the previous program could look like so:


```c
/*
... includes
*/

#define IP_FRAGMENTED 65343

// source IP and port
struct two_tuple {
    __u32 ip;
    __u16 port;
    __u16 _pad; // required to pad the size of the struct to a multiple of 4
};

struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __type(key, sizeof(struct two_tuple));
    __type(value, sizeof(struct two_tuple));
    __uint(max_entries, 1024);
} conn_track SEC(".maps");

SEC("classifier/ingress")
int ingress(struct __sk_buff *skb) {
    // ... previous logic

    // do not support fragmented packets as L4 headers may be missing
    if (iph->frag_off & IP_FRAGMENTED) {
        return TC_ACT_OK;
    }

    if (iph->protocol != IPPROTO_TCP) {
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
}
```