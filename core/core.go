package core

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"math/rand"
	"time"

	// dht "gx/ipfs/QmQcRLisUbREko56ThfgzdBorMGNfNjgqzvwuPPr1jFw6A/go-libp2p-kad-dht"
	// routing "gx/ipfs/QmafuecpeZp3k3sHJ5mUARHd4795revuadECQMkmHB8LfW/go-libp2p-routing"
	// p2phost "gx/ipfs/QmcyNeWPsoFGxThGpV8JnJdfUNankKhWCTrbrcFRQda4xR/go-libp2p-host"
	// ma "gx/ipfs/QmcyqRMCAXVtYPS4DiBrA7sezL9rRGfW8Ctx7cywL4TXJj/go-multiaddr"

	// ds "github.com/ipfs/go-datastore"

	"github.com/golang/glog"
	ds "github.com/ipfs/go-datastore"
	crypto "github.com/libp2p/go-libp2p-crypto"
	p2phost "github.com/libp2p/go-libp2p-host"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	net "github.com/libp2p/go-libp2p-net"
	peer "github.com/libp2p/go-libp2p-peer"
	ps "github.com/libp2p/go-libp2p-peerstore"
	protocol "github.com/libp2p/go-libp2p-protocol"
	routing "github.com/libp2p/go-libp2p-routing"
	swarm "github.com/libp2p/go-libp2p-swarm"
	bhost "github.com/livepeer/libp2p-spike/host/basic"
	rhost "github.com/livepeer/libp2p-spike/host/routed"
	ma "github.com/multiformats/go-multiaddr"
	multicodec "github.com/multiformats/go-multicodec"
	mcjson "github.com/multiformats/go-multicodec/json"

	lpmsStream "github.com/livepeer/lpms/stream"
)

// var IpnsValidatorTag = "ipns"

// var P_LIVEPEER = 422
var LivepeerProtocol = protocol.ID("/livepeer")

const (
	JoinMsgID = iota
	JoinAckID
	SubReqID
	ChunkHaveID
	ChunkReqID
	ChunkReqAckID
	TranscodeMsgID
	HaveMsgID
)

type Message struct {
	Msg  int
	Data interface{}
	// HangUp bool
}

type JoinMsg struct {
	ID   string
	Addr string
	//Shouldn't need any data...
}

type ChunkHaveMsg struct {
	ChunkName string
	StreamID  string
	PeerID    string
	Addr      string
}

type ChunkReqMsg struct {
	ChunkName string
	StreamID  string
	Peer      string
}

type ChunkReqAckMsg struct {
	ChunkName string
	StreamID  string
	Data      string //hex encoded
}

type SubReqMsg struct {
	ID   string
	Addr string
	SID  string
}

type Transcode struct {
	StrmID string
	//Could be a transcode config, but let's assume it's just the same config.
}

type WrappedStream struct {
	Stream net.Stream
	Enc    multicodec.Encoder
	Dec    multicodec.Decoder
	Writer *bufio.Writer
	Reader *bufio.Reader
}

func WrapStream(s net.Stream) *WrappedStream {
	reader := bufio.NewReader(s)
	writer := bufio.NewWriter(s)
	// This is where we pick our specific multicodec. In order to change the
	// codec, we only need to change this place.
	// See https://godoc.org/github.com/multiformats/go-multicodec/json
	dec := mcjson.Multicodec(false).Decoder(reader)
	enc := mcjson.Multicodec(false).Encoder(writer)
	return &WrappedStream{
		Stream: s,
		Reader: reader,
		Writer: writer,
		Enc:    enc,
		Dec:    dec,
	}
}

type LivepeerNode struct {
	// Self
	Identity  peer.ID             // the local node's identity
	Peerstore ps.Peerstore        // storage for other Peer instances
	Routing   routing.IpfsRouting // the routing system. recommend ipfs-dht
	Priv      crypto.PrivKey
	Pub       crypto.PubKey

	PeerHost    p2phost.Host             // the network host (server+client)
	StreamPeers map[string][]ps.PeerInfo // the network host (server+client)
	HLSBuf      *lpmsStream.HLSBuffer
	// HaveMsgBuffer map[string]peer.ID

	// StreamSwarms  map[string]net.Network //Every stream has its own swarm
}

