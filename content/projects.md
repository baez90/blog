---
title: 'Projects'
button: 'Projects'
weight: 2
---

## INetMock

[INetMock](https://gitlab.com/inetmock/inetmock) started as an resource/container friendly alternative to [INetSim](https://www.inetsim.org/).
While working on a project we tried to reduce analysis complexity coming from 'noise' in the network traffic recorded to a central INetSim cluster we were running.
We decided to decentralize the internet simulation, put it into a container image and run directly on every host multiple times in virtual networks.
Unfortunately INetSim has a relatively huge memory footprint (~1GB) which alone wouldn't been a showstopper but in combination with a relatively long startup time I felt having something smaller could be beneficial so I started to implement a prototype in Go.

2 years later INetMock has grown to kind of a full router (supporting DNS and DHCP) with support for faking HTTP/s (direct or proxy requests) requests.
Furthermore it is able to record PCAP files for further analysis and it emits events for every handled request.

It comes with a descriptive configuration language (embedded in a YAML configuration) to setup the behavior of all components and to define health checks/integration tests to validate your configuration.

Apart from working as a router it can also be used e.g. for integration tests of HTTP APIs, DNS/DoT/DoH clients and most likely other things I haven't even thought about.

## Goveal

[Goveal](https://github.com/baez90/goveal) is similar to [reveal-md](https://github.com/webpro/reveal-md) or previously _GitPitch_ but obviously in Go.
Originally I used GitPitch but then the author decided to go with a commercial license.
The commercial license made sense when I was working at the university but after that it didn't really make sense any more.
So I decided to replace it with a small custom CLI rendering the markdown into a static HTML file and serving it as a local web server (basically).

Later on I refined it more and more.
Currently I'm working on a rewrite which adds e.g. 1st class support for [mermaid-js](https://mermaid-js.github.io) diagrams in slides.