package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"io/ioutil"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/golang/glog"
	crypto "github.com/libp2p/go-libp2p-crypto"
	net "github.com/libp2p/go-libp2p-net"
	peer "github.com/libp2p/go-libp2p-peer"
	ps "github.com/libp2p/go-libp2p-peerstore"
	protocol "github.com/libp2p/go-libp2p-protocol"
	"github.com/livepeer/libp2p-spike/core"
	"github.com/livepeer/lpms"
	ma "github.com/multiformats/go-multiaddr"
	"github.com/nareix/joy4/av"

	lpmsStream "github.com/livepeer/lpms/stream"
)

var LivepeerProtocol = protocol.ID("/livepeer")
var SeedPeerID = "QmURyyVgQBd59rtLfNrdryZ6FAhYZvCUJTpePmXmbE4ghR"
var SeedAddr = "/ip4/127.0.0.1/tcp/10000"

type KeyFile struct {
	Pub  string
	Priv string
}

func getKeys(datadir string) (crypto.PrivKey, crypto.PubKey) {
	f, e := ioutil.ReadFile(path.Join(datadir, "keys.json"))
	var keyf KeyFile
	json.Unmarshal(f, &keyf)

	if e != nil || keyf.Priv == "" || keyf.Pub == "" {
		glog.Errorf("Cannot file keys in data dir, creating new keys")
		priv, pub, _ := crypto.GenerateKeyPair(crypto.RSA, 2048)

		privb, _ := priv.Bytes()
		pubb, _ := pub.Bytes()

		kf := KeyFile{Priv: crypto.ConfigEncodeKey(privb), Pub: crypto.ConfigEncodeKey(pubb)}
		kfb, _ := json.Marshal(kf)
		ioutil.WriteFile(path.Join(datadir, "keys.json"), kfb, 0644)

		return priv, pub
	}

	privb, _ := crypto.ConfigDecodeKey(keyf.Priv)
	pubb, _ := crypto.ConfigDecodeKey(keyf.Pub)
	priv, _ := crypto.UnmarshalPrivateKey(privb)
	pub, _ := crypto.UnmarshalPublicKey(pubb)
	return priv, pub
}

func handleProtocol(vs *VideoServer, n *core.LivepeerNode, ws *core.WrappedStream) {
	var msg core.Message
	err := ws.Dec.Decode(&msg)

	if err != nil {
		glog.Errorf("Got error decoding msg: %v", err)
		return
	}
	//Protocol:
	//	- Join Swarm (and form kademlia network)
	//	- Get Peer Info???
	//	- Have(strmID, chunkID)
	//	- Request Video Chunk(strmID, chunkID) (ChunkDATA)
	//	- Transcode(strmID, config) (newStrmID)
	switch msg.Msg {
	case core.JoinMsgID:
		handleJoinSwarm(n, msg)

	case core.JoinAckID:
		glog.Infof("Recieved Ack")

	case core.ChunkHaveID:
		handleHaveMsg(n, msg)

	case core.ChunkReqID:
		handleChunkReqMsg(vs.ChunkSub, n, msg)

	case core.ChunkReqAckID:
		handleChunkReqAckMsg(vs.ChunkSub.Buffer, n, msg)

	case core.SubReqID:
		handleSubscribeMsg(n, msg)
	default:
	}

}

func handleJoinSwarm(n *core.LivepeerNode, msg core.Message) {
	//Join the swarm
	joinMsg, ok := msg.Data.(map[string]interface{})
	if !ok {
		glog.Errorf("Error converting JoinMsg: %v", msg.Data)
	}
	pid, _ := peer.IDB58Decode(joinMsg["ID"].(string))
	addr, _ := ma.NewMultiaddr(joinMsg["Addr"].(string))
	n.Peerstore.AddAddr(pid, addr, ps.PermanentAddrTTL)
	n.PeerHost.Connect(context.Background(), ps.PeerInfo{ID: pid})

	//Print current peers
	glog.Infof("Peers: %v", n.Peerstore.Peers())
	for _, p := range n.Peerstore.Peers() {
		glog.Infof("%v", p.Pretty())
	}
	glog.Infof("\n\n")

	//Send Ack
	n.SendJoinAck(pid)
}