//NewNode creates a new Livepeerd node.
func NewNode(listenPort int, priv crypto.PrivKey, pub crypto.PubKey) (*LivepeerNode, error) {
	pid, err := peer.IDFromPublicKey(pub)
	if err != nil {
		return nil, err
	}

	// Create a multiaddress
	addr, err := ma.NewMultiaddr(fmt.Sprintf("/ip4/127.0.0.1/tcp/%d", listenPort))
	if err != nil {
		return nil, err
	}

	// Create a peerstore
	store := ps.NewPeerstore()
	store.AddPrivKey(pid, priv)
	store.AddPubKey(pid, pub)

	// Create swarm (implements libP2P Network)
	netwrk, err := swarm.NewNetwork(
		context.Background(),
		[]ma.Multiaddr{addr},
		pid,
		store,
		nil)

	basicHost := bhost.New(netwrk)

	r, err := constructDHTRouting(context.Background(), basicHost, ds.NewMapDatastore())
	rHost := rhost.Wrap(basicHost, r)

	// return &LivepeerNode{Identity: pid, Peerstore: store, Routing: r, PeerHost: rHost, HaveMsgBuffer: make(map[string]peer.ID), StreamPeers: make(map[string][]ps.PeerInfo), Priv: priv, Pub: pub}, nil
	buf := lpmsStream.NewHLSBuffer(5, 1000)
	return &LivepeerNode{Identity: pid, Peerstore: store, Routing: r, PeerHost: rHost, HLSBuf: buf, StreamPeers: make(map[string][]ps.PeerInfo), Priv: priv, Pub: pub}, nil
}

//Takes a HLS stream and sends HAVE messages for each chunk to known peers
func (n *LivepeerNode) SendHaveChunk(name, strmID string) {
	// pid, _ := peer.IDHexDecode(peerID)
	// strmSwarm := n.StreamSwarm(strmID)
	peers := n.StreamPeers[strmID]
	if peers == nil {
		peers = make([]ps.PeerInfo, 0, 0)
		n.StreamPeers[strmID] = peers
	}

	for _, peer := range peers {
		if peer.ID != n.Identity {
			glog.Infof("Sending Have Msg to %v for %v", peer.ID.Pretty(), name)
			n.PeerHost.Connect(context.Background(), peer)
			stream, err := n.PeerHost.NewStream(context.Background(), peer.ID, LivepeerProtocol)
			if err != nil {
				log.Fatal(err)
			}
			n.sendMessage(stream, peer.ID, ChunkHaveID, ChunkHaveMsg{ChunkName: name, StreamID: strmID, PeerID: n.Identity.Pretty(), Addr: n.PeerHost.Addrs()[0].String()})
		}
	}
}

func (n *LivepeerNode) SendJoin(pid peer.ID) {
	stream, err := n.PeerHost.NewStream(context.Background(), pid, LivepeerProtocol)
	if err != nil {
		log.Fatal(err)
	}
	n.sendMessage(stream, pid, JoinMsgID, JoinMsg{ID: n.Identity.Pretty(), Addr: n.PeerHost.Addrs()[0].String()})
}

func (n *LivepeerNode) SendJoinAck(pid peer.ID) {
	stream, err := n.PeerHost.NewStream(context.Background(), pid, LivepeerProtocol)
	if err != nil {
		log.Fatal(err)
	}
	n.sendMessage(stream, pid, JoinAckID, nil)
}

func (n *LivepeerNode) SendChunkReq(peer ps.PeerInfo, cn, strmID string) {
	if n.PeerHost.Network().Connectedness(peer.ID) != net.Connected {
		n.PeerHost.Connect(context.Background(), peer)
	}
	glog.Infof("Sending Chunk Req for %v to %v", cn, peer.ID.Pretty())
	stream, err := n.PeerHost.NewStream(context.Background(), peer.ID, LivepeerProtocol)
	if err != nil {
		log.Fatal(err)
	}
	n.sendMessage(stream, peer.ID, ChunkReqID, ChunkReqMsg{ChunkName: cn, StreamID: strmID, Peer: n.Identity.Pretty()})
}

func (n *LivepeerNode) SendChunkReqAck(peer ps.PeerInfo, cn, strmID string, data []byte) {
	if n.PeerHost.Network().Connectedness(peer.ID) != net.Connected {
		n.PeerHost.Connect(context.Background(), peer)
	}
	stream, err := n.PeerHost.NewStream(context.Background(), peer.ID, LivepeerProtocol)
	if err != nil {
		log.Fatal(err)
	}
	n.sendMessage(stream, peer.ID, ChunkReqAckID, ChunkReqAckMsg{ChunkName: cn, StreamID: strmID, Data: fmt.Sprintf("%x", data)})
}

