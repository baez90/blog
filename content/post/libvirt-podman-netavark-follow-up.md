+++
author = "Peter Kurfer"
title = "Libvirt & Podman: follow up for Podman 4.0 and netavark"
date = "2022-02-24"
description = "Joining libvirt VMs and containers with Podman 4.0's new network stack netavark"
tags = [
    "podman",
    "libvirt",
    "netavark"
]
+++

This is a follow up post to ["Joining libvirt {{<abbr short="VM" full="Virtual Machine" >}}s and Podman container in a common network"]({{<relref "libvirt-podman-network-mesh.md" >}}).
Therefore I won't cover all the basics again and how to configure libvirt because nothing's changed on that side.

## Podman 4.0

Podman 4.0 comes with a completely new network stack replacing the previous [{{<abbr short="CNI" full="Container Network Interface" >}}](https://www.cni.dev/) stack:

* [Netavark](https://github.com/containers/netavark)
* [Aardvark](https://github.com/containers/aardvark-dns)

There are [great resources](https://www.redhat.com/sysadmin/podman-new-network-stack) that explain the backgrounds of both tools and I don't think I could describe it better than the folks implementing it :smile: so if you're interested have a look at the aforementioned article or the [release post](https://podman.io/releases/2022/02/22/podman-release-v4.0.0.html).

## Netavark and libvirt

After reading the announcement I was most curious if I would be able to configure an equivalent setup for Netavark like I described it with Podman 3.x and CNI.

__Short answer:__ yes, it is possible! :tada:

_"But how?!"_ do you ask?
Well it's pretty much equivalent to the previous solution: you need to create a new Podman network I once more named it _'libvirt'_.
To get an idea how the config should look like and where it should placed.
I reused the CLI call from my previous article:

```bash
podman network create \
    --disable-dns \
    --internal \
    --gateway 10.10.2.37 \
    --ip-range 10.10.2.160/29 \
    --subnet 10.10.2.0/24 \
    libvirt
```

The configuration files are now obviously resided in `/etc/containers/networks/` and my (already modified) `libvirt.json` now looks like so:

```json
{
     "name": "libvirt",
     "id": "0489e6e643b97003c47b27a9ce0a6f6a8dce7d5f08329603e79a0ba48ad5285f",
     "driver": "bridge",
     "network_interface": "conbr0",
     "created": "2022-04-05T09:18:48.198960971+01:00",
     "subnets": [
          {
               "subnet": "10.10.1.0/24",
               "gateway": "10.10.1.42",
               "lease_range": {
                    "start_ip": "10.10.1.1",
                    "end_ip": "10.10.1.10"
               }
          }
     ],
     "ipv6_enabled": false,
     "internal": false,
     "dns_enabled": false,
     "ipam_options": {
          "driver": "host-local"
     }
}
```

_Side note: I'm really happy they dropped the `.conflist` extension because this way most editors offer really helpful syntax highlighting in the first place!_

Note that `"internal": false` is mandatory. Otherwise I wasn't able to establish communication between VM and container.
I also disabled the Aardvark {{<abbr short="DNS" full="Domain Name System">}} server and IPv6 support because I don't need it and I also don't expect much benefit of it due to the fact that it can't be aware of the VMs present in the network same as `dnsmasq` won't be able to resolve containers in the libvirt network.

Having this in place I was again able to reuse the CLI command from my previous article:

```bash
podman run \
    --rm \
    -d \
    --name nginx \
    --network libvirt \
    --ip 10.10.1.151 \
    docker.io/nginx:alpine
```

to create a Nginx container that can be reached from a VM.

## Troubleshooting

Sometimes the communication between container and VM fails - don't know if I restarted the libvirt network previously or somehow fucked up the container network configuration but a:

```bash
podman network reload <container ID/container name>
```

often resolved the problem.

## Final thoughts

I haven't used _Netavark_ and _Aardvark_ a lot, yet.
But I already noticed a few **really awesome** things:

- the `docker-compose` support seems to be a lot better now because containers are actually able to talk to each other by _service name_, something I wasn't able to configure properly in Podman 3.x - at least not rootless.
- with _Netavark_ all the Podman configuration is now unified within `/etc/containers` or `$HOME/.config/containers` respectively
- the new configuration format is a little bit cleaner the the previous one due to the fact that _Netavark_ does not support plugins and with a `.json` extension editors do help a lot more without requiring extra "configuration"