func handleHaveMsg(n *core.LivepeerNode, msg core.Message) {
	haveMsg, ok := msg.Data.(map[string]interface{})
	glog.Infof("Got have Msg: %v", haveMsg)
	if !ok {
		glog.Errorf("Error converting JoinMsg: %v", msg.Data)
	}
	cName, _ := haveMsg["ChunkName"].(string)
	pid, _ := peer.IDB58Decode(haveMsg["PeerID"].(string))
	addr, _ := ma.NewMultiaddr(haveMsg["Addr"].(string))

	//Send ChunkReq - greedy approach
	n.SendChunkReq(ps.PeerInfo{ID: pid, Addrs: []ma.Multiaddr{addr}}, cName, "")
}

func handleSubscribeMsg(n *core.LivepeerNode, msg core.Message) {
	subMsg, ok := msg.Data.(map[string]interface{})
	glog.Infof("Got subscribe Msg: %v", subMsg)
	if !ok {
		glog.Errorf("Error converting subMsg: %v", msg.Data)
	}

	//Add the peer to the stream swarm, so they'll start recieving HAVE messages
	pid, _ := peer.IDB58Decode(subMsg["ID"].(string))
	addr, _ := ma.NewMultiaddr(subMsg["Addr"].(string))
	sid := subMsg["SID"].(string)

	peers := n.StreamPeers[sid]
	if peers == nil {
		peers = make([]ps.PeerInfo, 0, 0)
		n.StreamPeers[sid] = peers
	}
	peers = append(peers, ps.PeerInfo{ID: pid, Addrs: []ma.Multiaddr{addr}})
	n.StreamPeers[sid] = peers

	// swarm.Peerstore().AddAddr(pid, addr, ps.PermanentAddrTTL)
	glog.Infof("Peers for %v: %v", sid, n.StreamPeers[sid])
}

func handleChunkReqMsg(cs *core.ChunkSubscriber, n *core.LivepeerNode, msg core.Message) {
	chunkReqMsg, ok := msg.Data.(map[string]interface{})
	glog.Infof("Got chunkReqMsg: %v", chunkReqMsg)
	if !ok {
		glog.Errorf("Error converting subMsg: %v", msg.Data)
	}

	//Send the data back
	cname, _ := chunkReqMsg["ChunkName"].(string)
	pid, _ := peer.IDB58Decode(chunkReqMsg["Peer"].(string))
	strmID, _ := chunkReqMsg["StreamID"].(string)
	seg, err := cs.Buffer.WaitAndGetSegment(context.Background(), cname)
	if err != nil {
		glog.Errorf("Chunk Req Handler Get Segment Failed: %v", err)
	}

	glog.Infof("Sending chunk req ack (%v) to %v", len(seg), pid.Pretty())
	n.SendChunkReqAck(ps.PeerInfo{ID: pid, Addrs: []ma.Multiaddr{n.PeerHost.Addrs()[0]}}, cname, strmID, seg)
}

func handleChunkReqAckMsg(buf *lpmsStream.HLSBuffer, n *core.LivepeerNode, msg core.Message) {
	chunkReqAckMsg, ok := msg.Data.(map[string]interface{})
	// glog.Infof("Got chunkReqAck Msg: %v", chunkReqAckMsg)
	if !ok {
		glog.Errorf("Error converting subMsg: %v", msg.Data)
	}

	// var msgStruct core.ChunkReqAckMsg
	// enc := gob.NewEncoder(&msgStruct)
	// err = enc.Encode(chunkReqAckMsg["Data"])

	//Send the data back
	cname, ok := chunkReqAckMsg["ChunkName"].(string)
	if !ok {
		glog.Errorf("Chunk Req Ack Handler Failed: %v", chunkReqAckMsg["ChunkName"])
	}
	data, ok := chunkReqAckMsg["Data"].(string)
	if !ok {
		glog.Errorf("Chunk Req Ack Handler Failed: %v", chunkReqAckMsg["Data"])
	}
	numStr := strings.Split(strings.Split(cname, "_")[1], ".")[0]
	num, err := strconv.Atoi(numStr)
	if err != nil {
		glog.Errorf("Chunk Req Ack Handler Failed: %v", err)
	}

	dataBytes, err := hex.DecodeString(data)
	if err != nil {
		glog.Errorf("Chunk Req Ack Handler Failed: %v", err)
	}

	err = buf.WriteSegment(uint64(num), cname, 8, dataBytes)
	if err != nil {
		glog.Errorf("Chunk Req Ack Handler Failed: %v", err)
	}
	// glog.Infof("Got Chunk: %v(%v)", cname, len(data))
	glog.Infof("Got Chunk #%v: %v(%v)", num, cname, len(dataBytes))
}