func (n *LivepeerNode) SendSubscribeReq(strmID StreamID) {
	pid, _ := strmID.SplitComponents()

	//Try and join the stream-specific swarm
	peerInfo, err := n.Routing.FindPeer(context.Background(), pid)
	if err != nil {
		log.Fatalf("Cannot subscribe to stream: %v", err)
	}

	// n.Peerstore.AddAddrs(pid, peerInfo.Addrs, ps.PermanentAddrTTL)
	// n.PeerHost.Connect(context.Background(), peerInfo)
	// n.SendJoin(pid)
	// swarm := n.StreamSwarm(strmID.String())
	// swarm.Peerstore().AddAddrs(pid, peerInfo.Addrs, ps.PermanentAddrTTL)
	// stream, err := swarm.NewStream(context.Background(), pid)

	n.PeerHost.Connect(context.Background(), peerInfo)
	stream, err := n.PeerHost.NewStream(context.Background(), pid, LivepeerProtocol)
	if err != nil {
		log.Fatal(err)
	}
	n.sendMessage(stream, pid, SubReqID, SubReqMsg{ID: n.Identity.Pretty(), Addr: n.PeerHost.Addrs()[0].String(), SID: strmID.String()})

	// swarm := n.StreamSwarm(strmID.String())
	// swarm.Peerstore().AddAddrs(pid, peerInfo.Addrs, ps.PermanentAddrTTL)
	// stream, err := swarm.NewStream(context.Background(), pid)
	// if err != nil {
	// 	log.Fatal(err)
	// }
	// glog.Infof("Sending subscribe message to %v", pid)
	// n.sendMessage(stream, pid, SubscribeReqID, nil)
}

func (n *LivepeerNode) sendMessage(stream net.Stream, pid peer.ID, msgID int, data interface{}) {
	// stream, err := n.PeerHost.NewStream(context.Background(), pid, LivepeerProtocol)
	// if err != nil {
	// 	log.Fatal(err)
	// }

	wrappedStream := WrapStream(stream)
	msg := Message{Msg: msgID, Data: data}
	// glog.Infof("msg.Data: %v", msg.Data)
	err := wrappedStream.Enc.Encode(msg)
	if err != nil {
		glog.Errorf("send message encode error: %v", err)
	}

	err = wrappedStream.Writer.Flush()
	if err != nil {
		glog.Errorf("send message flush error: %v", err)
	}
}

func constructDHTRouting(ctx context.Context, host p2phost.Host, dstore ds.Batching) (routing.IpfsRouting, error) {
	dhtRouting := dht.NewDHT(ctx, host, dstore)
	// dhtRouting.Validator[IpnsValidatorTag] = namesys.IpnsRecordValidator
	// dhtRouting.Selector[IpnsValidatorTag] = namesys.IpnsSelectorFunc
	return dhtRouting, nil
}

// func (n *LivepeerNode) StreamSwarm(strmID string) net.Network {
// 	// var strmSwarm net.Network
// 	if n.StreamSwarms[strmID] == nil {
// 		glog.Infof("Creating a new local swarm for %v", strmID)
// 		ps := ps.NewPeerstore()
// 		ps.AddPrivKey(n.Identity, n.Priv)
// 		ps.AddPubKey(n.Identity, n.Pub)

// 		strmSwarm, err := swarm.NewNetwork(
// 			context.Background(),
// 			[]ma.Multiaddr{n.PeerHost.Addrs()[0]},
// 			n.Identity,
// 			ps,
// 			nil)
// 		if err != nil {
// 			glog.Fatalf("Cannot create Stream Swarm: %v", err)
// 		}
// 		n.StreamSwarms[strmID] = strmSwarm
// 		return strmSwarm
// 	}

// 	return n.StreamSwarms[strmID]
// }

type StreamID string

func RandomStreamID() string {
	rand.Seed(time.Now().UnixNano())
	x := make([]byte, 16, 16)
	for i := 0; i < len(x); i++ {
		x[i] = byte(rand.Uint32())
	}
	// strmID, _ := peer.IDFromBytes(x)
	return fmt.Sprintf("%x", x)
}

func MakeStreamID(nodeID peer.ID, id string) StreamID {
	return StreamID(fmt.Sprintf("%s%s", peer.IDHexEncode(nodeID), id))
}

func (self *StreamID) String() string {
	return string(*self)
}

// Given a stream ID, return it's origin nodeID and the unique stream ID
func (self *StreamID) SplitComponents() (peer.ID, string) {
	strStreamID := string(*self)
	originComponentLength := 68 // 32 bytes == 64 hexadecimal digits
	if len(strStreamID) != (68 + 32) {
		return "", ""
	}
	pid, _ := peer.IDHexDecode(strStreamID[:originComponentLength])
	return pid, strStreamID[originComponentLength:]
}
