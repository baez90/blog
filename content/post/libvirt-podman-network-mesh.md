+++
author = "Peter Kurfer"
title = "Libvirt & Podman: network 'mesh'"
date = "2022-02-24"
description = "Joining libvirt VMs and Podman container in a common network"
tags = [
    "podman",
    "libvirt"
]
+++

_Disclaimer: I tested all this with Podman 3.x even though Podman 4.0 is already announced but the {{<abbr short="CNI" full="Container Network Interface" >}} driver is still available with Podman 4.0 and as soon as I get my hands on 4.0 I'll give **Netavark** a try, too!_

When playing around with containers and {{<abbr short="VM" full="Virtual Machine" >}}s one might ask if it's possible to bring VMs and containers into a common network segment.
I see 'why the hell would I need a VM anyway when already having containers' or something similar I almost see on your face :stuck_out_tongue_winking_eye:

Well 1st of all, not everything can be solved with containers.
For instance windows applications can be run in Windows containers but I'm not aware of how to run a Windows container on my Linux desktop.

But also in pure Linux environments there are cases where a VM is probably a better fit for the problem.
As you might know I'm a bit of network :nerd: and I love playing around with 'weird' stuff almost no one else does even think about if not forced to.
So if you try to implement for example your own DHCP server you might want to isolate your experiments (especially at the beginning) to avoid discussion about "why's Netflix on the TV not working?!" :smile: or also if you try to implement your own 'firewall' with {{<abbr short="DNAT" full="Destination network address translation" >}} support (stay tuned - post's following!).

## Part 1: Libvirt preparation

Okay now that I came around with _some_ arguments - if they're convincing or not is not important - how does this work?

Assuming you've Libvirt and Podman already installed on your system without any modification and you run

```bash
virsh net-list
```

you should have at least the `default` network already.

The definition of all networks (as of every other component of libvirt) is in XML.
`virsh` comes with a `net-dumpxml` command to export the configuration of a network:

```bash
virsh net-dumpxml default
```

The output should look (more or less) like in the following snippet:


```xml
<network>
  <name>default</name>
  <uuid>8d2028ed-cc9a-4eae-9883-b59b673d560d</uuid>
  <forward mode='nat'>
    <nat>
      <port start='1024' end='65535'/>
    </nat>
  </forward>
  <bridge name='virbr0' stp='on' delay='0'/>
  <mac address='63:b3:d8:75:53:6b'/>
  <ip address='192.168.122.1' netmask='255.255.255.0'>
    <dhcp>
      <range start='192.168.122.2' end='192.168.122.254'/>
    </dhcp>
  </ip>
</network>
```

So we've a `<network/>` that is defined by:

* a `<name/>`
* a `<uuid/>`
* a _optional_ `<forward/>` node
* a `<bridge/>` interface
* the `<mac/>` for the bridge interface (of the host)
* the `<ip/>` of the host on the bridge interface
  * an _optional_ `<dhcp/>` range definition

The complete reference for the XML schema can be found [here](https://libvirt.org/formatnetwork.html).

Before we have a closer look how to bring Podman containers into a Libvirt network, let's define a new `containers` network.
The following snippet contains the definition I'll use:

```xml
<network>
  <name>containers</name>
  <uuid>929b7b7d-bd82-452d-96b7-12f0cf1a4b17</uuid>
  <bridge name='conbr0' stp='on' delay='0'/>
  <mac address='af:af:13:ed:c6:41'/>
  <ip address='10.10.1.42' netmask='255.255.255.0'>
    <dhcp>
      <range start='10.10.1.100' end='10.10.1.150'/>
    </dhcp>
  </ip>
</network>
```

It's quite similar except I made a few adoptions:

* remove the `<forward/>` block
* change the `<name/>` and the `<uuid/>` (with the help of `uuidgen`)
* change the `name=""` of the `<bridge/>`
* change the `address=""` attribute of the `<mac/>` (use any [mac address generator](https://macaddress.io/mac-address-generator))
* change the `address=""` attribute of the `<ip/>` and `start=""` and `end=""` of the DHCP range accordingly

You may use any private network - as far as I can tell it shouldn't matter if you're using a class B, C or D private network as long as you don't have any conflicts with your LAN or any other virtual interfaces of your environment.

When done safe your network definition as `.xml` file.
To import the configuration you can use `virsh net-define` like in the following snippet (assuming the network definition is in `containers.xml`):

```bash
$ virsh net-define containers.xml

> Network containers defined from containers.xml
```

_Note: this only works because the XML already contains an `<uuid/>`. Otherwise you'd have to use `virsh net-create` and a few more extra steps to make the network actually persistent._

If you now check with `virsh net-list` you'd be disappointed because there's no network!
Checking again with `virsh net-list --all` explains why our `containers` network wasn't in the output previously because it is by default _inactive_.
To activate it we've to start it like so:

```bash
$ virsh net-start containers

> Network containers started
```

If you don't mind the extra adapter and wish to use the network frequently in the future you might consider to autostart it:


```bash
$ virsh net-autostart containers

> Network containers marked as autostarted
```

With our custom Libvirt network in place we're good to go to configure Podman.

## Part 2: Podman CNI network

_Note: this only works with **rootfull** Podman because rootless Podman does not use CNI but another network stack._

A clean Podman installation without any custom network created comes with the default network `podman`.
Rootfull Podman network configs are by default stored in `/etc/cni/net.d`.
You should find the default network as `87-podman.conflist` in the aforementioned directory.

Every Podman network is defined as JSON file.
We will define our own `libvirt` network to join Podman containers into the previously created Libvirt network.
You can either use `podman network create` to create the network (at least more or less) or you can copy for example the default network and make some adjustments.

To create the new network from the CLI you can use the following command:

```bash
podman network create \
    --disable-dns \
    --internal \
    --gateway 10.10.2.37 \
    --ip-range 10.10.2.160/29 \
    --subnet 10.10.2.0/24 \
    libvirt
```

Note that I used a different IP range as in the Libvirt network! Otherwise Podman will complain that the IP range is already in use at an adapter.
You can use this command to create the required file in `/etc/cni/net.d/` but you've to update the `ranges` accordingly before creating a container in the network.

Because we've to edit the `.conflist` either way copy the default one is also fine.

The `.conflist` I'm using looks like this:

```json
{
   "cniVersion": "0.4.0",
   "name": "libvirt",
   "plugins": [
      {
         "type": "bridge",
         "bridge": "conbr0",
         "isGateway": false,
         "hairpinMode": true,
         "ipam": {
            "type": "host-local",
            "routes": [
               {
                  "dst": "0.0.0.0/0"
               }
            ],
            "ranges": [
               [
                  {
                     "subnet": "10.10.1.0/24",
                     "rangeStart": "10.10.1.151",
                     "rangeEnd": "10.10.1.160",
                     "gateway": "10.10.1.42"
                  }
               ]
            ]
         }
      },
      {
         "type": "portmap",
         "capabilities": {
            "portMappings": true
         }
      },
      {
         "type": "firewall",
         "backend": ""
      },
      {
         "type": "tuning"
      }
   ]
}
```

Interestingly the `rangeStart` and `rangeEnd` are actually IP addresses and not tight to some IP networks but unfortunately there's no equivalent for `podman network create` hence I update both to a range after the DHCP range of the Libvirt network to make sure that no duplicate IPs are assigned.

I tend to declare the network as `host-local` but this shouldn't be critical.
The **most important** part is to update the `bridge` to the same interface like in the Libvirt network definition (in my case `conbr0`).

After this we're ready to go and you can for instance start a Nginx container in the `libvirt` network and you should be able to reach it from a VM in the Libvirt network:

```bash
podman run \
    --rm \
    -d \
    --name nginx \
    --network libvirt \
    --ip 10.10.1.151 \
    docker.io/nginx:alpine
```

A nice option for `podman run` is `--ip`.
You've to choose an IP from the previously configured `range` but you can skip the `podman inspect` or `ip a` to get the container IP and the container will have the IP every time you start it, if you like :wink: and speaking of 'nice' `podman run` options: you do know `--replace`, don't you?