// func getPeers(s net.Stream) {
// 	buf := bufio.NewReader(s)
// }

//Other Functionalities
//	- Write Video Chunk to Swarm, IPFS, S3

func main() {
	// glog.Infof("Starting node...")
	datadir := flag.String("datadir", "", "data directory")
	port := flag.Int("p", 0, "port")
	httpPort := flag.String("http", "", "http port")
	rtmpPort := flag.String("rtmp", "", "rtmp port")
	flag.Parse()

	if *datadir == "" {
		glog.Fatalf("Please provide a datadir with -datadir")
	}
	if *port == 0 {
		glog.Fatalf("Please provide port")
	}
	if *httpPort == "" {
		glog.Fatalf("Please provide http port")
	}
	if *rtmpPort == "" {
		glog.Fatalf("Please provide rtmp port")
	}

	priv, pub := getKeys(*datadir)

	n, err := core.NewNode(*port, priv, pub)
	if err != nil {
		glog.Errorf("Cannot create node: %v", err)
		return
	}

	vs := NewVideoServer(n, *rtmpPort, *httpPort, "", "")

	n.PeerHost.SetStreamHandler(LivepeerProtocol, func(s net.Stream) {
		wrappedStream := core.WrapStream(s)
		defer s.Close()
		handleProtocol(vs, n, wrappedStream)
	})

	// glog.Infof("Node ID: %v", n.PeerHost.ID())
	// glog.Infof("Identity: %s", n.Identity.Pretty())
	// glog.Infof("peer.ID: %s", peer.ID("0x1442ef0"))
	// glog.Infof("Addrs: %v", n.Peerstore.Addrs())
	seedID, _ := peer.IDB58Decode(SeedPeerID)

	//Try and connect to the seed node
	if n.Identity != seedID {
		addr, err := ma.NewMultiaddr(SeedAddr)
		if err != nil {
			glog.Fatalf("Cannot join swarm: %v", err)
		}
		n.Peerstore.AddAddr(seedID, addr, ps.PermanentAddrTTL)
		n.PeerHost.Connect(context.Background(), ps.PeerInfo{ID: seedID})

		n.SendJoin(seedID)
	}

	//TODO: Kick off a goroutine to monitor connection speed

	glog.Infof("%v listening for connections on %v", n.Identity.Pretty(), n.PeerHost.Addrs())
	go vs.StartVideoServer(context.Background())
	select {} // hang forever
}

type StreamDB struct {
	db map[string]lpmsStream.Stream
}

type BufferDB struct {
	db map[string]*lpmsStream.HLSBuffer
}

type VideoServer struct {
	SDB      *StreamDB
	BDB      *BufferDB
	Server   *lpms.LPMS
	Node     *core.LivepeerNode
	ChunkSub *core.ChunkSubscriber
}

func NewVideoServer(n *core.LivepeerNode, rtmpPort string, httpPort string, ffmpegPath string, vodPath string) *VideoServer {
	s := lpms.New(rtmpPort, httpPort, ffmpegPath, vodPath)
	sdb := &StreamDB{db: make(map[string]lpmsStream.Stream)}
	bdb := &BufferDB{db: make(map[string]*lpmsStream.HLSBuffer)}
	return &VideoServer{SDB: sdb, BDB: bdb, Server: s, Node: n}
}

