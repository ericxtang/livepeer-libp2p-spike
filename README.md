## Livepeer Libp2p Spike

[Livepeer](https://livepeer.org) is a decentralized live streaming broadcast platform. This
repo is a proof-of-concept spike built on top of [libp2p](https://github.com/libp2p) by [Protocol Labs](https://protocol.ai/).

This code allows you to stream a RTMP stream to many nodes on a peer-to-peer network.  (The RTMP stream will automatically be converted to a HLS stream so it can be viewed over http).

## Installation
Run `go get github.com/ericxtang/livepeer-libp2p-spike/cmd/livepeerd`

## Running the code

#### Start Boot Node

To start a boot node, create a data dir, and start the node by running: 

`livepeerd -datadir={datadir} -p={tcpport} -http={httpport} -rtmp={rtmpport} -logtostderr=true`.  

For example - `livepeerd -datadir=data1 -p=10000 -http=8080 -rtmp=1935 -logtostderr=true`

#### Start Peer

To start a peer, run 

`livepeerd -datadir={datadir} -p={tcpport} -http={httpport} -rtmp={rtmpport} -seedID={seedID} -seedAddr={seedAddr} -logtostderr=true`.

The `seedID` and `seedAddr` can be found in the boot node console.  For example:

`livepeerd -datadir=data2 -p=10001 -http=8081 -rtmp=1936 -seedID=QmURyyVgQBd59rtLfNrdryZ6FAhYZvCUJTpePmXmbE4ghR -seedAddr=/ip4/127.0.0.1/tcp/10000 -logtostderr=true`

#### Create Video Stream

Now, start a rtmp stream by running:

`ffmpeg -f avfoundation -framerate 30 -pixel_format uyvy422 -i "0:0" -vcodec libx264 -tune zerolatency -b 900k -x264-params keyint=60:min-keyint=60 -acodec aac -ac 1 -b:a 96k -f flv rtmp://localhost:1935/`

This creates a RTMP stream and sends it to the boot node using [ffmpeg](http://ffmpeg.org/).  You should be able to see a RTMP stream and a HLS stream created - something like: 

```
I0606 17:46:25.650033   44931 listener.go:35] RTMP server got upstream: rtmp://localhost:1935/
I0606 17:46:25.650203   44931 livepeerd.go:369] Streaming hls stream: 12205a83cf18c934570c1308b88860a2631fbfab4246b5dea6d0f6f03eac1561232c4d118318be39b85585bf5318943cd91b
I0606 17:46:25.650220   44931 listener.go:49] Got RTMP Stream: 12205a83cf18c934570c1308b88860a2631fbfab4246b5dea6d0f6f03eac1561232c35146d9a3ec1c8ace2162ab68cb599e6
I0606 17:46:25.650227   44931 listener.go:53] Writing RTMP to stream
I0606 17:46:25.653717   44931 video_segmenter.go:91] Ffmpeg path:
I0606 17:46:25.689362   44931 player.go:34] LPMS got RTMP request @ rtmp://localhost:1935/stream/12205a83cf18c934570c1308b88860a2631fbfab4246b5dea6d0f6f03eac1561232c35146d9a3ec1c8ace2162ab68cb599e6
I0606 17:46:25.689454   44931 livepeerd.go:383] Got req: %!(EXTRA string=/stream/12205a83cf18c934570c1308b88860a2631fbfab4246b5dea6d0f6f03eac1561232c35146d9a3ec1c8ace2162ab68cb599e6)
```

#### Subscribe to Stream

Now you can subscribe to the stream with the new node.  To do that, copy the HLS streamID from the output, and run

`curl http://localhost:{peerHttpPort}/subscribe?streamID={hlsStreamID}`

Make sure the peerHttpPort is the http port you used to start the second node.  If the subscribe request is successful, you should get a `ok` as response.

#### View Stream

Now you can play the hls stream using ffplay - 

`ffplay http://localhost:{peerHttpPort}/stream/{hlsStreamID}`



