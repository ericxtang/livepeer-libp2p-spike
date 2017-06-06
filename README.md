## Livepeer Libp2p Spike

[Livepeer](https://livepeer.org) is a decentralized live streaming broadcast platform. This
repo is a proof-of-concept spike built on top of [libp2p](https://github.com/libp2p) by [Protocol Labs](https://protocol.ai/).

This code allows you to stream a RTMP stream to many nodes on a peer-to-peer network.  (The RTMP stream will automatically be converted to a HLS stream so it can be viewed over http).

## Installation
Run `go get github.com/ericxtang/livepeer-libp2p-spike/cmd/livepeerd`

## Running the code
To start a boot node, create a data dir, and start the node by running `./livepeerd -datadir={datadir} -p={tcpport} -http={httpport} -rtmp={rtmpport} -logtostderr=true`.  For example, you can run `./livepeerd -datadir=datadir1 -p=10000 -http=8080 -rtmp=1935 -logtostderr=true`