func (vs *VideoServer) StartVideoServer(ctx context.Context) {
	//Create HLSBuffer, subscribe to the HLS stream
	glog.Infof("Creating new HLS Buffer")
	buffer := lpmsStream.NewHLSBuffer(10, 1000)
	cs := core.NewChunkSubscriber(buffer, vs.Node)
	vs.ChunkSub = cs

	vs.Server.HandleRTMPPublish(
		//getStreamID
		func(url *url.URL) (string, error) {
			return "", nil
		},
		//getStream
		func(url *url.URL) (lpmsStream.Stream, lpmsStream.Stream, error) {

			rtmpStrmID := core.StreamID(parseStreamID(url.Path))
			if rtmpStrmID == "" {
				rtmpStrmID = core.MakeStreamID(vs.Node.Identity, core.RandomStreamID())
			}
			nodeID, _ := rtmpStrmID.SplitComponents()
			if nodeID != vs.Node.Identity {
				glog.Errorf("Invalid rtmp strmID - nodeID component needs to be self.")
				return nil, nil, errors.New("Invalid RTMP StrmID")
			}

			hlsStrmID := core.MakeStreamID(vs.Node.Identity, core.RandomStreamID())
			rtmpStream := lpmsStream.NewVideoStream(string(rtmpStrmID), lpmsStream.RTMP)
			hlsStream := lpmsStream.NewVideoStream(string(hlsStrmID), lpmsStream.HLS)
			vs.SDB.db[string(rtmpStrmID)] = rtmpStream
			vs.SDB.db[string(hlsStrmID)] = hlsStream

			vs.BDB.db[string(hlsStrmID)] = buffer
			sub := lpmsStream.NewStreamSubscriber(hlsStream)
			go sub.StartHLSWorker(context.Background(), time.Second*10)
			err := sub.SubscribeHLS(string(hlsStrmID), cs)
			if err != nil {
				return nil, nil, errors.New("ErrStreamSubscriber")
			}

			glog.Infof("Streaming hls stream: %v", hlsStrmID)
			return rtmpStream, hlsStream, nil
		},
		//finishStream
		func(rtmpStrmID string, hlsStrmID string) {
			glog.Infof("Finish Stream - canceling stream (need to implement handler for Done())")
			// streamer.DeleteNetworkStream(streaming.StreamID(rtmpStrmID))
			// streamer.DeleteNetworkStream(streaming.StreamID(hlsStrmID))
			// streamer.UnsubscribeAll(rtmpStrmID)
			// streamer.UnsubscribeAll(hlsStrmID)
		})
	vs.Server.HandleRTMPPlay(
		//getStream
		func(ctx context.Context, reqPath string, dst av.MuxCloser) error {
			glog.Infof("Got req: ", reqPath)
			streamID := parseStreamID(reqPath)
			src := vs.SDB.db[streamID]

			if src != nil {
				src.ReadRTMPFromStream(ctx, dst)
			} else {
				glog.Error("Cannot find stream for ", streamID)
				return errors.New("ErrNotFound")
			}
			return nil
		})

	vs.Server.HandleHLSPlay(
		//getHLSBuffer
		func(reqPath string) (*lpmsStream.HLSBuffer, error) {
			streamID := parseStreamID(reqPath)
			glog.Infof("Got HTTP Req for stream: %v", streamID)
			buffer := vs.BDB.db[streamID]
			s := vs.SDB.db[streamID]

			if s == nil {
				//Send request to the network
				pl, _ := vs.ChunkSub.Buffer.LatestPlaylist()
				if pl.Segments[0] != nil {
					return vs.ChunkSub.Buffer, nil
				}

				return nil, errors.New("NotFound")
			}

			if buffer == nil {
				return nil, errors.New("NotFound")
			}
			return buffer, nil
		})

	http.HandleFunc("/subscribe", func(w http.ResponseWriter, r *http.Request) {
		vals, _ := url.ParseQuery(r.URL.RawQuery)
		strmID := core.StreamID(vals.Get("streamID"))
		glog.Infof("Got http request for /subscribe %v", strmID)

		vs.Node.SendSubscribeReq(strmID)
		w.Write([]byte("ok"))
	})

	vs.Server.Start()
}

func parseStreamID(reqPath string) string {
	var strmID string
	regex, _ := regexp.Compile("\\/stream\\/([[:alpha:]]|\\d)*")
	match := regex.FindString(reqPath)
	if match != "" {
		strmID = strings.Replace(match, "/stream/", "", -1)
	} else {
		regex, _ = regexp.Compile("\\?streamID=([[:alpha:]]|\\d)*")
		match = regex.FindString(reqPath)
		if match != "" {
			strmID = strings.Replace(match, "/streamID=", "", -1)
		}
	}
	return strmID
}
